package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

const (
	serverFinalizer    = "compute.sostackit.dev/server-finalizer"
	readyConditionType = "Ready"

	pollInterval  = 10 * time.Second
	errorInterval = time.Minute
	resyncPeriod  = 5 * time.Minute
)

// transitionalStates are STACKIT server states in which the controller
// should only observe and requeue, without attempting further actions.
var transitionalStates = map[string]bool{
	"CREATING": true, "STARTING": true, "STOPPING": true, "RESIZING": true,
	"DELETING": true, "UPDATING": true, "REBOOT": true, "REBOOTING": true,
	"REBUILD": true, "REBUILDING": true, "RESCUE": true, "RESCUING": true,
	"RESTORING": true, "SNAPSHOTTING": true, "MIGRATING": true,
	"BACKING-UP": true, "UNRESCUING": true, "DEALLOCATING": true,
}

// ServerReconciler reconciles a Server object against the STACKIT Compute
// Engine (IaaS) API.
type ServerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StackitClient *iaas.APIClient
}

// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=servers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=servers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=servers/finalizers,verbs=update

// Reconcile drives a Server towards the state described by its spec: create
// it in STACKIT if it doesn't exist yet, keep its power state/machine
// type/metadata in sync, mirror observed status back onto the CR, and
// delete the STACKIT server when the CR is deleted.
func (r *ServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	server := &computev1alpha1.Server{}
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result, err := r.reconcile(ctx, server)
	return demoteTransientAuthError(ctx, result, err)
}

func (r *ServerReconciler) reconcile(ctx context.Context, server *computev1alpha1.Server) (ctrl.Result, error) {
	if !server.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, server)
	}

	if !controllerutil.ContainsFinalizer(server, serverFinalizer) {
		controllerutil.AddFinalizer(server, serverFinalizer)
		if err := r.Update(ctx, server); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if server.Status.ServerId == "" {
		return r.reconcileCreate(ctx, server)
	}

	return r.reconcileExisting(ctx, server)
}

func (r *ServerReconciler) reconcileCreate(ctx context.Context, server *computev1alpha1.Server) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *server.Status.DeepCopy()

	imageID, imageReady, err := r.resolveImageRef(ctx, server)
	if err != nil {
		return r.invalidReference(ctx, server, before, err)
	}
	networkID, networkReady, err := r.resolveNetworkRef(ctx, server)
	if err != nil {
		return r.invalidReference(ctx, server, before, err)
	}
	bootVolumeID, bootVolumeReady, err := r.resolveBootVolumeRef(ctx, server)
	if err != nil {
		return r.invalidReference(ctx, server, before, err)
	}

	if !imageReady || !networkReady || !bootVolumeReady {
		r.setReadyCondition(server, metav1.ConditionFalse, "WaitingForReference", "waiting for referenced Image/Network/Volume to become ready")
		if !statusUnchanged(before, server.Status) {
			if statusErr := r.Status().Update(ctx, server); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil
	}

	name := serverName(server)
	payload := stackit.BuildCreatePayload(name, server.Spec, imageID, networkID, bootVolumeID)

	created, err := stackit.CreateServer(ctx, r.StackitClient, server.Spec.ProjectId, server.Spec.Region, payload)
	if err != nil {
		r.setReadyCondition(server, metav1.ConditionFalse, "CreateFailed", err.Error())
		if !statusUnchanged(before, server.Status) {
			if statusErr := r.Status().Update(ctx, server); statusErr != nil {
				logger.Error(statusErr, "unable to update Server status after create failure")
			}
		}
		return ctrl.Result{}, fmt.Errorf("creating server: %w", err)
	}
	if created.Id == nil {
		return ctrl.Result{}, fmt.Errorf("STACKIT returned a server without an ID")
	}

	server.Status.ServerId = *created.Id
	r.applyServerStatus(server, created)
	r.setReadyCondition(server, metav1.ConditionFalse, "Creating", "server creation triggered")
	if !statusUnchanged(before, server.Status) {
		if err := r.Status().Update(ctx, server); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status after create: %w", err)
		}
	}

	logger.Info("triggered server creation", "serverId", server.Status.ServerId)
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// resolveImageRef resolves the server's desired image ID from exactly one
// of spec.imageId or spec.imageRef. ready is false (with a nil error) when
// a ref is set but the referenced Image doesn't exist yet or hasn't
// populated status.imageId yet - that's a normal, retryable wait, not an
// error. Neither is required when spec.bootVolumeRef is set, since the
// server then boots from that existing Volume instead of an image.
func (r *ServerReconciler) resolveImageRef(ctx context.Context, server *computev1alpha1.Server) (id string, ready bool, err error) {
	if server.Spec.ImageId != "" && server.Spec.ImageRef != nil {
		return "", false, fmt.Errorf("spec.imageId and spec.imageRef are mutually exclusive")
	}
	if server.Spec.ImageRef != nil {
		image := &computev1alpha1.Image{}
		key := client.ObjectKey{Namespace: server.Namespace, Name: server.Spec.ImageRef.Name}
		if err := r.Get(ctx, key, image); err != nil {
			if apierrors.IsNotFound(err) {
				return "", false, nil
			}
			return "", false, err
		}
		return image.Status.ImageId, image.Status.ImageId != "", nil
	}
	if server.Spec.ImageId != "" {
		return server.Spec.ImageId, true, nil
	}
	if server.Spec.BootVolumeRef != nil {
		return "", true, nil
	}
	return "", false, fmt.Errorf("one of spec.imageId or spec.imageRef must be set")
}

