package controller

import (
	"context"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

const (
	testProjectID = "11111111-1111-1111-1111-111111111111"
	testImageID   = "22222222-2222-2222-2222-222222222222"
	testNetworkID = "33333333-3333-3333-3333-333333333333"
	testServerID  = "44444444-4444-4444-4444-444444444444"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := computev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return scheme
}

func newTestServer(name string, mutate func(*computev1alpha1.Server)) *computev1alpha1.Server {
	server := &computev1alpha1.Server{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: computev1alpha1.ServerSpec{
			ProjectId:   testProjectID,
			Region:      "eu01",
			MachineType: "c1.2",
			ImageId:     testImageID,
			NetworkId:   testNetworkID,
		},
	}
	if mutate != nil {
		mutate(server)
	}
	return server
}

func newReconciler(t *testing.T, mock *iaas.DefaultAPIServiceMock, objs ...client.Object) *ServerReconciler {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(&computev1alpha1.Server{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	fakeClient := builder.Build()

	return &ServerReconciler{
		Client:        fakeClient,
		Scheme:        newTestScheme(t),
		StackitClient: &iaas.APIClient{DefaultAPI: mock},
	}
}

func reconcileRequest(server *computev1alpha1.Server) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: server.Name, Namespace: server.Namespace}}
}

func getServer(t *testing.T, c client.Client, name string) *computev1alpha1.Server {
	t.Helper()
	got := &computev1alpha1.Server{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
		t.Fatalf("getting server: %v", err)
	}
	return got
}

func readyCondition(server *computev1alpha1.Server) *metav1.Condition {
	return meta.FindStatusCondition(server.Status.Conditions, readyConditionType)
}

// --- finalizer wiring ---

func TestReconcile_AddsFinalizer(t *testing.T) {
	server := newTestServer("srv", nil)
	r := newReconciler(t, &iaas.DefaultAPIServiceMock{}, server)

	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}

	got := getServer(t, r.Client, server.Name)
	if !controllerutil.ContainsFinalizer(got, serverFinalizer) {
		t.Errorf("finalizer not added, got finalizers %v", got.Finalizers)
	}
}

func TestReconcile_NotFoundIgnored(t *testing.T) {
	r := newReconciler(t, &iaas.DefaultAPIServiceMock{})

	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "missing", Namespace: "default"}})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("Result = %+v, want empty", res)
	}
}

// --- create ---

func TestReconcileCreate_Success(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		created = true
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Status:      utils.Ptr("CREATING"),
			MachineType: "c1.2",
		}, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateServer was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}

	got := getServer(t, r.Client, server.Name)
	if got.Status.ServerId != testServerID {
		t.Errorf("Status.ServerId = %q, want %q", got.Status.ServerId, testServerID)
	}
	cond := readyCondition(got)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Creating" {
		t.Errorf("Ready condition = %+v, want False/Creating", cond)
	}
}

func TestReconcileCreate_APIError(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		return nil, oapierror.NewError(500, "internal error")
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server)
	_, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err == nil {
		t.Fatal("Reconcile() error = nil, want error")
	}

	got := getServer(t, r.Client, server.Name)
	cond := readyCondition(got)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "CreateFailed" {
		t.Errorf("Ready condition = %+v, want False/CreateFailed", cond)
	}
}

// --- reference resolution ---

func TestReconcileCreate_ResolvesImageRefToID(t *testing.T) {
	image := &computev1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "my-image", Namespace: "default"},
		Status:     computev1alpha1.ImageStatus{ImageId: testImageID},
	}
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Spec.ImageId = ""
		s.Spec.ImageRef = &computev1alpha1.LocalObjectReference{Name: "my-image"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("CREATING"), MachineType: "c1.2"}, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server, image)
	_, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	got := getServer(t, r.Client, server.Name)
	cond := readyCondition(got)
	if cond == nil || cond.Reason != "Creating" {
		t.Errorf("Ready condition = %+v, want reason Creating (server should have been created)", cond)
	}
}

func TestReconcileCreate_WaitsForNetworkRefNotReady(t *testing.T) {
	network := &computev1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{Name: "my-network", Namespace: "default"},
		// Status.NetworkId intentionally empty: Network exists but isn't Ready yet.
	}
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Spec.NetworkId = ""
		s.Spec.NetworkRef = &computev1alpha1.LocalObjectReference{Name: "my-network"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		t.Error("CreateServer was called before the referenced Network was ready")
		return nil, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server, network)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}

	got := getServer(t, r.Client, server.Name)
	if got.Status.ServerId != "" {
		t.Errorf("Status.ServerId = %q, want empty", got.Status.ServerId)
	}
}

