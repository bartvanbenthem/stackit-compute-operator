package stackit

import (
	"context"
	"errors"
	"testing"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

const testImageID = "66666666-6666-6666-6666-666666666666"

func TestBuildImageCreatePayload_Required(t *testing.T) {
	spec := computev1alpha1.ImageSpec{
		DiskFormat: "qcow2",
	}

	payload := BuildImageCreatePayload("my-image", spec)

	if payload.Name != "my-image" {
		t.Errorf("Name = %q, want %q", payload.Name, "my-image")
	}
	if payload.DiskFormat != spec.DiskFormat {
		t.Errorf("DiskFormat = %q, want %q", payload.DiskFormat, spec.DiskFormat)
	}
	if payload.MinDiskSize != nil {
		t.Errorf("MinDiskSize = %v, want nil", payload.MinDiskSize)
	}
	if payload.Checksum != nil {
		t.Errorf("Checksum = %v, want nil", payload.Checksum)
	}
	if payload.Config != nil {
		t.Errorf("Config = %v, want nil", payload.Config)
	}
}

func TestBuildImageCreatePayload_Optional(t *testing.T) {
	protected := true
	spec := computev1alpha1.ImageSpec{
		DiskFormat:  "qcow2",
		MinDiskSize: 10,
		MinRam:      512,
		Protected:   &protected,
		Labels:      map[string]string{"team": "infra"},
		Checksum: &computev1alpha1.ImageChecksumSpec{
			Algorithm: "sha512",
			Digest:    "deadbeef",
		},
		Config: &computev1alpha1.ImageConfigSpec{
			Architecture:    "x86",
			OperatingSystem: "linux",
			DiskBus:         "virtio",
			SecureBoot:      &protected,
		},
	}

	payload := BuildImageCreatePayload("my-image", spec)

	if payload.MinDiskSize == nil || *payload.MinDiskSize != spec.MinDiskSize {
		t.Errorf("MinDiskSize = %v, want %d", payload.MinDiskSize, spec.MinDiskSize)
	}
	if payload.Protected == nil || *payload.Protected != protected {
		t.Errorf("Protected = %v, want %v", payload.Protected, protected)
	}
	if payload.Labels["team"] != "infra" {
		t.Errorf("Labels = %v, want team=infra", payload.Labels)
	}
	if payload.Checksum == nil || payload.Checksum.Algorithm != "sha512" || payload.Checksum.Digest != "deadbeef" {
		t.Errorf("Checksum = %+v, want sha512/deadbeef", payload.Checksum)
	}
	if payload.Config == nil {
		t.Fatal("Config = nil, want non-nil")
	}
	if payload.Config.Architecture == nil || *payload.Config.Architecture != "x86" {
		t.Errorf("Config.Architecture = %v, want %q", payload.Config.Architecture, "x86")
	}
	if !payload.Config.DiskBus.IsSet() || payload.Config.DiskBus.Get() == nil || *payload.Config.DiskBus.Get() != "virtio" {
		t.Errorf("Config.DiskBus = %+v, want virtio", payload.Config.DiskBus)
	}
	if payload.Config.SecureBoot == nil || !*payload.Config.SecureBoot {
		t.Errorf("Config.SecureBoot = %v, want true", payload.Config.SecureBoot)
	}
}

func TestBuildImageUpdatePayload(t *testing.T) {
	spec := computev1alpha1.ImageSpec{
		Name:       "renamed",
		DiskFormat: "raw",
	}

	payload := BuildImageUpdatePayload(spec)

	if payload.Name == nil || *payload.Name != "renamed" {
		t.Errorf("Name = %v, want %q", payload.Name, "renamed")
	}
	if payload.DiskFormat == nil || *payload.DiskFormat != "raw" {
		t.Errorf("DiskFormat = %v, want %q", payload.DiskFormat, "raw")
	}

	empty := BuildImageUpdatePayload(computev1alpha1.ImageSpec{})
	if empty.Name != nil || empty.DiskFormat != nil || empty.Protected != nil {
		t.Errorf("empty BuildImageUpdatePayload() = %+v, want all nil", empty)
	}
}

// TestImageAPIWrappers exercises the thin CreateImage/GetImage/.../
// DeleteImage wrappers against the SDK's own DefaultAPIServiceMock,
// confirming each wires up to the right SDK method and passes
// results/errors through untouched. CreateImage returns
// *iaas.ImageCreateResponse, unlike the other Create* wrappers.
func TestImageAPIWrappers(t *testing.T) {
	wantErr := errors.New("boom")
	wantCreateResp := &iaas.ImageCreateResponse{Id: testImageID, UploadUrl: "https://upload.example.com"}
	imageID := testImageID
	wantImage := &iaas.Image{Id: &imageID}

	mock := &iaas.DefaultAPIServiceMock{}
	createCalled, getCalled, updateCalled, deleteCalled := false, false, false, false

	createFn := func(r iaas.ApiCreateImageRequest) (*iaas.ImageCreateResponse, error) {
		createCalled = true
		return wantCreateResp, nil
	}
	mock.CreateImageExecuteMock = &createFn

	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		getCalled = true
		return nil, wantErr
	}
	mock.GetImageExecuteMock = &getFn

	updateFn := func(r iaas.ApiUpdateImageRequest) (*iaas.Image, error) {
		updateCalled = true
		return wantImage, nil
	}
	mock.UpdateImageExecuteMock = &updateFn

	deleteFn := func(r iaas.ApiDeleteImageRequest) error {
		deleteCalled = true
		return wantErr
	}
	mock.DeleteImageExecuteMock = &deleteFn

	client := &iaas.APIClient{DefaultAPI: mock}
	ctx := context.Background()

	if got, err := CreateImage(ctx, client, testProjectID, testRegion, iaas.CreateImagePayload{}); !createCalled || err != nil || got != wantCreateResp {
		t.Errorf("CreateImage() = %v, %v; called=%v, want %v, nil", got, err, createCalled, wantCreateResp)
	}
	if _, err := GetImage(ctx, client, testProjectID, testRegion, testImageID); !getCalled || err != wantErr {
		t.Errorf("GetImage() error = %v; called=%v, want %v", err, getCalled, wantErr)
	}
	if got, err := UpdateImage(ctx, client, testProjectID, testRegion, testImageID, iaas.UpdateImagePayload{}); !updateCalled || err != nil || got != wantImage {
		t.Errorf("UpdateImage() = %v, %v; called=%v, want %v, nil", got, err, updateCalled, wantImage)
	}
	if err := DeleteImage(ctx, client, testProjectID, testRegion, testImageID); !deleteCalled || err != wantErr {
		t.Errorf("DeleteImage() error = %v; called=%v, want %v", err, deleteCalled, wantErr)
	}
}
