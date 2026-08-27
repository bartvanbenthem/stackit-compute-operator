package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
	"github.com/bartvanbenthem/stackit-compute-operator/internal/stackit"
)

const imageFinalizer = "compute.sostackit.dev/image-finalizer"

// ImageReconciler reconciles an Image object against the STACKIT Compute
// Engine (IaaS) API.
//
// Creating an image only registers its metadata and returns an upload URL;
// STACKIT does not make the image available until its bytes are PUT to that
// URL out-of-band, which this controller cannot do declaratively. Ready
// therefore stays False/"AwaitingUpload" until a later observation reports
// the image as AVAILABLE.
type ImageReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StackitClient *iaas.APIClient
}

// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=images,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=images/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=images/finalizers,verbs=update

// Reconcile drives an Image towards the state described by its spec. When
// spec.existingId is set, it only observes the referenced STACKIT image and
// never creates, updates, or deletes it. Otherwise it owns the image's full
// lifecycle, mirroring ServerReconciler's pattern.
func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	image := &computev1alpha1.Image{}
	if err := r.Get(ctx, req.NamespacedName, image); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result, err := r.reconcile(ctx, image)
	return demoteTransientAuthError(ctx, result, err)
}

func (r *ImageReconciler) reconcile(ctx context.Context, image *computev1alpha1.Image) (ctrl.Result, error) {
	if !image.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, image)
	}

	adopted := isAdopted(image.Spec.ExistingID)

	if !adopted {
		added, err := ensureFinalizer(ctx, r.Client, image, imageFinalizer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if added {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if image.Status.ImageId == "" {
		return r.reconcileCreate(ctx, image, adopted)
	}

	return r.reconcileExisting(ctx, image, adopted)
}

func (r *ImageReconciler) reconcileCreate(ctx context.Context, image *computev1alpha1.Image, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *image.Status.DeepCopy()
	projectID, region := image.Spec.ProjectId, image.Spec.Region

	if adopted {
		id := *image.Spec.ExistingID
		current, err := stackit.GetImage(ctx, r.StackitClient, projectID, region, id)
		if err != nil {
			if stackit.IsNotFound(err) {
				r.setReadyCondition(image, metav1.ConditionFalse, "NotFound", "existingId not found in STACKIT")
				if !statusUnchanged(before, image.Status) {
					if statusErr := r.Status().Update(ctx, image); statusErr != nil {
						logger.Error(statusErr, "unable to update Image status after adopt-not-found")
					}
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			return ctrl.Result{}, fmt.Errorf("fetching existing image: %w", err)
		}
		image.Status.ImageId = id
		r.applyImageStatus(image, current)
		if !statusUnchanged(before, image.Status) {
			if err := r.Status().Update(ctx, image); err != nil {
				return ctrl.Result{}, fmt.Errorf("updating status after adopt: %w", err)
			}
		}
		logger.Info("adopted existing image", "imageId", id)
		return ctrl.Result{Requeue: true}, nil
	}

	name := imageName(image)
	payload := stackit.BuildImageCreatePayload(name, image.Spec)

	created, err := stackit.CreateImage(ctx, r.StackitClient, projectID, region, payload)
	if err != nil {
		r.setReadyCondition(image, metav1.ConditionFalse, "CreateFailed", err.Error())
		if !statusUnchanged(before, image.Status) {
			if statusErr := r.Status().Update(ctx, image); statusErr != nil {
				logger.Error(statusErr, "unable to update Image status after create failure")
			}
		}
		return ctrl.Result{}, fmt.Errorf("creating image: %w", err)
	}

	image.Status.ImageId = created.Id
	image.Status.UploadUrl = created.UploadUrl
	image.Status.ObservedGeneration = image.Generation
	r.setReadyCondition(image, metav1.ConditionFalse, "AwaitingUpload", "image registered; upload image bytes to status.uploadUrl before it becomes available")
	if !statusUnchanged(before, image.Status) {
		if err := r.Status().Update(ctx, image); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status after create: %w", err)
		}
	}

	logger.Info("triggered image creation", "imageId", image.Status.ImageId)
	return ctrl.Result{RequeueAfter: errorInterval}, nil
}

func (r *ImageReconciler) reconcileExisting(ctx context.Context, image *computev1alpha1.Image, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *image.Status.DeepCopy()
	projectID, region, imageID := image.Spec.ProjectId, image.Spec.Region, image.Status.ImageId

	current, err := stackit.GetImage(ctx, r.StackitClient, projectID, region, imageID)
	if err != nil {
		if stackit.IsNotFound(err) {
			if adopted {
				logger.Info("adopted image no longer exists in STACKIT", "imageId", imageID)
				r.setReadyCondition(image, metav1.ConditionFalse, "NotFound", "existingId not found in STACKIT")
				if !statusUnchanged(before, image.Status) {
					if statusErr := r.Status().Update(ctx, image); statusErr != nil {
						return ctrl.Result{}, statusErr
					}
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			logger.Info("image no longer exists in STACKIT, will recreate", "imageId", imageID)
			image.Status.ImageId = ""
			r.setReadyCondition(image, metav1.ConditionFalse, "NotFound", "image not found in STACKIT, will recreate")
			if !statusUnchanged(before, image.Status) {
				if statusErr := r.Status().Update(ctx, image); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching image: %w", err)
	}

	r.applyImageStatus(image, current)
	state := image.Status.State

	switch state {
	case "CREATING":
		// For Image, CREATING typically means STACKIT is still waiting for
		// the uploaded bytes, not actively working - surface that
		// distinction instead of a generic "Transitioning" reason.
		r.setReadyCondition(image, metav1.ConditionFalse, "AwaitingUpload", "image is CREATING; upload bytes to status.uploadUrl")
		if !statusUnchanged(before, image.Status) {
			if err := r.Status().Update(ctx, image); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: errorInterval}, nil

	case "DELETING":
		r.setReadyCondition(image, metav1.ConditionFalse, "Transitioning", fmt.Sprintf("image is %s", state))
		if !statusUnchanged(before, image.Status) {
			if err := r.Status().Update(ctx, image); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil

	case "ERROR":
		r.setReadyCondition(image, metav1.ConditionFalse, "Error", fmt.Sprintf("image is %s", state))
		if !statusUnchanged(before, image.Status) {
			if err := r.Status().Update(ctx, image); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: errorInterval}, nil
	}

	if !adopted {
		if triggered, err := r.reconcileDrift(ctx, image, current); err != nil {
			return ctrl.Result{}, err
		} else if triggered {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	reason, condStatus := "Unknown", metav1.ConditionFalse
	switch state {
	case "AVAILABLE":
		reason, condStatus = "Available", metav1.ConditionTrue
	case "DEACTIVATED":
		reason = "Deactivated"
	}
	r.setReadyCondition(image, condStatus, reason, fmt.Sprintf("image is %s", state))
	if !statusUnchanged(before, image.Status) {
		if err := r.Status().Update(ctx, image); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: resyncPeriod}, nil
}

// reconcileDrift compares the desired spec against the observed STACKIT
// image and triggers at most one corrective API call per reconcile, same as
// ServerReconciler.reconcileDrift. Only called for owned (non-adopted)
// images. Config is not individually diffed (its nullable-string fields
// don't compare cleanly against the SDK's response shape) - it is simply
// resent whenever another field's drift already triggers an update.
func (r *ImageReconciler) reconcileDrift(ctx context.Context, image *computev1alpha1.Image, current *iaas.Image) (bool, error) {
	logger := log.FromContext(ctx)
	projectID, region, imageID := image.Spec.ProjectId, image.Spec.Region, image.Status.ImageId

	name := imageName(image)
	nameChanged := current.Name != name
	diskFormatChanged := image.Spec.DiskFormat != "" && current.DiskFormat != image.Spec.DiskFormat
	minDiskSizeChanged := image.Spec.MinDiskSize > 0 && derefInt64(current.MinDiskSize) != image.Spec.MinDiskSize
	minRamChanged := image.Spec.MinRam > 0 && derefInt64(current.MinRam) != image.Spec.MinRam
	protectedChanged := image.Spec.Protected != nil && derefBool(current.Protected) != *image.Spec.Protected
	labelsChanged := !labelsEqual(current.Labels, image.Spec.Labels)

	if nameChanged || diskFormatChanged || minDiskSizeChanged || minRamChanged || protectedChanged || labelsChanged {
		logger.Info("updating image metadata",
			"nameChanged", nameChanged, "diskFormatChanged", diskFormatChanged,
			"minDiskSizeChanged", minDiskSizeChanged, "minRamChanged", minRamChanged,
			"protectedChanged", protectedChanged, "labelsChanged", labelsChanged)
		desired := image.Spec
		desired.Name = name
		payload := stackit.BuildImageUpdatePayload(desired)
		if _, err := stackit.UpdateImage(ctx, r.StackitClient, projectID, region, imageID, payload); err != nil {
			return false, fmt.Errorf("updating image: %w", err)
		}
		return true, nil
	}

	return false, nil
}

func (r *ImageReconciler) reconcileDelete(ctx context.Context, image *computev1alpha1.Image) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(image, imageFinalizer) {
		return ctrl.Result{}, nil
	}

	if image.Status.ImageId == "" {
		if err := removeFinalizerAndUpdate(ctx, r.Client, image, imageFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	projectID, region, imageID := image.Spec.ProjectId, image.Spec.Region, image.Status.ImageId

	current, err := stackit.GetImage(ctx, r.StackitClient, projectID, region, imageID)
	if err != nil {
		if stackit.IsNotFound(err) {
			logger.Info("image deleted from STACKIT", "imageId", imageID)
			if err := removeFinalizerAndUpdate(ctx, r.Client, image, imageFinalizer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("checking image before deletion: %w", err)
	}

	if derefString(current.Status) != "DELETING" {
		if err := stackit.DeleteImage(ctx, r.StackitClient, projectID, region, imageID); err != nil && !stackit.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("deleting image: %w", err)
		}
		logger.Info("triggered image deletion", "imageId", imageID)
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// applyImageStatus mirrors observed fields from a fetched *iaas.Image. It
// does not touch status.UploadUrl: GetImage's response has no such field,
// unlike the ImageCreateResponse returned at creation time.
func (r *ImageReconciler) applyImageStatus(image *computev1alpha1.Image, current *iaas.Image) {
	image.Status.State = derefString(current.Status)
	image.Status.ImportProgress = derefInt64(current.ImportProgress)
	image.Status.Size = derefInt64(current.Size)
	image.Status.ObservedGeneration = image.Generation
}

func (r *ImageReconciler) setReadyCondition(image *computev1alpha1.Image, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&image.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: image.Generation,
	})
}

// imageName returns the name the image should have in STACKIT, defaulting
// to the Kubernetes object name when spec.name is unset.
func imageName(image *computev1alpha1.Image) string {
	if image.Spec.Name != "" {
		return image.Spec.Name
	}
	return image.Name
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&computev1alpha1.Image{}).
		Named("image").
		Complete(r)
}