func TestReconcileCreate_RejectsBothImageIdAndImageRef(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		// ImageId is already set by newTestServer; also set ImageRef.
		s.Spec.ImageRef = &computev1alpha1.LocalObjectReference{Name: "my-image"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		t.Error("CreateServer was called despite an invalid reference")
		return nil, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("Result = %+v, want empty (no requeue on a permanent validation error)", res)
	}

	got := getServer(t, r.Client, server.Name)
	cond := readyCondition(got)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "InvalidReference" {
		t.Errorf("Ready condition = %+v, want False/InvalidReference", cond)
	}
}

// TestReconcileCreate_ResolvesBootVolumeRef verifies the controller-side
// half of "boot from an existing volume": that resolveBootVolumeRef reads
// the referenced Volume's status.volumeId and lets creation proceed. The
// resulting BootVolumeSource.Type=="volume" translation itself is a pure
// function covered directly by
// TestBuildCreatePayload_ResolvedBootVolumeID in internal/stackit, since
// the SDK's request builder keeps the payload it was given unexported and
// can't be inspected through the mock here.
func TestReconcileCreate_ResolvesBootVolumeRef(t *testing.T) {
	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: "my-boot-volume", Namespace: "default"},
		Status:     computev1alpha1.VolumeStatus{VolumeId: testVolumeID, State: "AVAILABLE"},
	}
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Spec.BootVolumeRef = &computev1alpha1.LocalObjectReference{Name: "my-boot-volume"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		created = true
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("CREATING"), MachineType: "c1.2"}, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server, volume)
	if _, err := r.Reconcile(context.Background(), reconcileRequest(server)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateServer was not called (boot volume ref should have resolved)")
	}
}

// TestReconcileCreate_BootVolumeRefWithoutImage verifies that
// spec.bootVolumeRef alone, with neither spec.imageId nor spec.imageRef
// set, is enough to create the server - per BootVolumeRef's doc comment in
// server_types.go, an existing boot Volume makes the image unnecessary.
func TestReconcileCreate_BootVolumeRefWithoutImage(t *testing.T) {
	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: "my-boot-volume", Namespace: "default"},
		Status:     computev1alpha1.VolumeStatus{VolumeId: testVolumeID, State: "AVAILABLE"},
	}
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Spec.ImageId = ""
		s.Spec.BootVolumeRef = &computev1alpha1.LocalObjectReference{Name: "my-boot-volume"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		created = true
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("CREATING"), MachineType: "c1.2"}, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server, volume)
	if _, err := r.Reconcile(context.Background(), reconcileRequest(server)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateServer was not called (bootVolumeRef alone should be enough without an image)")
	}
}

// TestReconcileCreate_WaitsForBootVolumeAvailable verifies that a
// bootVolumeRef with a VolumeId but a non-AVAILABLE state (e.g. still
// CREATING, or RESERVED by another in-flight create) is treated as
// not-ready: the controller must wait rather than call CreateServer, since
// STACKIT rejects "Volume is in wrong state" for anything but AVAILABLE.
func TestReconcileCreate_WaitsForBootVolumeAvailable(t *testing.T) {
	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: "my-boot-volume", Namespace: "default"},
		Status:     computev1alpha1.VolumeStatus{VolumeId: testVolumeID, State: "RESERVED"},
	}
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Spec.BootVolumeRef = &computev1alpha1.LocalObjectReference{Name: "my-boot-volume"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		created = true
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("CREATING"), MachineType: "c1.2"}, nil
	}
	mock.CreateServerExecuteMock = &createFn

	r := newReconciler(t, mock, server, volume)
	if _, err := r.Reconcile(context.Background(), reconcileRequest(server)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if created {
		t.Error("CreateServer was called while boot volume was RESERVED, not AVAILABLE")
	}
}

// --- existing: state handling ---

func TestReconcileExisting_Transitional(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Status:      utils.Ptr("RESIZING"),
			MachineType: "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}

	got := getServer(t, r.Client, server.Name)
	cond := readyCondition(got)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Transitioning" {
		t.Errorf("Ready condition = %+v, want False/Transitioning", cond)
	}
}

