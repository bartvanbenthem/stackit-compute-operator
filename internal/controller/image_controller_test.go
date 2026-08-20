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

const (
	testImgID     = "66666666-6666-6666-6666-666666666666"
	testUploadURL = "https://upload.example.com/put-image-bytes"
)

func newTestImage(name string, mutate func(*computev1alpha1.Image)) *computev1alpha1.Image {
	image := &computev1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: computev1alpha1.ImageSpec{
			ProjectId:  testProjectID,
			Region:     "eu01",
			DiskFormat: "qcow2",
		},
	}
	if mutate != nil {
		mutate(image)
	}
	return image
}

func newImageReconciler(t *testing.T, mock *iaas.DefaultAPIServiceMock, objs ...client.Object) *ImageReconciler {
	t.Helper()
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithStatusSubresource(&computev1alpha1.Image{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return &ImageReconciler{
		Client:        builder.Build(),
		Scheme:        newTestScheme(t),
		StackitClient: &iaas.APIClient{DefaultAPI: mock},
	}
}

func getImage(t *testing.T, c client.Client, name string) *computev1alpha1.Image {
	t.Helper()
	got := &computev1alpha1.Image{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, got); err != nil {
		t.Fatalf("getting image: %v", err)
	}
	return got
}

func imageReconcileRequest(image *computev1alpha1.Image) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: image.Name, Namespace: image.Namespace}}
}

// --- finalizer wiring ---

func TestImageReconcile_AdoptedNeverAddsFinalizer(t *testing.T) {
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		i.Spec.ExistingID = utils.Ptr(testImgID)
	})
	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("AVAILABLE")}, nil
	}
	mock.GetImageExecuteMock = &getFn

	r := newImageReconciler(t, mock, image)
	if _, err := r.Reconcile(context.Background(), imageReconcileRequest(image)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	got := getImage(t, r.Client, image.Name)
	if controllerutil.ContainsFinalizer(got, imageFinalizer) {
		t.Errorf("finalizer added on adopted image, got finalizers %v", got.Finalizers)
	}
}

// --- create ---

func TestImageReconcileCreate_Success(t *testing.T) {
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		controllerutil.AddFinalizer(i, imageFinalizer)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	created := false
	createFn := func(r iaas.ApiCreateImageRequest) (*iaas.ImageCreateResponse, error) {
		created = true
		return &iaas.ImageCreateResponse{Id: testImgID, UploadUrl: testUploadURL}, nil
	}
	mock.CreateImageExecuteMock = &createFn

	r := newImageReconciler(t, mock, image)
	res, err := r.Reconcile(context.Background(), imageReconcileRequest(image))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !created {
		t.Error("CreateImage was not called")
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getImage(t, r.Client, image.Name)
	if got.Status.ImageId != testImgID {
		t.Errorf("Status.ImageId = %q, want %q", got.Status.ImageId, testImgID)
	}
	if got.Status.UploadUrl != testUploadURL {
		t.Errorf("Status.UploadUrl = %q, want %q", got.Status.UploadUrl, testUploadURL)
	}
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != "AwaitingUpload" {
		t.Errorf("Ready condition = %+v, want False/AwaitingUpload", cond)
	}
}

func TestImageReconcileCreate_Adopted_NotFound(t *testing.T) {
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		i.Spec.ExistingID = utils.Ptr(testImgID)
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return nil, oapierror.NewError(404, "not found")
	}
	mock.GetImageExecuteMock = &getFn

	r := newImageReconciler(t, mock, image)
	res, err := r.Reconcile(context.Background(), imageReconcileRequest(image))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getImage(t, r.Client, image.Name)
	if got.Status.ImageId != "" {
		t.Errorf("Status.ImageId = %q, want empty", got.Status.ImageId)
	}
}

// --- existing: state handling ---

func TestImageReconcileExisting_CreatingMapsToAwaitingUpload(t *testing.T) {
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		controllerutil.AddFinalizer(i, imageFinalizer)
		i.Status.ImageId = testImgID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("CREATING")}, nil
	}
	mock.GetImageExecuteMock = &getFn

	r := newImageReconciler(t, mock, image)
	res, err := r.Reconcile(context.Background(), imageReconcileRequest(image))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != errorInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, errorInterval)
	}

	got := getImage(t, r.Client, image.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Reason != "AwaitingUpload" {
		t.Errorf("Ready condition = %+v, want reason AwaitingUpload", cond)
	}
}

