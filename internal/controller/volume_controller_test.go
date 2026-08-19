package controller

import (
	"context"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	computev1alpha1 "github.com/bartvanbenthem/stackit-vm-operator/api/v1alpha1"
)

const testVolumeID = "55555555-5555-5555-5555-555555555555"

func newTestVolume(name string, mutate func(*computev1alpha1.Volume)) *computev1alpha1.Volume {
	volume := &computev1alpha1.Volume{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: computev1alpha1.VolumeSpec{
			ProjectId:        testProjectID,
			Region:           "eu01",
			AvailabilityZone: "eu01-1",
			Size:             32,
		},
	}
	if mutate != nil {
		mutate(volume)
	}
	return volume
}

func newVolumeReconciler(t *testing.T, mock *iaas.DefaultAPIServiceMock, objs ...client.Object) *VolumeReconciler {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(&computev1alpha1.Volume{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return &VolumeReconciler{
		Client:        builder.Build(),
		Scheme:        newTestScheme(t),
		StackitClient: &iaas.APIClient{DefaultAPI: mock},
	}
}

func getVolume(t *testing.T, c client.Client, name string) *computev1alpha1.Volume {
	t.Helper()
	got := &computev1alpha1.Volume{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
		t.Fatalf("getting volume: %v", err)
	}
	return got
}

func volumeReconcileRequest(volume *computev1alpha1.Volume) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: volume.Name, Namespace: volume.Namespace}}
}

func findReadyCondition(conditions []metav1.Condition) *metav1.Condition {
	return meta.FindStatusCondition(conditions, readyConditionType)
}

// --- finalizer wiring ---

func TestVolumeReconcile_AddsFinalizerWhenOwned(t *testing.T) {
	volume := newTestVolume("vol", nil)
	r := newVolumeReconciler(t, &iaas.DefaultAPIServiceMock{}, volume)

	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}

	got := getVolume(t, r.Client, volume.Name)
	if !controllerutil.ContainsFinalizer(got, volumeFinalizer) {
		t.Errorf("finalizer not added, got finalizers %v", got.Finalizers)
	}
}

func TestVolumeReconcile_AdoptedNeverAddsFinalizer(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		v.Spec.ExistingID = utils.Ptr(testVolumeID)
	})
	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("AVAILABLE")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn

	r := newVolumeReconciler(t, mock, volume)
	if _, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	got := getVolume(t, r.Client, volume.Name)
	if controllerutil.ContainsFinalizer(got, volumeFinalizer) {
		t.Errorf("finalizer added on adopted volume, got finalizers %v", got.Finalizers)
	}
}

// --- create ---

func TestVolumeReconcileCreate_Success(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		controllerutil.AddFinalizer(v, volumeFinalizer)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateVolumeRequest) (*iaas.Volume, error) {
		created = true
		return &iaas.Volume{Id: utils.Ptr(testVolumeID), Status: utils.Ptr("CREATING"), AvailabilityZone: "eu01-1"}, nil
	}
	mock.CreateVolumeExecuteMock = &createFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateVolume was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}

	got := getVolume(t, r.Client, volume.Name)
	if got.Status.VolumeId != testVolumeID {
		t.Errorf("Status.VolumeId = %q, want %q", got.Status.VolumeId, testVolumeID)
	}
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "Creating" {
		t.Errorf("Ready condition = %+v, want False/Creating", cond)
	}
}

func TestVolumeReconcileCreate_Adopted_Success(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		v.Spec.ExistingID = utils.Ptr(testVolumeID)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getCalled, createCalled := false, false
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		getCalled = true
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("AVAILABLE"), Size: utils.Ptr(int64(64))}, nil
	}
	mock.GetVolumeExecuteMock = &getFn
	createFn := func(r iaas.ApiCreateVolumeRequest) (*iaas.Volume, error) {
		createCalled = true
		return nil, nil
	}
	mock.CreateVolumeExecuteMock = &createFn

	r := newVolumeReconciler(t, mock, volume)
	if _, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !getCalled {
		t.Error("GetVolume was not called")
	}
	if createCalled {
		t.Error("CreateVolume was called for an adopted volume")
	}

	got := getVolume(t, r.Client, volume.Name)
	if got.Status.VolumeId != testVolumeID {
		t.Errorf("Status.VolumeId = %q, want %q", got.Status.VolumeId, testVolumeID)
	}
	if got.Status.Size != 64 {
		t.Errorf("Status.Size = %d, want 64", got.Status.Size)
	}
}

func TestVolumeReconcileCreate_Adopted_NotFound(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		v.Spec.ExistingID = utils.Ptr(testVolumeID)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return nil, oapierror.NewError(404, "not found")
	}
	mock.GetVolumeExecuteMock = &getFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getVolume(t, r.Client, volume.Name)
	if got.Status.VolumeId != "" {
		t.Errorf("Status.VolumeId = %q, want empty", got.Status.VolumeId)
	}
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "NotFound" {
		t.Errorf("Ready condition = %+v, want False/NotFound", cond)
	}
}

// --- existing: state handling ---

func TestVolumeReconcileExisting_Transitional(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		controllerutil.AddFinalizer(v, volumeFinalizer)
		v.Status.VolumeId = testVolumeID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("RESIZING")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}
}