func TestReconcileExisting_Error(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:           utils.Ptr(testServerID),
			Status:       utils.Ptr("ERROR"),
			ErrorMessage: utils.Ptr("out of quota"),
			MachineType:  "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getServer(t, r.Client, server.Name)
	cond := readyCondition(got)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Error" || cond.Message != "out of quota" {
		t.Errorf("Ready condition = %+v, want False/Error \"out of quota\"", cond)
	}
}

func TestReconcileExisting_ActiveStable(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Name:        server.Name,
			Status:      utils.Ptr("ACTIVE"),
			PowerStatus: utils.Ptr("RUNNING"),
			MachineType: "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != resyncPeriod {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, resyncPeriod)
	}

	got := getServer(t, r.Client, server.Name)
	cond := readyCondition(got)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Active" {
		t.Errorf("Ready condition = %+v, want True/Active", cond)
	}
}

func TestReconcileExisting_NotFoundTriggersRecreate(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return nil, oapierror.NewError(404, "not found")
	}
	mock.GetServerExecuteMock = &getFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}

	got := getServer(t, r.Client, server.Name)
	if got.Status.ServerId != "" {
		t.Errorf("Status.ServerId = %q, want empty (cleared for recreate)", got.Status.ServerId)
	}
}

// --- drift reconciliation ---

func TestReconcileDrift_Resize(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
		s.Spec.MachineType = "c1.4"
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Name:        server.Name,
			Status:      utils.Ptr("ACTIVE"),
			PowerStatus: utils.Ptr("RUNNING"),
			MachineType: "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn
	resized := false
	resizeFn := func(r iaas.ApiResizeServerRequest) error {
		resized = true
		return nil
	}
	mock.ResizeServerExecuteMock = &resizeFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !resized {
		t.Error("ResizeServer was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestReconcileDrift_StopWhenInactiveRequested(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
		s.Spec.PowerState = computev1alpha1.PowerStateInactive
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Name:        server.Name,
			Status:      utils.Ptr("ACTIVE"),
			PowerStatus: utils.Ptr("RUNNING"),
			MachineType: "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn
	stopped := false
	stopFn := func(r iaas.ApiStopServerRequest) error {
		stopped = true
		return nil
	}
	mock.StopServerExecuteMock = &stopFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !stopped {
		t.Error("StopServer was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestReconcileDrift_StartWhenActiveRequestedButStopped(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Name:        server.Name,
			Status:      utils.Ptr("INACTIVE"),
			PowerStatus: utils.Ptr("STOPPED"),
			MachineType: "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn
	started := false
	startFn := func(r iaas.ApiStartServerRequest) error {
		started = true
		return nil
	}
	mock.StartServerExecuteMock = &startFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !started {
		t.Error("StartServer was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestReconcileDrift_MetadataUpdate(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
		s.Spec.Labels = map[string]string{"team": "infra"}
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Name:        "old-name",
			Status:      utils.Ptr("ACTIVE"),
			PowerStatus: utils.Ptr("RUNNING"),
			MachineType: "c1.2",
			Labels:      map[string]interface{}{},
		}, nil
	}
	mock.GetServerExecuteMock = &getFn
	updated := false
	updateFn := func(r iaas.ApiUpdateServerRequest) (*iaas.Server, error) {
		updated = true
		return &iaas.Server{}, nil
	}
	mock.UpdateServerExecuteMock = &updateFn

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !updated {
		t.Error("UpdateServer was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestReconcileDrift_NoActionWhenInSync(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{
			Id:          utils.Ptr(testServerID),
			Name:        server.Name,
			Status:      utils.Ptr("ACTIVE"),
			PowerStatus: utils.Ptr("RUNNING"),
			MachineType: "c1.2",
		}, nil
	}
	mock.GetServerExecuteMock = &getFn
	mock.ResizeServerExecuteMock = failIfCalledResize(t)
	mock.StopServerExecuteMock = failIfCalledStop(t)
	mock.StartServerExecuteMock = failIfCalledStart(t)
	mock.UpdateServerExecuteMock = failIfCalledUpdate(t)

	r := newReconciler(t, mock, server)
	res, err := r.Reconcile(context.Background(), reconcileRequest(server))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != resyncPeriod {
		t.Errorf("RequeueAfter = %v, want %v (no drift action)", res.RequeueAfter, resyncPeriod)
	}
}

func failIfCalledResize(t *testing.T) *func(iaas.ApiResizeServerRequest) error {
	t.Helper()
	fn := func(r iaas.ApiResizeServerRequest) error {
		t.Error("ResizeServer should not have been called")
		return nil
	}
	return &fn
}

func failIfCalledStop(t *testing.T) *func(iaas.ApiStopServerRequest) error {
	t.Helper()
	fn := func(r iaas.ApiStopServerRequest) error {
		t.Error("StopServer should not have been called")
		return nil
	}
	return &fn
}

func failIfCalledStart(t *testing.T) *func(iaas.ApiStartServerRequest) error {
	t.Helper()
	fn := func(r iaas.ApiStartServerRequest) error {
		t.Error("StartServer should not have been called")
		return nil
	}
	return &fn
}

func failIfCalledUpdate(t *testing.T) *func(iaas.ApiUpdateServerRequest) (*iaas.Server, error) {
	t.Helper()
	fn := func(r iaas.ApiUpdateServerRequest) (*iaas.Server, error) {
		t.Error("UpdateServer should not have been called")
		return nil, nil
	}
	return &fn
}

// --- delete ---

func TestReconcileDelete_NoFinalizerIsNoop(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		s.DeletionTimestamp = &metav1.Time{}
	})
	// Objects with a non-zero deletion timestamp and no finalizers can't be
	// created through the fake client's normal path (it would delete them
	// immediately), so build one that already has the finalizer removed by
	// constructing state directly via reconcileDelete.
	r := newReconciler(t, &iaas.DefaultAPIServiceMock{})

	res, err := r.reconcileDelete(context.Background(), server)
	if err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("Result = %+v, want empty", res)
	}
}

func TestReconcileDelete_NoServerIdRemovesFinalizer(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
	})
	r := newReconciler(t, &iaas.DefaultAPIServiceMock{}, server)

	res, err := r.reconcileDelete(context.Background(), server)
	if err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("Result = %+v, want empty", res)
	}

	got := getServer(t, r.Client, server.Name)
	if controllerutil.ContainsFinalizer(got, serverFinalizer) {
		t.Error("finalizer still present, want removed")
	}
}

