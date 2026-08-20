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

const networkFinalizer = "compute.sostackit.dev/network-finalizer"

// networkTransitionalStates are STACKIT network states in which the
// controller should only observe and requeue, without attempting further
// actions.
var networkTransitionalStates = map[string]bool{
	"CREATING": true, "DELETING": true, "UPDATING": true,
}

// NetworkReconciler reconciles a Network object against the STACKIT
// Compute Engine (IaaS) API.
type NetworkReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StackitClient *iaas.APIClient
}

// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=networks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=networks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=networks/finalizers,verbs=update

// Reconcile drives a Network towards the state described by its spec. When
// spec.existingId is set, it only observes the referenced STACKIT network
// and never creates, updates, or deletes it. Otherwise it owns the
// network's full lifecycle, mirroring ServerReconciler's pattern.
func (r *NetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	network := &computev1alpha1.Network{}
	if err := r.Get(ctx, req.NamespacedName, network); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !network.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, network)
	}

	adopted := isAdopted(network.Spec.ExistingID)

	if !adopted {
		added, err := ensureFinalizer(ctx, r.Client, network, networkFinalizer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if added {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if network.Status.NetworkId == "" {
		return r.reconcileCreate(ctx, network, adopted)
	}

	return r.reconcileExisting(ctx, network, adopted)
}

func (r *NetworkReconciler) reconcileCreate(ctx context.Context, network *computev1alpha1.Network, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	projectID, region := network.Spec.ProjectId, network.Spec.Region

	if adopted {
		id := *network.Spec.ExistingID
		current, err := stackit.GetNetwork(ctx, r.StackitClient, projectID, region, id)
		if err != nil {
			if stackit.IsNotFound(err) {
				r.setReadyCondition(network, metav1.ConditionFalse, "NotFound", "existingId not found in STACKIT")
				if statusErr := r.Status().Update(ctx, network); statusErr != nil {
					logger.Error(statusErr, "unable to update Network status after adopt-not-found")
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			return ctrl.Result{}, fmt.Errorf("fetching existing network: %w", err)
		}
		network.Status.NetworkId = id
		r.applyNetworkStatus(network, current)
		if err := r.Status().Update(ctx, network); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status after adopt: %w", err)
		}
		logger.Info("adopted existing network", "networkId", id)
		return ctrl.Result{Requeue: true}, nil
	}

	name := networkName(network)
	payload := stackit.BuildNetworkCreatePayload(name, network.Spec)

	created, err := stackit.CreateNetwork(ctx, r.StackitClient, projectID, region, payload)
	if err != nil {
		r.setReadyCondition(network, metav1.ConditionFalse, "CreateFailed", err.Error())
		if statusErr := r.Status().Update(ctx, network); statusErr != nil {
			logger.Error(statusErr, "unable to update Network status after create failure")
		}
		return ctrl.Result{}, fmt.Errorf("creating network: %w", err)
	}

	network.Status.NetworkId = created.Id
	r.applyNetworkStatus(network, created)
	r.setReadyCondition(network, metav1.ConditionFalse, "Creating", "network creation triggered")
	if err := r.Status().Update(ctx, network); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status after create: %w", err)
	}

	logger.Info("triggered network creation", "networkId", network.Status.NetworkId)
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *NetworkReconciler) reconcileExisting(ctx context.Context, network *computev1alpha1.Network, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	projectID, region, networkID := network.Spec.ProjectId, network.Spec.Region, network.Status.NetworkId

	current, err := stackit.GetNetwork(ctx, r.StackitClient, projectID, region, networkID)
	if err != nil {
		if stackit.IsNotFound(err) {
			if adopted {
				logger.Info("adopted network no longer exists in STACKIT", "networkId", networkID)
				r.setReadyCondition(network, metav1.ConditionFalse, "NotFound", "existingId not found in STACKIT")
				if statusErr := r.Status().Update(ctx, network); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			logger.Info("network no longer exists in STACKIT, will recreate", "networkId", networkID)
			network.Status.NetworkId = ""
			r.setReadyCondition(network, metav1.ConditionFalse, "NotFound", "network not found in STACKIT, will recreate")
			if statusErr := r.Status().Update(ctx, network); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching network: %w", err)
	}

	r.applyNetworkStatus(network, current)
	state := network.Status.State

	switch {
	case networkTransitionalStates[state]:
		r.setReadyCondition(network, metav1.ConditionFalse, "Transitioning", fmt.Sprintf("network is %s", state))
		if err := r.Status().Update(ctx, network); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil

	case state == "FAILED":
		r.setReadyCondition(network, metav1.ConditionFalse, "Error", fmt.Sprintf("network is %s", state))
		if err := r.Status().Update(ctx, network); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: errorInterval}, nil
	}

	if !adopted {
		if triggered, err := r.reconcileDrift(ctx, network, current); err != nil {
			return ctrl.Result{}, err
		} else if triggered {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	reason, condStatus := "Unknown", metav1.ConditionFalse
	switch state {
	case "CREATED":
		reason, condStatus = "Created", metav1.ConditionTrue
	case "UPDATED":
		reason, condStatus = "Updated", metav1.ConditionTrue
	}
	r.setReadyCondition(network, condStatus, reason, fmt.Sprintf("network is %s", state))
	if err := r.Status().Update(ctx, network); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: resyncPeriod}, nil
}

// reconcileDrift compares the desired spec against the observed STACKIT
// network and triggers at most one corrective API call per reconcile, same
// as ServerReconciler.reconcileDrift. Only called for owned (non-adopted)
// networks. Unlike the other resources, STACKIT's PartialUpdateNetwork
// returns no updated object, so on success this only signals a fast
// requeue - the next GetNetwork picks up the applied change.
func (r *NetworkReconciler) reconcileDrift(ctx context.Context, network *computev1alpha1.Network, current *iaas.Network) (bool, error) {
	logger := log.FromContext(ctx)
	projectID, region, networkID := network.Spec.ProjectId, network.Spec.Region, network.Status.NetworkId

	name := networkName(network)
	nameChanged := current.Name != name
	dhcpChanged := network.Spec.Dhcp != nil && derefBool(current.Dhcp) != *network.Spec.Dhcp
	routedChanged := network.Spec.Routed != nil && derefBool(current.Routed) != *network.Spec.Routed
	routingTableChanged := network.Spec.RoutingTableId != "" && derefString(current.RoutingTableId) != network.Spec.RoutingTableId
	labelsChanged := !labelsEqual(current.Labels, network.Spec.Labels)

	if nameChanged || dhcpChanged || routedChanged || routingTableChanged || labelsChanged {
		updateName := ""
		if nameChanged {
			updateName = name
		}
		var updateDhcp, updateRouted *bool
		if dhcpChanged {
			updateDhcp = network.Spec.Dhcp
		}
		if routedChanged {
			updateRouted = network.Spec.Routed
		}
		updateRoutingTable := ""
		if routingTableChanged {
			updateRoutingTable = network.Spec.RoutingTableId
		}
		var updateLabels map[string]string
		if labelsChanged {
			updateLabels = network.Spec.Labels
		}
		logger.Info("updating network metadata",
			"nameChanged", nameChanged, "dhcpChanged", dhcpChanged,
			"routedChanged", routedChanged, "routingTableChanged", routingTableChanged,
			"labelsChanged", labelsChanged)
		payload := stackit.BuildNetworkUpdatePayload(updateName, updateDhcp, updateRouted, updateRoutingTable, updateLabels)
		if err := stackit.UpdateNetwork(ctx, r.StackitClient, projectID, region, networkID, payload); err != nil {
			return false, fmt.Errorf("updating network: %w", err)
		}
		return true, nil
	}

	return false, nil
}

func (r *NetworkReconciler) reconcileDelete(ctx context.Context, network *computev1alpha1.Network) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(network, networkFinalizer) {
		return ctrl.Result{}, nil
	}

	if network.Status.NetworkId == "" {
		if err := removeFinalizerAndUpdate(ctx, r.Client, network, networkFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	projectID, region, networkID := network.Spec.ProjectId, network.Spec.Region, network.Status.NetworkId

	current, err := stackit.GetNetwork(ctx, r.StackitClient, projectID, region, networkID)
	if err != nil {
		if stackit.IsNotFound(err) {
			logger.Info("network deleted from STACKIT", "networkId", networkID)
			if err := removeFinalizerAndUpdate(ctx, r.Client, network, networkFinalizer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("checking network before deletion: %w", err)
	}

	if current.Status != "DELETING" {
		if err := stackit.DeleteNetwork(ctx, r.StackitClient, projectID, region, networkID); err != nil {
			if stackit.IsConflict(err) {
				logger.Info("network not yet deletable, retrying", "networkId", networkID, "reason", err.Error())
				return ctrl.Result{RequeueAfter: pollInterval}, nil
			}
			if !stackit.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting network: %w", err)
			}
		}
		logger.Info("triggered network deletion", "networkId", networkID)
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *NetworkReconciler) applyNetworkStatus(network *computev1alpha1.Network, current *iaas.Network) {
	network.Status.State = current.Status
	if current.Ipv4 != nil {
		network.Status.Ipv4Prefixes = append([]string(nil), current.Ipv4.Prefixes...)
		network.Status.Ipv4Gateway = derefString(current.Ipv4.Gateway.Get())
	}
	network.Status.ObservedGeneration = network.Generation
}

func (r *NetworkReconciler) setReadyCondition(network *computev1alpha1.Network, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&network.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: network.Generation,
	})
}

// networkName returns the name the network should have in STACKIT,
// defaulting to the Kubernetes object name when spec.name is unset.
func networkName(network *computev1alpha1.Network) string {
	if network.Spec.Name != "" {
		return network.Spec.Name
	}
	return network.Name
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&computev1alpha1.Network{}).
		Named("network").
		Complete(r)
}