// resolveNetworkRef resolves the server's desired network ID from exactly
// one of spec.networkId or spec.networkRef, with the same not-ready-yet
// semantics as resolveImageRef.
func (r *ServerReconciler) resolveNetworkRef(ctx context.Context, server *computev1alpha1.Server) (id string, ready bool, err error) {
	if server.Spec.NetworkId != "" && server.Spec.NetworkRef != nil {
		return "", false, fmt.Errorf("spec.networkId and spec.networkRef are mutually exclusive")
	}
	if server.Spec.NetworkRef != nil {
		network := &computev1alpha1.Network{}
		key := client.ObjectKey{Namespace: server.Namespace, Name: server.Spec.NetworkRef.Name}
		if err := r.Get(ctx, key, network); err != nil {
			if apierrors.IsNotFound(err) {
				return "", false, nil
			}
			return "", false, err
		}
		return network.Status.NetworkId, network.Status.NetworkId != "", nil
	}
	if server.Spec.NetworkId != "" {
		return server.Spec.NetworkId, true, nil
	}
	return "", false, fmt.Errorf("one of spec.networkId or spec.networkRef must be set")
}

// resolveBootVolumeRef resolves the ID of an existing Volume to boot from
// when spec.bootVolumeRef is set. Unlike image/network, this has no raw-ID
// counterpart and is genuinely optional: an unset ref reports ready with an
// empty ID, meaning "create a boot volume from the image as usual".
//
// Readiness requires the volume to be AVAILABLE, not just to have a
// VolumeId: STACKIT assigns the ID as soon as creation is triggered, well
// before the volume is usable as a boot source. Starting a server create
// against a volume that's still CREATING (or RESERVED by another in-flight
// create) fails with "Volume is in wrong state" - waiting for AVAILABLE
// avoids hammering the API with those doomed attempts.
func (r *ServerReconciler) resolveBootVolumeRef(ctx context.Context, server *computev1alpha1.Server) (id string, ready bool, err error) {
	if server.Spec.BootVolumeRef == nil {
		return "", true, nil
	}
	volume := &computev1alpha1.Volume{}
	key := client.ObjectKey{Namespace: server.Namespace, Name: server.Spec.BootVolumeRef.Name}
	if err := r.Get(ctx, key, volume); err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return volume.Status.VolumeId, volume.Status.VolumeId != "" && volume.Status.State == "AVAILABLE", nil
}