func TestReconcileDelete_AlreadyGoneRemovesFinalizer(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return nil, oapierror.NewError(404, "not found")
	}
	mock.GetServerExecuteMock = &getFn

	r := newReconciler(t, mock, server)
	res, err := r.reconcileDelete(context.Background(), server)
	if err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("Result = %+v, want empty", res)
	}

	got := getServer(t, r.Client, server.Name)
	if controllerutil.ContainsFinalizer(got, serverFinalizer) {
		t.Error("finalizer still present, want removed")
	}
}

func TestReconcileDelete_TriggersDeleteOnce(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("ACTIVE")}, nil
	}
	mock.GetServerExecuteMock = &getFn
	deleted := false
	deleteFn := func(r iaas.ApiDeleteServerRequest) error {
		deleted = true
		return nil
	}
	mock.DeleteServerExecuteMock = &deleteFn

	r := newReconciler(t, mock, server)
	res, err := r.reconcileDelete(context.Background(), server)
	if err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
	if !deleted {
		t.Error("DeleteServer was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}

	got := getServer(t, r.Client, server.Name)
	if !controllerutil.ContainsFinalizer(got, serverFinalizer) {
		t.Error("finalizer removed too early, should wait until STACKIT confirms deletion")
	}
}

func TestReconcileDelete_SkipsDeleteWhenAlreadyDeleting(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("DELETING")}, nil
	}
	mock.GetServerExecuteMock = &getFn
	mock.DeleteServerExecuteMock = failIfCalledDelete(t)

	r := newReconciler(t, mock, server)
	res, err := r.reconcileDelete(context.Background(), server)
	if err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}
}

func TestReconcileDelete_GetServerErrorPropagates(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return nil, oapierror.NewError(500, "internal error")
	}
	mock.GetServerExecuteMock = &getFn
	mock.DeleteServerExecuteMock = failIfCalledDelete(t)

	r := newReconciler(t, mock, server)
	if _, err := r.reconcileDelete(context.Background(), server); err == nil {
		t.Fatal("reconcileDelete() error = nil, want error")
	}

	got := getServer(t, r.Client, server.Name)
	if !controllerutil.ContainsFinalizer(got, serverFinalizer) {
		t.Error("finalizer removed despite a failed pre-delete status check")
	}
}

