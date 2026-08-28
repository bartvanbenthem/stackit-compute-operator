package controller

import (
	"context"
	"fmt"
	"strings"

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

const volumeFinalizer = "compute.sostackit.dev/volume-finalizer"

// volumeTransitionalStates are STACKIT volume states in which the
// controller should only observe and requeue, without attempting further
// actions.
var volumeTransitionalStates = map[string]bool{
	"CREATING": true, "ATTACHING": true, "DETACHING": true, "DOWNLOADING": true,
	"BACKING-UP": true, "RESIZING": true, "RESTORING-BACKUP": true, "RETYPING": true,
	"UPLOADING": true, "AWAITING-TRANSFER": true, "MAINTENANCE": true, "RESERVED": true,
}

// VolumeReconciler reconciles a Volume object against the STACKIT Compute
// Engine (IaaS) API.
type VolumeReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StackitClient *iaas.APIClient
}

// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=volumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=volumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=volumes/finalizers,verbs=update

// Reconcile drives a Volume towards the state described by its spec. When
// spec.existingId is set, it only observes the referenced STACKIT volume
// and never creates, updates, or deletes it. Otherwise it owns the volume's
// full lifecycle, mirroring ServerReconciler's pattern.
func (r *VolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	volume := &computev1alpha1.Volume{}
	if err := r.Get(ctx, req.NamespacedName, volume); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.FromContext(ctx).Info("starting volume reconciliation", "name", req.Name, "namespace", namespaceOrClusterScoped(req.Namespace))

	result, err := r.reconcile(ctx, volume)
	return demoteTransientAuthError(ctx, result, err)
}

