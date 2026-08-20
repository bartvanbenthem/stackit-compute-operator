package stackit

import (
	"context"
	"errors"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

const testVolumeID = "55555555-5555-5555-5555-555555555555"

func TestBuildVolumeCreatePayload_Required(t *testing.T) {
	spec := computev1alpha1.VolumeSpec{
		AvailabilityZone: "eu01-1",
	}

	payload := BuildVolumeCreatePayload("my-volume", spec)

	if payload.AvailabilityZone != spec.AvailabilityZone {
		t.Errorf("AvailabilityZone = %q, want %q", payload.AvailabilityZone, spec.AvailabilityZone)
	}
	if payload.Name == nil || *payload.Name != "my-volume" {
		t.Errorf("Name = %v, want %q", payload.Name, "my-volume")
	}
	if payload.Size != nil {
		t.Errorf("Size = %v, want nil", payload.Size)
	}
	if payload.Source != nil {
		t.Errorf("Source = %v, want nil", payload.Source)
	}
	if payload.Labels != nil {
		t.Errorf("Labels = %v, want nil", payload.Labels)
	}
}

func TestBuildVolumeCreatePayload_Optional(t *testing.T) {
	bootable := true
	spec := computev1alpha1.VolumeSpec{
		AvailabilityZone: "eu01-1",
		Size:             32,
		PerformanceClass: "storage_premium_perf1",
		Bootable:         &bootable,
		Description:      "boot volume",
		Source: &computev1alpha1.VolumeSourceSpec{
			Id:   "22222222-2222-2222-2222-222222222222",
			Type: "image",
		},
		Labels: map[string]string{"team": "infra"},
	}

	payload := BuildVolumeCreatePayload("my-volume", spec)

	if payload.Size == nil || *payload.Size != spec.Size {
		t.Errorf("Size = %v, want %d", payload.Size, spec.Size)
	}
	if payload.PerformanceClass == nil || *payload.PerformanceClass != spec.PerformanceClass {
		t.Errorf("PerformanceClass = %v, want %q", payload.PerformanceClass, spec.PerformanceClass)
	}
	if payload.Bootable == nil || *payload.Bootable != bootable {
		t.Errorf("Bootable = %v, want %v", payload.Bootable, bootable)
	}
	if payload.Description == nil || *payload.Description != spec.Description {
		t.Errorf("Description = %v, want %q", payload.Description, spec.Description)
	}
	if payload.Source == nil || payload.Source.Id != spec.Source.Id || payload.Source.Type != spec.Source.Type {
		t.Errorf("Source = %+v, want %+v", payload.Source, spec.Source)
	}
	if payload.Labels["team"] != "infra" {
		t.Errorf("Labels = %v, want team=infra", payload.Labels)
	}
}

func TestBuildVolumeUpdatePayload(t *testing.T) {
	bootable := true
	payload := BuildVolumeUpdatePayload("new-name", "new-desc", &bootable, map[string]string{"a": "b"})

	if payload.Name == nil || *payload.Name != "new-name" {
		t.Errorf("Name = %v, want %q", payload.Name, "new-name")
	}
	if payload.Description == nil || *payload.Description != "new-desc" {
		t.Errorf("Description = %v, want %q", payload.Description, "new-desc")
	}
	if payload.Bootable == nil || *payload.Bootable != bootable {
		t.Errorf("Bootable = %v, want %v", payload.Bootable, bootable)
	}
	if payload.Labels["a"] != "b" {
		t.Errorf("Labels = %v, want a=b", payload.Labels)
	}

	empty := BuildVolumeUpdatePayload("", "", nil, nil)
	if empty.Name != nil || empty.Description != nil || empty.Bootable != nil || empty.Labels != nil {
		t.Errorf("empty BuildVolumeUpdatePayload() = %+v, want all nil", empty)
	}
}

// TestVolumeAPIWrappers exercises the thin CreateVolume/GetVolume/.../
// DeleteVolume wrappers against the SDK's own DefaultAPIServiceMock,
// confirming each wires up to the right SDK method and passes
// results/errors through untouched.
func TestVolumeAPIWrappers(t *testing.T) {
	wantErr := errors.New("boom")
	wantVolume := &iaas.Volume{Id: utils.Ptr(testVolumeID)}

	mock := &iaas.DefaultAPIServiceMock{}
	createCalled, getCalled, updateCalled, resizeCalled, deleteCalled := false, false, false, false, false

	createFn := func(r iaas.ApiCreateVolumeRequest) (*iaas.Volume, error) {
		createCalled = true
		return wantVolume, nil
	}
	mock.CreateVolumeExecuteMock = &createFn

	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		getCalled = true
		return nil, wantErr
	}
	mock.GetVolumeExecuteMock = &getFn

	updateFn := func(r iaas.ApiUpdateVolumeRequest) (*iaas.Volume, error) {
		updateCalled = true
		return wantVolume, nil
	}
	mock.UpdateVolumeExecuteMock = &updateFn

	resizeFn := func(r iaas.ApiResizeVolumeRequest) error {
		resizeCalled = true
		return wantErr
	}
	mock.ResizeVolumeExecuteMock = &resizeFn

	deleteFn := func(r iaas.ApiDeleteVolumeRequest) error {
		deleteCalled = true
		return wantErr
	}
	mock.DeleteVolumeExecuteMock = &deleteFn

	client := &iaas.APIClient{DefaultAPI: mock}
	ctx := context.Background()

	if got, err := CreateVolume(ctx, client, testProjectID, testRegion, iaas.CreateVolumePayload{}); !createCalled || err != nil || got != wantVolume {
		t.Errorf("CreateVolume() = %v, %v; called=%v, want %v, nil", got, err, createCalled, wantVolume)
	}
	if _, err := GetVolume(ctx, client, testProjectID, testRegion, testVolumeID); !getCalled || err != wantErr {
		t.Errorf("GetVolume() error = %v; called=%v, want %v", err, getCalled, wantErr)
	}
	if got, err := UpdateVolume(ctx, client, testProjectID, testRegion, testVolumeID, iaas.UpdateVolumePayload{}); !updateCalled || err != nil || got != wantVolume {
		t.Errorf("UpdateVolume() = %v, %v; called=%v, want %v, nil", got, err, updateCalled, wantVolume)
	}
	if err := ResizeVolume(ctx, client, testProjectID, testRegion, testVolumeID, 64); !resizeCalled || err != wantErr {
		t.Errorf("ResizeVolume() error = %v; called=%v, want %v", err, resizeCalled, wantErr)
	}
	if err := DeleteVolume(ctx, client, testProjectID, testRegion, testVolumeID); !deleteCalled || err != wantErr {
		t.Errorf("DeleteVolume() error = %v; called=%v, want %v", err, deleteCalled, wantErr)
	}
}
