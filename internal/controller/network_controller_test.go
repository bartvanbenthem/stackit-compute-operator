package controller

import (
	"context"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

const testNetID = "77777777-7777-7777-7777-777777777777"

func newTestNetwork(name string, mutate func(*computev1alpha1.Network)) *computev1alpha1.Network {
	network := &computev1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: computev1alpha1.NetworkSpec{
			ProjectId: testProjectID,
			Region:    "eu01",
		},
	}
	if mutate != nil {
		mutate(network)
	}
	return network
}

func newNetworkReconciler(t *testing.T, mock *iaas.DefaultAPIServiceMock, objs ...client.Object) *NetworkReconciler {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(&computev1alpha1.Network{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return &NetworkReconciler{
		Client:        builder.Build(),
		Scheme:        newTestScheme(t),
		StackitClient: &iaas.APIClient{DefaultAPI: mock},
	}
}

func getNetwork(t *testing.T, c client.Client, name string) *computev1alpha1.Network {
	t.Helper()
	got := &computev1alpha1.Network{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
		t.Fatalf("getting network: %v", err)
	}
	return got
}

func networkReconcileRequest(network *computev1alpha1.Network) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: network.Name, Namespace: network.Namespace}}
}

// --- finalizer wiring ---

func TestNetworkReconcile_AdoptedNeverAddsFinalizer(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		n.Spec.ExistingID = utils.Ptr(testNetID)
	})
	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn

	r := newNetworkReconciler(t, mock, network)
	if _, err := r.Reconcile(context.Background(), networkReconcileRequest(network)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	got := getNetwork(t, r.Client, network.Name)
	if controllerutil.ContainsFinalizer(got, networkFinalizer) {
		t.Errorf("finalizer added on adopted network, got finalizers %v", got.Finalizers)
	}
}

// --- create ---

func TestNetworkReconcileCreate_Success(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		controllerutil.AddFinalizer(n, networkFinalizer)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateNetworkRequest) (*iaas.Network, error) {
		created = true
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATING"}, nil
	}
	mock.CreateNetworkExecuteMock = &createFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateNetwork was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}

	got := getNetwork(t, r.Client, network.Name)
	if got.Status.NetworkId != testNetID {
		t.Errorf("Status.NetworkId = %q, want %q", got.Status.NetworkId, testNetID)
	}
}

func TestNetworkReconcileCreate_Adopted_Success(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		n.Spec.ExistingID = utils.Ptr(testNetID)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	createCalled := false
	createFn := func(r iaas.ApiCreateNetworkRequest) (*iaas.Network, error) {
		createCalled = true
		return nil, nil
	}
	mock.CreateNetworkExecuteMock = &createFn
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn

	r := newNetworkReconciler(t, mock, network)
	if _, err := r.Reconcile(context.Background(), networkReconcileRequest(network)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if createCalled {
		t.Error("CreateNetwork was called for an adopted network")
	}

	got := getNetwork(t, r.Client, network.Name)
	if got.Status.NetworkId != testNetID {
		t.Errorf("Status.NetworkId = %q, want %q", got.Status.NetworkId, testNetID)
	}
}

func TestNetworkReconcileCreate_Adopted_NotFound(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		n.Spec.ExistingID = utils.Ptr(testNetID)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return nil, oapierror.NewError(404, "not found")
	}
	mock.GetNetworkExecuteMock = &getFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getNetwork(t, r.Client, network.Name)
	if got.Status.NetworkId != "" {
		t.Errorf("Status.NetworkId = %q, want empty", got.Status.NetworkId)
	}
}

// --- existing: state handling ---

func TestNetworkReconcileExisting_Transitional(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		controllerutil.AddFinalizer(n, networkFinalizer)
		n.Status.NetworkId = testNetID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "UPDATING"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}
}

func TestNetworkReconcileExisting_Failed(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		controllerutil.AddFinalizer(n, networkFinalizer)
		n.Status.NetworkId = testNetID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "FAILED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getNetwork(t, r.Client, network.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Reason != "Error" {
		t.Errorf("Ready condition = %+v, want reason Error", cond)
	}
}

func TestNetworkReconcileExisting_Created(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		controllerutil.AddFinalizer(n, networkFinalizer)
		n.Status.NetworkId = testNetID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != resyncPeriod {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, resyncPeriod)
	}

	got := getNetwork(t, r.Client, network.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Created" {
		t.Errorf("Ready condition = %+v, want True/Created", cond)
	}
}

// --- drift ---

func TestNetworkReconcileDrift_NameChange_RequeuesWithoutStatusUpdate(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		controllerutil.AddFinalizer(n, networkFinalizer)
		n.Status.NetworkId = testNetID
		n.Spec.Name = "renamed"
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn
	updated := false
	updateFn := func(r iaas.ApiPartialUpdateNetworkRequest) error {
		updated = true
		return nil
	}
	mock.PartialUpdateNetworkExecuteMock = &updateFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !updated {
		t.Error("PartialUpdateNetwork was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestNetworkReconcileExisting_Adopted_SkipsDrift(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		n.Spec.ExistingID = utils.Ptr(testNetID)
		n.Status.NetworkId = testNetID
		n.Spec.Name = "renamed"
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn
	failIfUpdateCalled := func(r iaas.ApiPartialUpdateNetworkRequest) error {
		t.Error("PartialUpdateNetwork was called for an adopted network")
		return nil
	}
	mock.PartialUpdateNetworkExecuteMock = &failIfUpdateCalled

	r := newNetworkReconciler(t, mock, network)
	if _, err := r.Reconcile(context.Background(), networkReconcileRequest(network)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}

// --- delete ---

func TestNetworkReconcileDelete_Adopted_NeverDeletes(t *testing.T) {
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		n.Spec.ExistingID = utils.Ptr(testNetID)
		n.Status.NetworkId = testNetID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	failIfDeleteCalled := func(r iaas.ApiDeleteNetworkRequest) error {
		t.Error("DeleteNetwork was called for an adopted network")
		return nil
	}
	mock.DeleteNetworkExecuteMock = &failIfDeleteCalled

	r := newNetworkReconciler(t, mock, network)
	network.DeletionTimestamp = &metav1.Time{}
	if _, err := r.reconcileDelete(context.Background(), network); err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
}

func TestNetworkReconcileDelete_GetThenDelete(t *testing.T) {
	now := metav1.Now()
	network := newTestNetwork("net", func(n *computev1alpha1.Network) {
		controllerutil.AddFinalizer(n, networkFinalizer)
		n.Status.NetworkId = testNetID
		n.DeletionTimestamp = &now
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		return &iaas.Network{Id: testNetID, Name: "net", Status: "CREATED"}, nil
	}
	mock.GetNetworkExecuteMock = &getFn
	deleted := false
	deleteFn := func(r iaas.ApiDeleteNetworkRequest) error {
		deleted = true
		return nil
	}
	mock.DeleteNetworkExecuteMock = &deleteFn

	r := newNetworkReconciler(t, mock, network)
	res, err := r.Reconcile(context.Background(), networkReconcileRequest(network))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !deleted {
		t.Error("DeleteNetwork was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}
}

// --- helpers ---

func TestNetworkName(t *testing.T) {
	n := newTestNetwork("net", nil)
	if got := networkName(n); got != "net" {
		t.Errorf("networkName() = %q, want %q", got, "net")
	}
	n.Spec.Name = "explicit"
	if got := networkName(n); got != "explicit" {
		t.Errorf("networkName() = %q, want %q", got, "explicit")
	}
}