func TestReconcileDelete_DeleteServerErrorPropagates(t *testing.T) {
	server := newTestServer("srv", func(s *computev1alpha1.Server) {
		controllerutil.AddFinalizer(s, serverFinalizer)
		s.Status.ServerId = testServerID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		return &iaas.Server{Id: utils.Ptr(testServerID), Status: utils.Ptr("ACTIVE")}, nil
	}
	mock.GetServerExecuteMock = &getFn
	deleteFn := func(r iaas.ApiDeleteServerRequest) error {
		return oapierror.NewError(500, "internal error")
	}
	mock.DeleteServerExecuteMock = &deleteFn

	r := newReconciler(t, mock, server)
	if _, err := r.reconcileDelete(context.Background(), server); err == nil {
		t.Fatal("reconcileDelete() error = nil, want error")
	}

	got := getServer(t, r.Client, server.Name)
	if !controllerutil.ContainsFinalizer(got, serverFinalizer) {
		t.Error("finalizer removed despite a failed DeleteServer call")
	}
}

func failIfCalledDelete(t *testing.T) *func(iaas.ApiDeleteServerRequest) error {
	t.Helper()
	fn := func(r iaas.ApiDeleteServerRequest) error {
		t.Error("DeleteServer should not have been called")
		return nil
	}
	return &fn
}

// --- helper function unit tests ---

func TestServerName(t *testing.T) {
	withSpecName := newTestServer("k8s-name", func(s *computev1alpha1.Server) {
		s.Spec.Name = "stackit-name"
	})
	if got := serverName(withSpecName); got != "stackit-name" {
		t.Errorf("serverName() = %q, want %q", got, "stackit-name")
	}

	withoutSpecName := newTestServer("k8s-name", nil)
	if got := serverName(withoutSpecName); got != "k8s-name" {
		t.Errorf("serverName() = %q, want %q", got, "k8s-name")
	}
}

func TestLabelsEqual(t *testing.T) {
	tests := []struct {
		name    string
		current map[string]interface{}
		desired map[string]string
		want    bool
	}{
		{"both empty", map[string]interface{}{}, map[string]string{}, true},
		{"desired subset of current", map[string]interface{}{"a": "b", "stackit-managed": "true"}, map[string]string{"a": "b"}, true},
		{"missing key", map[string]interface{}{"a": "b"}, map[string]string{"a": "b", "c": "d"}, false},
		{"value mismatch", map[string]interface{}{"a": "b"}, map[string]string{"a": "c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelsEqual(tt.current, tt.desired); got != tt.want {
				t.Errorf("labelsEqual(%v, %v) = %v, want %v", tt.current, tt.desired, got, tt.want)
			}
		})
	}
}

func TestApplyServerStatus(t *testing.T) {
	server := newTestServer("srv", nil)
	server.Generation = 3

	current := &iaas.Server{
		Status:      utils.Ptr("ACTIVE"),
		PowerStatus: utils.Ptr("RUNNING"),
		MachineType: "c1.4",
		Nics: []iaas.ServerNetwork{
			{
				NetworkId: testNetworkID,
				Ipv4:      utils.Ptr("10.0.0.5"),
				Mac:       "aa:bb:cc:dd:ee:ff",
			},
		},
	}

	r := &ServerReconciler{}
	r.applyServerStatus(server, current)

	if server.Status.State != "ACTIVE" {
		t.Errorf("State = %q, want ACTIVE", server.Status.State)
	}
	if server.Status.PowerStatus != "RUNNING" {
		t.Errorf("PowerStatus = %q, want RUNNING", server.Status.PowerStatus)
	}
	if server.Status.MachineType != "c1.4" {
		t.Errorf("MachineType = %q, want c1.4", server.Status.MachineType)
	}
	if server.Status.ObservedGeneration != 3 {
		t.Errorf("ObservedGeneration = %d, want 3", server.Status.ObservedGeneration)
	}
	if len(server.Status.Nics) != 1 || server.Status.Nics[0].Ipv4 != "10.0.0.5" {
		t.Errorf("Nics = %+v, want one NIC with Ipv4 10.0.0.5", server.Status.Nics)
	}
}