func TestImageReconcileExisting_Available(t *testing.T) {
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		controllerutil.AddFinalizer(i, imageFinalizer)
		i.Status.ImageId = testImgID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("AVAILABLE")}, nil
	}
	mock.GetImageExecuteMock = &getFn

	r := newImageReconciler(t, mock, image)
	res, err := r.Reconcile(context.Background(), imageReconcileRequest(image))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if res.RequeueAfter != resyncPeriod {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, resyncPeriod)
	}

	got := getImage(t, r.Client, image.Name)
	cond := findReadyCondition(got.Status.Conditions)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.Reason != "Available" {
		t.Errorf("Ready condition = %+v, want True/Available", cond)
	}
}

// --- drift ---

func TestImageReconcileDrift_ProtectedChange(t *testing.T) {
	protected := true
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		controllerutil.AddFinalizer(i, imageFinalizer)
		i.Status.ImageId = testImgID
		i.Spec.Protected = &protected
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("AVAILABLE"), Protected: utils.Ptr(false)}, nil
	}
	mock.GetImageExecuteMock = &getFn
	updated := false
	updateFn := func(r iaas.ApiUpdateImageRequest) (*iaas.Image, error) {
		updated = true
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("AVAILABLE")}, nil
	}
	mock.UpdateImageExecuteMock = &updateFn

	r := newImageReconciler(t, mock, image)
	res, err := r.Reconcile(context.Background(), imageReconcileRequest(image))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !updated {
		t.Error("UpdateImage was not called")
	}
	if !res.Requeue {
		t.Errorf("Result = %+v, want Requeue=true", res)
	}
}

func TestImageReconcileExisting_Adopted_SkipsDrift(t *testing.T) {
	protected := true
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		i.Spec.ExistingID = utils.Ptr(testImgID)
		i.Status.ImageId = testImgID
		i.Spec.Protected = &protected
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("AVAILABLE"), Protected: utils.Ptr(false)}, nil
	}
	mock.GetImageExecuteMock = &getFn
	failIfUpdateCalled := func(r iaas.ApiUpdateImageRequest) (*iaas.Image, error) {
		t.Error("UpdateImage was called for an adopted image")
		return nil, nil
	}
	mock.UpdateImageExecuteMock = &failIfUpdateCalled

	r := newImageReconciler(t, mock, image)
	if _, err := r.Reconcile(context.Background(), imageReconcileRequest(image)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}

// --- delete ---

func TestImageReconcileDelete_Adopted_NeverDeletes(t *testing.T) {
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		i.Spec.ExistingID = utils.Ptr(testImgID)
		i.Status.ImageId = testImgID
	})

	mock := &iaas.DefaultAPIServiceMock{}
	failIfDeleteCalled := func(r iaas.ApiDeleteImageRequest) error {
		t.Error("DeleteImage was called for an adopted image")
		return nil
	}
	mock.DeleteImageExecuteMock = &failIfDeleteCalled

	r := newImageReconciler(t, mock, image)
	image.DeletionTimestamp = &metav1.Time{}
	if _, err := r.reconcileDelete(context.Background(), image); err != nil {
		t.Fatalf("reconcileDelete() error = %v", err)
	}
}

func TestImageReconcileDelete_GetThenDelete(t *testing.T) {
	now := metav1.Now()
	image := newTestImage("img", func(i *computev1alpha1.Image) {
		controllerutil.AddFinalizer(i, imageFinalizer)
		i.Status.ImageId = testImgID
		i.DeletionTimestamp = &now
	})

	mock := &iaas.DefaultAPIServiceMock{}
	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		return &iaas.Image{Name: "img", DiskFormat: "qcow2", Status: utils.Ptr("AVAILABLE")}, nil
	}
	mock.GetImageExecuteMock = &getFn
	deleted := false
	deleteFn := func(r iaas.ApiDeleteImageRequest) error {
		deleted = true
		return nil
	}
	mock.DeleteImageExecuteMock = &deleteFn

	r := newImageReconciler(t, mock, image)
	res, err := r.Reconcile(context.Background(), imageReconcileRequest(image))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !deleted {
		t.Error("DeleteImage was not called")
	}
	if res.RequeueAfter != pollInterval {
		t.Errorf("RequeueAfter = %v, want %v", res.RequeueAfter, pollInterval)
	}
}

// --- helpers ---

func TestImageName(t *testing.T) {
	i := newTestImage("img", nil)
	if got := imageName(i); got != "img" {
		t.Errorf("imageName() = %q, want %q", got, "img")
	}
	i.Spec.Name = "explicit"
	if got := imageName(i); got != "explicit" {
		t.Errorf("imageName() = %q, want %q", got, "explicit")
	}
}
