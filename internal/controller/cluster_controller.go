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

	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
	"github.com/bartvanbenthem/stackit-compute-operator/internal/stackit"
)

const clusterFinalizer = "compute.sostackit.dev/cluster-finalizer"

// clusterTransitionalStates are STACKIT SKE aggregated cluster states in
// which the controller should only observe and requeue, without attempting
// further actions.
var clusterTransitionalStates = map[string]bool{
	"STATE_CREATING": true, "STATE_DELETING": true, "STATE_RECONCILING": true,
	"STATE_HIBERNATING": true, "STATE_WAKINGUP": true,
}

// ClusterReconciler reconciles a Cluster object against the STACKIT
// Kubernetes Engine (SKE) API.
type ClusterReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StackitClient *ske.APIClient
}

// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=clusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=compute.sostackit.dev,resources=clusters/finalizers,verbs=update

// Reconcile drives a Cluster towards the state described by its spec. When
// spec.existingClusterName is set, it only observes the referenced STACKIT
// cluster and never creates, updates, or deletes it. Otherwise it owns the
// cluster's full lifecycle, mirroring the other reconcilers' pattern. Unlike
// the IaaS resources, SKE has no separate object ID: the cluster name is
// itself the identifier used for every API call.
func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	cluster := &computev1alpha1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result, err := r.reconcile(ctx, cluster)
	return demoteTransientAuthError(ctx, result, err)
}