// invalidReference records a permanent (non-retryable) reference validation
// failure, e.g. both a ref and its raw-ID counterpart being set. The next
// spec edit re-triggers reconciliation via the controller's watch on
// Server, so no requeue is scheduled here.
func (r *ServerReconciler) invalidReference(ctx context.Context, server *computev1alpha1.Server, before computev1alpha1.ServerStatus, refErr error) (ctrl.Result, error) {
	r.setReadyCondition(server, metav1.ConditionFalse, "InvalidReference", refErr.Error())
	if !statusUnchanged(before, server.Status) {
		if statusErr := r.Status().Update(ctx, server); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
	}
	return ctrl.Result{}, nil
}

func (r *ServerReconciler) reconcileExisting(ctx context.Context, server *computev1alpha1.Server) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *server.Status.DeepCopy()
	projectID, region, serverID := server.Spec.ProjectId, server.Spec.Region, server.Status.ServerId

	current, err := stackit.GetServer(ctx, r.StackitClient, projectID, region, serverID)
	if err != nil {
		if stackit.IsNotFound(err) {
			logger.Info("server no longer exists in STACKIT, will recreate", "serverId", serverID)
			server.Status.ServerId = ""
			r.setReadyCondition(server, metav1.ConditionFalse, "NotFound", "server not found in STACKIT, will recreate")
			if !statusUnchanged(before, server.Status) {
				if statusErr := r.Status().Update(ctx, server); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching server: %w", err)
	}

	r.applyServerStatus(server, current)
	state := server.Status.State

	switch {
	case transitionalStates[state]:
		r.setReadyCondition(server, metav1.ConditionFalse, "Transitioning", fmt.Sprintf("server is %s", state))
		if !statusUnchanged(before, server.Status) {
			if err := r.Status().Update(ctx, server); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil

	case state == "ERROR":
		r.setReadyCondition(server, metav1.ConditionFalse, "Error", derefString(current.ErrorMessage))
		if !statusUnchanged(before, server.Status) {
			if err := r.Status().Update(ctx, server); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: errorInterval}, nil
	}

	if triggered, err := r.reconcileDrift(ctx, server, current); err != nil {
		return ctrl.Result{}, err
	} else if triggered {
		return ctrl.Result{Requeue: true}, nil
	}

	reason, condStatus := "Unknown", metav1.ConditionFalse
	switch state {
	case "ACTIVE":
		reason, condStatus = "Active", metav1.ConditionTrue
	case "INACTIVE":
		reason, condStatus = "Inactive", metav1.ConditionTrue
	}
	r.setReadyCondition(server, condStatus, reason, fmt.Sprintf("server is %s", state))
	if !statusUnchanged(before, server.Status) {
		if err := r.Status().Update(ctx, server); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: resyncPeriod}, nil
}

// reconcileDrift compares the desired spec against the observed STACKIT
// server and triggers at most one corrective API call per reconcile so that
// status is re-synced between actions. It reports whether an action was
// triggered.
func (r *ServerReconciler) reconcileDrift(ctx context.Context, server *computev1alpha1.Server, current *iaas.Server) (bool, error) {
	logger := log.FromContext(ctx)
	projectID, region, serverID := server.Spec.ProjectId, server.Spec.Region, server.Status.ServerId

	if server.Spec.MachineType != "" && current.MachineType != server.Spec.MachineType {
		logger.Info("resizing server", "from", current.MachineType, "to", server.Spec.MachineType)
		if err := stackit.ResizeServer(ctx, r.StackitClient, projectID, region, serverID, server.Spec.MachineType); err != nil {
			return false, fmt.Errorf("resizing server: %w", err)
		}
		return true, nil
	}

	desiredPower := server.Spec.PowerState
	if desiredPower == "" {
		desiredPower = computev1alpha1.PowerStateActive
	}
	powerStatus := derefString(current.PowerStatus)
	switch {
	case desiredPower == computev1alpha1.PowerStateInactive && powerStatus == "RUNNING":
		logger.Info("stopping server", "serverId", serverID)
		if err := stackit.StopServer(ctx, r.StackitClient, projectID, region, serverID); err != nil {
			return false, fmt.Errorf("stopping server: %w", err)
		}
		return true, nil
	case desiredPower == computev1alpha1.PowerStateActive && powerStatus == "STOPPED":
		logger.Info("starting server", "serverId", serverID)
		if err := stackit.StartServer(ctx, r.StackitClient, projectID, region, serverID); err != nil {
			return false, fmt.Errorf("starting server: %w", err)
		}
		return true, nil
	}

	name := serverName(server)
	nameChanged := current.Name != name
	labelsChanged := !labelsEqual(current.Labels, server.Spec.Labels)
	if nameChanged || labelsChanged {
		updateName := ""
		if nameChanged {
			updateName = name
		}
		var updateLabels map[string]string
		if labelsChanged {
			updateLabels = server.Spec.Labels
		}
		logger.Info("updating server metadata", "nameChanged", nameChanged, "labelsChanged", labelsChanged)
		payload := stackit.BuildUpdatePayload(updateName, updateLabels)
		if _, err := stackit.UpdateServer(ctx, r.StackitClient, projectID, region, serverID, payload); err != nil {
			return false, fmt.Errorf("updating server: %w", err)
		}
		return true, nil
	}

	return false, nil
}

func (r *ServerReconciler) reconcileDelete(ctx context.Context, server *computev1alpha1.Server) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(server, serverFinalizer) {
		return ctrl.Result{}, nil
	}

	if server.Status.ServerId == "" {
		controllerutil.RemoveFinalizer(server, serverFinalizer)
		if err := r.Update(ctx, server); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	projectID, region, serverID := server.Spec.ProjectId, server.Spec.Region, server.Status.ServerId

	current, err := stackit.GetServer(ctx, r.StackitClient, projectID, region, serverID)
	if err != nil {
		if stackit.IsNotFound(err) {
			logger.Info("server deleted from STACKIT", "serverId", serverID)
			controllerutil.RemoveFinalizer(server, serverFinalizer)
			if updateErr := r.Update(ctx, server); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("checking server before deletion: %w", err)
	}

	if derefString(current.Status) != "DELETING" {
		if err := stackit.DeleteServer(ctx, r.StackitClient, projectID, region, serverID); err != nil && !stackit.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("deleting server: %w", err)
		}
		logger.Info("triggered server deletion", "serverId", serverID)
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *ServerReconciler) applyServerStatus(server *computev1alpha1.Server, current *iaas.Server) {
	server.Status.State = derefString(current.Status)
	server.Status.PowerStatus = derefString(current.PowerStatus)
	server.Status.MachineType = current.MachineType
	server.Status.ObservedGeneration = server.Generation

	nics := make([]computev1alpha1.NicStatus, 0, len(current.Nics))
	for _, nic := range current.Nics {
		nics = append(nics, computev1alpha1.NicStatus{
			NetworkId: nic.NetworkId,
			Ipv4:      derefString(nic.Ipv4),
			Ipv6:      derefString(nic.Ipv6),
			Mac:       nic.Mac,
		})
	}
	server.Status.Nics = nics
}

func (r *ServerReconciler) setReadyCondition(server *computev1alpha1.Server, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: server.Generation,
	})
}

// serverName returns the name the server should have in STACKIT, defaulting
// to the Kubernetes object name when spec.name is unset.
func serverName(server *computev1alpha1.Server) string {
	if server.Spec.Name != "" {
		return server.Spec.Name
	}
	return server.Name
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// labelsEqual reports whether every desired label is present in current
// with the same value. Extra labels present only in current (e.g. STACKIT's
// own "stackit-" prefixed labels) are ignored.
func labelsEqual(current map[string]interface{}, desired map[string]string) bool {
	for k, v := range desired {
		cv, ok := current[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", cv) != v {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&computev1alpha1.Server{}).
		Named("server").
		Complete(r)
}