func TestVolumeReconcileExisting_ErrorPrefixedState(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		controllerutil.AddFinalizer(v, volumeFinalizer)
		v.Status.VolumeId = testVolumeID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("ERROR_RESIZING")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getVolume(t, r.Client, volume.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Reason != "Error" {
		t.Errorf("Ready condition = %+v, want reason Error", cond)
	}
}

func TestVolumeReconcileExisting_Available(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		controllerutil.AddFinalizer(v, volumeFinalizer)
		v.Status.VolumeId = testVolumeID
		v.Spec.Size = 32
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("AVAILABLE"), Size: utils.Ptr(int64(32)), Name: utils.Ptr("vol")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != resyncPeriod {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, resyncPeriod)
	}

	got := getVolume(t, r.Client, volume.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Available" {
		t.Errorf("Ready condition = %+v, want True/Available", cond)
	}
}

// --- drift ---

func TestVolumeReconcileDrift_Resize(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		controllerutil.AddFinalizer(v, volumeFinalizer)
		v.Status.VolumeId = testVolumeID
		v.Spec.Size = 64
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("AVAILABLE"), Size: utils.Ptr(int64(32)), Name: utils.Ptr("vol")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn
	resized := false
	resizeFn := func(r iaas.ApiResizeVolumeRequest) error {
		resized = true
		return nil
	}
	mock.ResizeVolumeExecuteMock = &resizeFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !resized {
		t.Error("ResizeVolume was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestVolumeReconcileExisting_Adopted_SkipsDrift(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		v.Spec.ExistingID = utils.Ptr(testVolumeID)
		v.Status.VolumeId = testVolumeID
		v.Spec.Size = 999 // differs from observed; must not trigger a resize
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("AVAILABLE"), Size: utils.Ptr(int64(32)), Name: utils.Ptr("vol")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn
	failIfResizeCalled := func(r iaas.ApiResizeVolumeRequest) error {
		t.Error("ResizeVolume was called for an adopted volume")
		return nil
	}
	mock.ResizeVolumeExecuteMock = &failIfResizeCalled
	failIfUpdateCalled := func(r iaas.ApiUpdateVolumeRequest) (*iaas.Volume, error) {
		t.Error("UpdateVolume was called for an adopted volume")
		return nil, nil
	}
	mock.UpdateVolumeExecuteMock = &failIfUpdateCalled

	r := newVolumeReconciler(t, mock, volume)
	if _, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}

// --- delete ---

func TestVolumeReconcileDelete_NoFinalizerNoop(t *testing.T) {
	// Seeded without a DeletionTimestamp (the fake client refuses to seed an
	// object with a DeletionTimestamp but no finalizers - that combination
	// can't exist in a real cluster); mutated and reconciled directly,
	// mirroring server_controller_test.go's TestReconcileDelete_* pattern.
	volume := newTestVolume("vol", nil)

	r := newVolumeReconciler(t, &iaas.DefaultAPIServiceMock{}, volume)
	volume.DeletionTimestamp = &metav1.Time{}
	res, err := r.reconcileDelete(context.Background(), volume)
	if err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
	if res.Requeue || res.RequeueAfter != 0 {
		t.Errorf("Result = %+v, want empty", res)
	}
}

func TestVolumeReconcileDelete_Adopted_NeverDeletes(t *testing.T) {
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		v.Spec.ExistingID = utils.Ptr(testVolumeID)
		v.Status.VolumeId = testVolumeID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	failIfDeleteCalled := func(r iaas.ApiDeleteVolumeRequest) error {
		t.Error("DeleteVolume was called for an adopted volume")
		return nil
	}
	mock.DeleteVolumeExecuteMock = &failIfDeleteCalled

	r := newVolumeReconciler(t, mock, volume)
	volume.DeletionTimestamp = &metav1.Time{}
	if _, err := r.reconcileDelete(context.Background(), volume); err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
}

func TestVolumeReconcileDelete_GetThenDelete(t *testing.T) {
	now := metav1.Now()
	volume := newTestVolume("vol", func(v *computev1alpha1.Volume) {
		controllerutil.AddFinalizer(v, volumeFinalizer)
		v.Status.VolumeId = testVolumeID
		v.DeletionTimestamp = &now
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		return &iaas.Volume{AvailabilityZone: "eu01-1", Status: utils.Ptr("AVAILABLE")}, nil
	}
	mock.GetVolumeExecuteMock = &getFn
	deleted := false
	deleteFn := func(r iaas.ApiDeleteVolumeRequest) error {
		deleted = true
		return nil
	}
	mock.DeleteVolumeExecuteMock = &deleteFn

	r := newVolumeReconciler(t, mock, volume)
	res, err := r.Reconcile(context.Background(), volumeReconcileRequest(volume))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !deleted {
		t.Error("DeleteVolume was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}
}

// --- helpers ---

func TestVolumeName(t *testing.T) {
	v := newTestVolume("vol", nil)
	if got := volumeName(v); got != "vol" {
		t.Errorf("volumeName() = %q, want %q", got, "vol")
	}
	v.Spec.Name = "explicit"
	if got := volumeName(v); got != "explicit" {
		t.Errorf("volumeName() = %q, want %q", got, "explicit")
	}
}