func (r *ClusterReconciler) reconcile(ctx context.Context, cluster *computev1alpha1.Cluster) (ctrl.Result, error) {
	if !cluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster)
	}

	adopted := isAdopted(cluster.Spec.ExistingClusterName)

	if !adopted {
		added, err := ensureFinalizer(ctx, r.Client, cluster, clusterFinalizer)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		if added {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if cluster.Status.ClusterName == "" {
		return r.reconcileCreate(ctx, cluster, adopted)
	}

	return r.reconcileExisting(ctx, cluster, adopted)
}

func (r *ClusterReconciler) reconcileCreate(ctx context.Context, cluster *computev1alpha1.Cluster, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *cluster.Status.DeepCopy()
	projectID, region := cluster.Spec.ProjectId, cluster.Spec.Region

	if adopted {
		name := *cluster.Spec.ExistingClusterName
		current, err := stackit.GetCluster(ctx, r.StackitClient, projectID, region, name)
		if err != nil {
			if stackit.IsNotFound(err) {
				r.setReadyCondition(cluster, metav1.ConditionFalse, "NotFound", "existingClusterName not found in STACKIT")
				if !statusUnchanged(before, cluster.Status) {
					if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
						logger.Error(statusErr, "unable to update Cluster status after adopt-not-found")
					}
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			return ctrl.Result{}, fmt.Errorf("fetching existing cluster: %w", err)
		}
		cluster.Status.ClusterName = name
		r.applyClusterStatus(cluster, current)
		if !statusUnchanged(before, cluster.Status) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, fmt.Errorf("updating status after adopt: %w", err)
			}
		}
		logger.Info("adopted existing cluster", "clusterName", name)
		return ctrl.Result{Requeue: true}, nil
	}

	name := clusterName(cluster)
	payload := stackit.BuildClusterPayload(cluster.Spec)

	created, err := stackit.CreateOrUpdateCluster(ctx, r.StackitClient, projectID, region, name, payload)
	if err != nil {
		r.setReadyCondition(cluster, metav1.ConditionFalse, "CreateFailed", err.Error())
		if !statusUnchanged(before, cluster.Status) {
			if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
				logger.Error(statusErr, "unable to update Cluster status after create failure")
			}
		}
		return ctrl.Result{}, fmt.Errorf("creating cluster: %w", err)
	}

	cluster.Status.ClusterName = name
	r.applyClusterStatus(cluster, created)
	r.setReadyCondition(cluster, metav1.ConditionFalse, "Creating", "cluster creation triggered")
	if !statusUnchanged(before, cluster.Status) {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating status after create: %w", err)
		}
	}

	logger.Info("triggered cluster creation", "clusterName", cluster.Status.ClusterName)
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *ClusterReconciler) reconcileExisting(ctx context.Context, cluster *computev1alpha1.Cluster, adopted bool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	before := *cluster.Status.DeepCopy()
	projectID, region, name := cluster.Spec.ProjectId, cluster.Spec.Region, cluster.Status.ClusterName

	current, err := stackit.GetCluster(ctx, r.StackitClient, projectID, region, name)
	if err != nil {
		if stackit.IsNotFound(err) {
			if adopted {
				logger.Info("adopted cluster no longer exists in STACKIT", "clusterName", name)
				r.setReadyCondition(cluster, metav1.ConditionFalse, "NotFound", "existingClusterName not found in STACKIT")
				if !statusUnchanged(before, cluster.Status) {
					if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
						return ctrl.Result{}, statusErr
					}
				}
				return ctrl.Result{RequeueAfter: errorInterval}, nil
			}
			logger.Info("cluster no longer exists in STACKIT, will recreate", "clusterName", name)
			cluster.Status.ClusterName = ""
			r.setReadyCondition(cluster, metav1.ConditionFalse, "NotFound", "cluster not found in STACKIT, will recreate")
			if !statusUnchanged(before, cluster.Status) {
				if statusErr := r.Status().Update(ctx, cluster); statusErr != nil {
					return ctrl.Result{}, statusErr
				}
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching cluster: %w", err)
	}

	r.applyClusterStatus(cluster, current)
	state := cluster.Status.State

	switch {
	case clusterTransitionalStates[state]:
		r.setReadyCondition(cluster, metav1.ConditionFalse, "Transitioning", fmt.Sprintf("cluster is %s", state))
		if !statusUnchanged(before, cluster.Status) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil

	case state == "STATE_UNHEALTHY":
		r.setReadyCondition(cluster, metav1.ConditionFalse, "Unhealthy", fmt.Sprintf("cluster is %s", state))
		if !statusUnchanged(before, cluster.Status) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: errorInterval}, nil
	}

	if !adopted {
		if triggered, err := r.reconcileDrift(ctx, cluster, current); err != nil {
			return ctrl.Result{}, err
		} else if triggered {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	reason, condStatus := "Unknown", metav1.ConditionFalse
	switch state {
	case "STATE_HEALTHY":
		reason, condStatus = "Healthy", metav1.ConditionTrue
	case "STATE_HIBERNATED":
		reason, condStatus = "Hibernated", metav1.ConditionTrue
	}
	r.setReadyCondition(cluster, condStatus, reason, fmt.Sprintf("cluster is %s", state))
	if !statusUnchanged(before, cluster.Status) {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: resyncPeriod}, nil
}

// reconcileDrift compares the desired spec against the observed STACKIT
// cluster and triggers at most one corrective API call per reconcile, same
// as the other reconcilers' drift handling. SKE's CreateOrUpdateCluster is a
// full-replace upsert, so any detected drift is corrected by resubmitting
// the whole desired payload rather than a partial patch. Only called for
// owned (non-adopted) clusters.
func (r *ClusterReconciler) reconcileDrift(ctx context.Context, cluster *computev1alpha1.Cluster, current *ske.Cluster) (bool, error) {
	logger := log.FromContext(ctx)
	projectID, region, name := cluster.Spec.ProjectId, cluster.Spec.Region, cluster.Status.ClusterName

	versionChanged := current.Kubernetes.Version != cluster.Spec.KubernetesVersion
	nodepoolsChanged := !nodepoolsEqual(current.Nodepools, cluster.Spec.NodePools)
	maintenanceChanged := !maintenanceEqual(current.Maintenance, cluster.Spec.Maintenance)

	if versionChanged || nodepoolsChanged || maintenanceChanged {
		logger.Info("updating cluster",
			"versionChanged", versionChanged, "nodepoolsChanged", nodepoolsChanged,
			"maintenanceChanged", maintenanceChanged)
		payload := stackit.BuildClusterPayload(cluster.Spec)
		if _, err := stackit.CreateOrUpdateCluster(ctx, r.StackitClient, projectID, region, name, payload); err != nil {
			return false, fmt.Errorf("updating cluster: %w", err)
		}
		return true, nil
	}

	return false, nil
}

func (r *ClusterReconciler) reconcileDelete(ctx context.Context, cluster *computev1alpha1.Cluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cluster, clusterFinalizer) {
		return ctrl.Result{}, nil
	}

	if cluster.Status.ClusterName == "" {
		if err := removeFinalizerAndUpdate(ctx, r.Client, cluster, clusterFinalizer); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	projectID, region, name := cluster.Spec.ProjectId, cluster.Spec.Region, cluster.Status.ClusterName

	current, err := stackit.GetCluster(ctx, r.StackitClient, projectID, region, name)
	if err != nil {
		if stackit.IsNotFound(err) {
			logger.Info("cluster deleted from STACKIT", "clusterName", name)
			if err := removeFinalizerAndUpdate(ctx, r.Client, cluster, clusterFinalizer); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("checking cluster before deletion: %w", err)
	}

	if clusterAggregatedState(current) != "STATE_DELETING" {
		if err := stackit.DeleteCluster(ctx, r.StackitClient, projectID, region, name); err != nil {
			if stackit.IsConflict(err) {
				logger.Info("cluster not yet deletable, retrying", "clusterName", name, "reason", err.Error())
				return ctrl.Result{RequeueAfter: pollInterval}, nil
			}
			if !stackit.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting cluster: %w", err)
			}
		}
		logger.Info("triggered cluster deletion", "clusterName", name)
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func (r *ClusterReconciler) applyClusterStatus(cluster *computev1alpha1.Cluster, current *ske.Cluster) {
	cluster.Status.State = clusterAggregatedState(current)
	cluster.Status.KubernetesVersion = current.Kubernetes.Version

	if current.Status != nil {
		cluster.Status.Hibernated = derefBool(current.Status.Hibernated)
		cluster.Status.PodAddressRanges = append([]string(nil), current.Status.PodAddressRanges...)
		cluster.Status.EgressAddressRanges = append([]string(nil), current.Status.EgressAddressRanges...)

		errs := make([]string, 0, len(current.Status.Errors))
		for _, e := range current.Status.Errors {
			errs = append(errs, fmt.Sprintf("%s: %s", derefString(e.Code), derefString(e.Message)))
		}
		cluster.Status.Errors = errs
	}

	cluster.Status.ObservedGeneration = cluster.Generation
}

func (r *ClusterReconciler) setReadyCondition(cluster *computev1alpha1.Cluster, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               readyConditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
}

// clusterAggregatedState returns current's aggregated status state, or "" if
// current has no status yet (e.g. immediately after creation).
func clusterAggregatedState(current *ske.Cluster) string {
	if current.Status == nil || current.Status.Aggregated == nil {
		return ""
	}
	return string(*current.Status.Aggregated)
}

// clusterName returns the name the cluster should have in STACKIT,
// defaulting to the Kubernetes object name when spec.name is unset.
func clusterName(cluster *computev1alpha1.Cluster) string {
	if cluster.Spec.Name != "" {
		return cluster.Spec.Name
	}
	return cluster.Name
}

// nodepoolsEqual reports whether the observed node pools match the desired
// spec closely enough that no update is needed. It compares the fields this
// operator manages; fields SKE may add server-side (taints, CRI, etc.) are
// ignored.
func nodepoolsEqual(current []ske.Nodepool, desired []computev1alpha1.NodePoolSpec) bool {
	if len(current) != len(desired) {
		return false
	}
	byName := make(map[string]ske.Nodepool, len(current))
	for _, np := range current {
		byName[np.Name] = np
	}
	for _, want := range desired {
		got, ok := byName[want.Name]
		if !ok {
			return false
		}
		if got.Machine.Type != want.MachineType {
			return false
		}
		if got.Machine.Image.Name != want.MachineImageName || got.Machine.Image.Version != want.MachineImageVersion {
			return false
		}
		if got.Minimum != int32(want.Minimum) || got.Maximum != int32(want.Maximum) {
			return false
		}
		if got.Volume.Size != int32(want.Volume.Size) {
			return false
		}
		if want.Volume.Type != "" && derefString(got.Volume.Type) != want.Volume.Type {
			return false
		}
		if !stringSlicesEqualUnordered(got.AvailabilityZones, want.AvailabilityZones) {
			return false
		}
	}
	return true
}

// maintenanceEqual reports whether the observed maintenance config matches
// the desired spec. A nil desired spec is only considered equal to a nil
// (or zero-value) observed config, since SKE's own default cannot be
// distinguished from "unset" through this comparison; in practice this means
// an unset spec.maintenance never triggers drift once SKE's default has been
// applied.
func maintenanceEqual(current *ske.Maintenance, desired *computev1alpha1.ClusterMaintenanceSpec) bool {
	if desired == nil {
		return true
	}
	if current == nil {
		return false
	}
	if derefBool(current.AutoUpdate.KubernetesVersion) != derefBool(desired.AutoUpdateKubernetesVersion) {
		return false
	}
	if derefBool(current.AutoUpdate.MachineImageVersion) != derefBool(desired.AutoUpdateMachineImageVersion) {
		return false
	}
	if !current.TimeWindow.Start.Equal(desired.Start.Time) || !current.TimeWindow.End.Equal(desired.End.Time) {
		return false
	}
	return true
}

func stringSlicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&computev1alpha1.Cluster{}).
		Named("cluster").
		Complete(r)
}
