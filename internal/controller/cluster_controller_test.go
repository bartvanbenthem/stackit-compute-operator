package controller

import (
	"context"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

func newTestCluster(name string, mutate func(*computev1alpha1.Cluster)) *computev1alpha1.Cluster {
	cluster := &computev1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: computev1alpha1.ClusterSpec{
			ProjectId:         testProjectID,
			Region:            "eu01",
			KubernetesVersion: "1.29.3",
			NodePools: []computev1alpha1.NodePoolSpec{
				{
					Name:                "pool-1",
					MachineType:         "c1.2",
					MachineImageName:    "flatcar",
					MachineImageVersion: "3815.2.0",
					AvailabilityZones:   []string{"eu01-1"},
					Minimum:             1,
					Maximum:             3,
					Volume:              computev1alpha1.NodePoolVolumeSpec{Size: 32},
				},
			},
		},
	}
	if mutate != nil {
		mutate(cluster)
	}
	return cluster
}

func newClusterReconciler(t *testing.T, mock *ske.DefaultAPIServiceMock, objs ...client.Object) *ClusterReconciler {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(&computev1alpha1.Cluster{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return &ClusterReconciler{
		Client:        builder.Build(),
		Scheme:        newTestScheme(t),
		StackitClient: &ske.APIClient{DefaultAPI: mock},
	}
}

func getCluster(t *testing.T, c client.Client, name string) *computev1alpha1.Cluster {
	t.Helper()
	got := &computev1alpha1.Cluster{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
		t.Fatalf("getting cluster: %v", err)
	}
	return got
}

func clusterReconcileRequest(cluster *computev1alpha1.Cluster) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}}
}

func healthyClusterResponse() *ske.Cluster {
	state := ske.CLUSTERSTATUSSTATE_STATE_HEALTHY
	return &ske.Cluster{
		Name:       utils.Ptr("clu"),
		Kubernetes: ske.Kubernetes{Version: "1.29.3"},
		Nodepools: []ske.Nodepool{
			{
				Name:              "pool-1",
				AvailabilityZones: []string{"eu01-1"},
				Machine:           ske.Machine{Type: "c1.2", Image: ske.Image{Name: "flatcar", Version: "3815.2.0"}},
				Minimum:           1,
				Maximum:           3,
				Volume:            ske.Volume{Size: 32},
			},
		},
		Status: &ske.ClusterStatus{Aggregated: &state},
	}
}

// --- finalizer wiring ---

func TestClusterReconcile_AdoptedNeverAddsFinalizer(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		c.Spec.ExistingClusterName = utils.Ptr("clu")
	})
	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	if _, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	got := getCluster(t, r.Client, cluster.Name)
	if controllerutil.ContainsFinalizer(got, clusterFinalizer) {
		t.Errorf("finalizer added on adopted cluster, got finalizers %v", got.Finalizers)
	}
}

// --- create ---

func TestClusterReconcileCreate_Success(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
	})

	mock := &ske.DefaultAPIServiceMock{}
	created := false
	createFn := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		created = true
		state := ske.CLUSTERSTATUSSTATE_STATE_CREATING
		resp := healthyClusterResponse()
		resp.Status.Aggregated = &state
		return resp, nil
	}
	mock.CreateOrUpdateClusterExecuteMock = &createFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateOrUpdateCluster was not called")
	}
	if res.RequeueAfter != clusterPollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, clusterPollInterval)
	}

	got := getCluster(t, r.Client, cluster.Name)
	if got.Status.ClusterName != "clu" {
		t.Errorf("Status.ClusterName = %q, want %q", got.Status.ClusterName, "clu")
	}
}

func TestClusterReconcileCreate_AlreadyExists_AdoptsStatus(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
	})

	mock := &ske.DefaultAPIServiceMock{}
	createFn := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		body := []byte(`{"code":"AlreadyExists","message":"already exists: cluster with name \"clu\""}`)
		return nil, oapierror.NewErrorWithBody(409, "Conflict", body, nil)
	}
	mock.CreateOrUpdateClusterExecuteMock = &createFn
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != clusterPollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, clusterPollInterval)
	}

	got := getCluster(t, r.Client, cluster.Name)
	if got.Status.ClusterName != "clu" {
		t.Errorf("Status.ClusterName = %q, want %q", got.Status.ClusterName, "clu")
	}
	if got.Status.State != string(ske.CLUSTERSTATUSSTATE_STATE_HEALTHY) {
		t.Errorf("Status.State = %q, want %q", got.Status.State, ske.CLUSTERSTATUSSTATE_STATE_HEALTHY)
	}
}

func TestClusterReconcileCreate_Adopted_Success(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		c.Spec.ExistingClusterName = utils.Ptr("clu")
	})

	mock := &ske.DefaultAPIServiceMock{}
	createCalled := false
	createFn := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		createCalled = true
		return nil, nil
	}
	mock.CreateOrUpdateClusterExecuteMock = &createFn
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	if _, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if createCalled {
		t.Error("CreateOrUpdateCluster was called for an adopted cluster")
	}

	got := getCluster(t, r.Client, cluster.Name)
	if got.Status.ClusterName != "clu" {
		t.Errorf("Status.ClusterName = %q, want %q", got.Status.ClusterName, "clu")
	}
}