func (r *VolumeReconciler) reconcile(ctx context.Context, volume *computev1alpha1.Volume) (ctrl.Result, error) {
	if !volume.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, volume)
	}

	adopted := isAdopted(volume.Spec.ExistingID)

	if !adopted {
		added, err := ensureFinalizer(ctx, r.Client, volume, volumeFinalizer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if added {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if volume.Status.VolumeId == "" {
		return r.reconcileCreate(ctx, volume, adopted)
	}

	return r.reconcileExisting(ctx, volume, adopted)
}

func (r *VolumeReconciler) reconcileCreate(ctx context.Context, volume *computev1alpha1.Volume, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *volume.Status.DeepCopy()
	projectID, region := volume.Spec.ProjectId, volume.Spec.Region

	if adopted {
		id := *volume.Spec.ExistingID
		current, err := stackit.GetVolume(ctx, r.StackitClient, projectID, region, id)
		if err != nil {
			if stackit.IsNotFound(err) {
				r.setReadyCondition(volume, metav1.ConditionFalse, "NotFound", "existingId not found in STACKIT")
				if !statusUnchanged(before, volume.Status) {
					if statusErr := r.Status().Update(ctx, volume); statusErr != nil {
						logger.Error(statusErr, "unable to update Volume status after adopt-not-found")
					}
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			return ctrl.Result{}, fmt.Errorf("fetching existing volume: %w", err)
		}
		volume.Status.VolumeId = id
		r.applyVolumeStatus(volume, current)
		if !statusUnchanged(before, volume.Status) {
			if err := r.Status().Update(ctx, volume); err != nil {
				return ctrl.Result{}, fmt.Errorf("updating status after adopt: %w", err)
			}
		}
		logger.Info("adopted existing volume", "volumeId", id)
		return ctrl.Result{Requeue: true}, nil
	}

	name := volumeName(volume)
	payload := stackit.BuildVolumeCreatePayload(name, volume.Spec)

	created, err := stackit.CreateVolume(ctx, r.StackitClient, projectID, region, payload)
	if err != nil {
		r.setReadyCondition(volume, metav1.ConditionFalse, "CreateFailed", err.Error())
		if !statusUnchanged(before, volume.Status) {
			if statusErr := r.Status().Update(ctx, volume); statusErr != nil {
				logger.Error(statusErr, "unable to update Volume status after create failure")
			}
		}
		return ctrl.Result{}, fmt.Errorf("creating volume: %w", err)
	}
	if created.Id == nil {
		return ctrl.Result{}, fmt.Errorf("STACKIT returned a volume without an ID")
	}

	volume.Status.VolumeId = *created.Id
	r.applyVolumeStatus(volume, created)
	r.setReadyCondition(volume, metav1.ConditionFalse, "Creating", "volume creation triggered")
	if !statusUnchanged(before, volume.Status) {
		if err := r.Status().Update(ctx, volume); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status after create: %w", err)
		}
	}

	logger.Info("triggered volume creation", "volumeId", volume.Status.VolumeId)
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *VolumeReconciler) reconcileExisting(ctx context.Context, volume *computev1alpha1.Volume, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *volume.Status.DeepCopy()
	projectID, region, volumeID := volume.Spec.ProjectId, volume.Spec.Region, volume.Status.VolumeId

	current, err := stackit.GetVolume(ctx, r.StackitClient, projectID, region, volumeID)
	if err != nil {
		if stackit.IsNotFound(err) {
			if adopted {
				logger.Info("adopted volume no longer exists in STACKIT", "volumeId", volumeID)
				r.setReadyCondition(volume, metav1.ConditionFalse, "NotFound", "existingId not found in STACKIT")
				if !statusUnchanged(before, volume.Status) {
					if statusErr := r.Status().Update(ctx, volume); statusErr != nil {
						return ctrl.Result{}, statusErr
					}
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			logger.Info("volume no longer exists in STACKIT, will recreate", "volumeId", volumeID)
			volume.Status.VolumeId = ""
			r.setReadyCondition(volume, metav1.ConditionFalse, "NotFound", "volume not found in STACKIT, will recreate")
			if !statusUnchanged(before, volume.Status) {
				if statusErr := r.Status().Update(ctx, volume); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching volume: %w", err)
	}

	r.applyVolumeStatus(volume, current)
	state := volume.Status.State

	switch {
	case volumeTransitionalStates[state]:
		r.setReadyCondition(volume, metav1.ConditionFalse, "Transitioning", fmt.Sprintf("volume is %s", state))
		if !statusUnchanged(before, volume.Status) {
			if err := r.Status().Update(ctx, volume); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil

	case strings.HasPrefix(state, "ERROR"):
		r.setReadyCondition(volume, metav1.ConditionFalse, "Error", fmt.Sprintf("volume is %s", state))
		if !statusUnchanged(before, volume.Status) {
			if err := r.Status().Update(ctx, volume); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: errorInterval}, nil
	}

	if !adopted {
		if triggered, err := r.reconcileDrift(ctx, volume, current); err != nil {
			return ctrl.Result{}, err
		} else if triggered {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	reason, condStatus := "Unknown", metav1.ConditionFalse
	switch state {
	case "AVAILABLE":
		reason, condStatus = "Available", metav1.ConditionTrue
	case "ATTACHED":
		reason, condStatus = "Attached", metav1.ConditionTrue
	}
	r.setReadyCondition(volume, condStatus, reason, fmt.Sprintf("volume is %s", state))
	if !statusUnchanged(before, volume.Status) {
		if err := r.Status().Update(ctx, volume); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: resyncPeriod}, nil
}

// reconcileDrift compares the desired spec against the observed STACKIT
// volume and triggers at most one corrective API call per reconcile, same
// as ServerReconciler.reconcileDrift. Only called for owned (non-adopted)
// volumes.
func (r *VolumeReconciler) reconcileDrift(ctx context.Context, volume *computev1alpha1.Volume, current *iaas.Volume) (bool, error) {
	logger := log.FromContext(ctx)
	projectID, region, volumeID := volume.Spec.ProjectId, volume.Spec.Region, volume.Status.VolumeId

	if volume.Spec.Size > 0 && derefInt64(current.Size) != volume.Spec.Size {
		logger.Info("resizing volume", "from", derefInt64(current.Size), "to", volume.Spec.Size)
		if err := stackit.ResizeVolume(ctx, r.StackitClient, projectID, region, volumeID, volume.Spec.Size); err != nil {
			return false, fmt.Errorf("resizing volume: %w", err)
		}
		return true, nil
	}

	name := volumeName(volume)
	nameChanged := derefString(current.Name) != name
	descChanged := volume.Spec.Description != "" && derefString(current.Description) != volume.Spec.Description
	bootableChanged := volume.Spec.Bootable != nil && derefBool(current.Bootable) != *volume.Spec.Bootable
	labelsChanged := !labelsEqual(current.Labels, volume.Spec.Labels)

	if nameChanged || descChanged || bootableChanged || labelsChanged {
		updateName, updateDesc := "", ""
		if nameChanged {
			updateName = name
		}
		if descChanged {
			updateDesc = volume.Spec.Description
		}
		var updateBootable *bool
		if bootableChanged {
			updateBootable = volume.Spec.Bootable
		}
		var updateLabels map[string]string
		if labelsChanged {
			updateLabels = volume.Spec.Labels
		}
		logger.Info("updating volume metadata", "nameChanged", nameChanged, "descChanged", descChanged, "bootableChanged", bootableChanged, "labelsChanged", labelsChanged)
		payload := stackit.BuildVolumeUpdatePayload(updateName, updateDesc, updateBootable, updateLabels)
		if _, err := stackit.UpdateVolume(ctx, r.StackitClient, projectID, region, volumeID, payload); err != nil {
			return false, fmt.Errorf("updating volume: %w", err)
		}
		return true, nil
	}

	return false, nil
}

func (r *VolumeReconciler) reconcileDelete(ctx context.Context, volume *computev1alpha1.Volume) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(volume, volumeFinalizer) {
		return ctrl.Result{}, nil
	}

	if volume.Status.VolumeId == "" {
		if err := removeFinalizerAndUpdate(ctx, r.Client, volume, volumeFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	projectID, region, volumeID := volume.Spec.ProjectId, volume.Spec.Region, volume.Status.VolumeId

	current, err := stackit.GetVolume(ctx, r.StackitClient, projectID, region, volumeID)
	if err != nil {
		if stackit.IsNotFound(err) {
			logger.Info("volume deleted from STACKIT", "volumeId", volumeID)
			if err := removeFinalizerAndUpdate(ctx, r.Client, volume, volumeFinalizer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("checking volume before deletion: %w", err)
	}

	if derefString(current.Status) != "DELETING" {
		if err := stackit.DeleteVolume(ctx, r.StackitClient, projectID, region, volumeID); err != nil {
			if stackit.IsConflict(err) {
				logger.Info("volume not yet deletable, retrying", "volumeId", volumeID, "reason", err.Error())
				return ctrl.Result{RequeueAfter: pollInterval}, nil
			}
			if !stackit.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting volume: %w", err)
			}
		}
		logger.Info("triggered volume deletion", "volumeId", volumeID)
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *VolumeReconciler) applyVolumeStatus(volume *computev1alpha1.Volume, current *iaas.Volume) {
	volume.Status.State = derefString(current.Status)
	volume.Status.Size = derefInt64(current.Size)
	volume.Status.ServerId = derefString(current.ServerId)
	volume.Status.ObservedGeneration = volume.Generation
}

func (r *VolumeReconciler) setReadyCondition(volume *computev1alpha1.Volume, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&volume.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: volume.Generation,
	})
}

// volumeName returns the name the volume should have in STACKIT, defaulting
// to the Kubernetes object name when spec.name is unset.
func volumeName(volume *computev1alpha1.Volume) string {
	if volume.Spec.Name != "" {
		return volume.Spec.Name
	}
	return volume.Name
}

// SetupWithManager sets up the controller with the Manager.
func (r *VolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&computev1alpha1.Volume{}).
		Named("volume").
		Complete(r)
}