func TestClusterReconcileCreate_Adopted_NotFound(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		c.Spec.ExistingClusterName = utils.Ptr("clu")
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return nil, oapierror.NewError(404, "not found")
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getCluster(t, r.Client, cluster.Name)
	if got.Status.ClusterName != "" {
		t.Errorf("Status.ClusterName = %q, want empty", got.Status.ClusterName)
	}
}

// --- existing: state handling ---

func TestClusterReconcileExisting_Transitional(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
		c.Status.ClusterName = "clu"
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		state := ske.CLUSTERSTATUSSTATE_STATE_RECONCILING
		resp := healthyClusterResponse()
		resp.Status.Aggregated = &state
		return resp, nil
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != clusterPollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, clusterPollInterval)
	}
}

func TestClusterReconcileExisting_Unhealthy(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
		c.Status.ClusterName = "clu"
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		state := ske.CLUSTERSTATUSSTATE_STATE_UNHEALTHY
		resp := healthyClusterResponse()
		resp.Status.Aggregated = &state
		return resp, nil
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getCluster(t, r.Client, cluster.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Reason != "Unhealthy" {
		t.Errorf("Ready condition = %+v, want reason Unhealthy", cond)
	}
}

func TestClusterReconcileExisting_Healthy(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
		c.Status.ClusterName = "clu"
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != resyncPeriod {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, resyncPeriod)
	}

	got := getCluster(t, r.Client, cluster.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Healthy" {
		t.Errorf("Ready condition = %+v, want True/Healthy", cond)
	}
}

// --- drift ---

func TestClusterReconcileDrift_VersionChange_RequeuesWithoutStatusUpdate(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
		c.Status.ClusterName = "clu"
		c.Spec.KubernetesVersion = "1.30.0"
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn
	updated := false
	updateFn := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		updated = true
		return healthyClusterResponse(), nil
	}
	mock.CreateOrUpdateClusterExecuteMock = &updateFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !updated {
		t.Error("CreateOrUpdateCluster was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestClusterReconcileExisting_Adopted_SkipsDrift(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		c.Spec.ExistingClusterName = utils.Ptr("clu")
		c.Status.ClusterName = "clu"
		c.Spec.KubernetesVersion = "1.30.0"
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn
	failIfUpdateCalled := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		t.Error("CreateOrUpdateCluster was called for an adopted cluster")
		return nil, nil
	}
	mock.CreateOrUpdateClusterExecuteMock = &failIfUpdateCalled

	r := newClusterReconciler(t, mock, cluster)
	if _, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}

// --- delete ---

func TestClusterReconcileDelete_Adopted_NeverDeletes(t *testing.T) {
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		c.Spec.ExistingClusterName = utils.Ptr("clu")
		c.Status.ClusterName = "clu"
	})

	mock := &ske.DefaultAPIServiceMock{}
	failIfDeleteCalled := func(r ske.ApiDeleteClusterRequest) (map[string]interface{}, error) {
		t.Error("DeleteCluster was called for an adopted cluster")
		return nil, nil
	}
	mock.DeleteClusterExecuteMock = &failIfDeleteCalled

	r := newClusterReconciler(t, mock, cluster)
	cluster.DeletionTimestamp = &metav1.Time{}
	if _, err := r.reconcileDelete(context.Background(), cluster); err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
}

func TestClusterReconcileDelete_GetThenDelete(t *testing.T) {
	now := metav1.Now()
	cluster := newTestCluster("clu", func(c *computev1alpha1.Cluster) {
		controllerutil.AddFinalizer(c, clusterFinalizer)
		c.Status.ClusterName = "clu"
		c.DeletionTimestamp = &now
	})

	mock := &ske.DefaultAPIServiceMock{}
	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		return healthyClusterResponse(), nil
	}
	mock.GetClusterExecuteMock = &getFn
	deleted := false
	deleteFn := func(r ske.ApiDeleteClusterRequest) (map[string]interface{}, error) {
		deleted = true
		return nil, nil
	}
	mock.DeleteClusterExecuteMock = &deleteFn

	r := newClusterReconciler(t, mock, cluster)
	res, err := r.Reconcile(context.Background(), clusterReconcileRequest(cluster))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !deleted {
		t.Error("DeleteCluster was not called")
	}
	if res.RequeueAfter != clusterPollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, clusterPollInterval)
	}
}

// --- helpers ---

func TestClusterName(t *testing.T) {
	c := newTestCluster("clu", nil)
	if got := clusterName(c); got != "clu" {
		t.Errorf("clusterName() = %q, want %q", got, "clu")
	}
	c.Spec.Name = "explicit"
	if got := clusterName(c); got != "explicit" {
		t.Errorf("clusterName() = %q, want %q", got, "explicit")
	}
}
