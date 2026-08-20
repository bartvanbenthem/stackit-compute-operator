package stackit

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

const (
	testProjectID = "proj-1"
	testRegion    = "eu01"
	testServerID  = "srv-1"
)

// TestAPIWrappers exercises the thin CreateServer/GetServer/.../DeleteServer
// wrappers against the SDK's own DefaultAPIServiceMock, confirming each
// wires up to the right SDK method and passes results/errors through
// untouched. The request builder types keep their fields unexported, so
// this can't assert on what was sent - that's covered by the
// BuildCreatePayload/BuildUpdatePayload tests above instead.
func TestAPIWrappers(t *testing.T) {
	wantErr := errors.New("boom")
	wantServer := &iaas.Server{Id: utils.Ptr(testServerID)}

	mock := &iaas.DefaultAPIServiceMock{}
	createCalled, getCalled, updateCalled, resizeCalled, startCalled, stopCalled, deleteCalled := false, false, false, false, false, false, false

	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		createCalled = true
		return wantServer, nil
	}
	mock.CreateServerExecuteMock = &createFn

	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		getCalled = true
		return nil, wantErr
	}
	mock.GetServerExecuteMock = &getFn

	updateFn := func(r iaas.ApiUpdateServerRequest) (*iaas.Server, error) {
		updateCalled = true
		return wantServer, nil
	}
	mock.UpdateServerExecuteMock = &updateFn

	resizeFn := func(r iaas.ApiResizeServerRequest) error {
		resizeCalled = true
		return wantErr
	}
	mock.ResizeServerExecuteMock = &resizeFn

	startFn := func(r iaas.ApiStartServerRequest) error {
		startCalled = true
		return nil
	}
	mock.StartServerExecuteMock = &startFn

	stopFn := func(r iaas.ApiStopServerRequest) error {
		stopCalled = true
		return nil
	}
	mock.StopServerExecuteMock = &stopFn

	deleteFn := func(r iaas.ApiDeleteServerRequest) error {
		deleteCalled = true
		return wantErr
	}
	mock.DeleteServerExecuteMock = &deleteFn

	client := &iaas.APIClient{DefaultAPI: mock}
	ctx := context.Background()

	if got, err := CreateServer(ctx, client, testProjectID, testRegion, iaas.CreateServerPayload{}); !createCalled || err != nil || got != wantServer {
		t.Errorf("CreateServer() = %v, %v; called=%v, want %v, nil", got, err, createCalled, wantServer)
	}
	if _, err := GetServer(ctx, client, testProjectID, testRegion, testServerID); !getCalled || err != wantErr {
		t.Errorf("GetServer() error = %v; called=%v, want %v", err, getCalled, wantErr)
	}
	if got, err := UpdateServer(ctx, client, testProjectID, testRegion, testServerID, iaas.UpdateServerPayload{}); !updateCalled || err != nil || got != wantServer {
		t.Errorf("UpdateServer() = %v, %v; called=%v, want %v, nil", got, err, updateCalled, wantServer)
	}
	if err := ResizeServer(ctx, client, testProjectID, testRegion, testServerID, "c1.4"); !resizeCalled || err != wantErr {
		t.Errorf("ResizeServer() error = %v; called=%v, want %v", err, resizeCalled, wantErr)
	}
	if err := StartServer(ctx, client, testProjectID, testRegion, testServerID); !startCalled || err != nil {
		t.Errorf("StartServer() error = %v; called=%v, want nil", err, startCalled)
	}
	if err := StopServer(ctx, client, testProjectID, testRegion, testServerID); !stopCalled || err != nil {
		t.Errorf("StopServer() error = %v; called=%v, want nil", err, stopCalled)
	}
	if err := DeleteServer(ctx, client, testProjectID, testRegion, testServerID); !deleteCalled || err != wantErr {
		t.Errorf("DeleteServer() error = %v; called=%v, want %v", err, deleteCalled, wantErr)
	}
}

func TestBuildCreatePayload_Required(t *testing.T) {
	spec := computev1alpha1.ServerSpec{
		ProjectId:   "11111111-1111-1111-1111-111111111111",
		Region:      "eu01",
		MachineType: "c1.2",
		ImageId:     "22222222-2222-2222-2222-222222222222",
		NetworkId:   "33333333-3333-3333-3333-333333333333",
	}

	payload := BuildCreatePayload("my-server", spec, spec.ImageId, spec.NetworkId, "")

	if payload.Name != "my-server" {
		t.Errorf("Name = %q, want %q", payload.Name, "my-server")
	}
	if payload.MachineType != spec.MachineType {
		t.Errorf("MachineType = %q, want %q", payload.MachineType, spec.MachineType)
	}
	if payload.ImageId == nil || *payload.ImageId != spec.ImageId {
		t.Errorf("ImageId = %v, want %q", payload.ImageId, spec.ImageId)
	}
	if payload.Networking.CreateServerNetworking == nil ||
		payload.Networking.CreateServerNetworking.NetworkId == nil ||
		*payload.Networking.CreateServerNetworking.NetworkId != spec.NetworkId {
		t.Errorf("Networking.NetworkId = %+v, want %q", payload.Networking, spec.NetworkId)
	}

	// Optional fields must stay unset when the spec doesn't set them.
	if payload.AvailabilityZone != nil {
		t.Errorf("AvailabilityZone = %v, want nil", payload.AvailabilityZone)
	}
	if payload.KeypairName != nil {
		t.Errorf("KeypairName = %v, want nil", payload.KeypairName)
	}
	if payload.UserData != nil {
		t.Errorf("UserData = %v, want nil", payload.UserData)
	}
	if payload.SecurityGroups != nil {
		t.Errorf("SecurityGroups = %v, want nil", payload.SecurityGroups)
	}
	if payload.ServiceAccountMails != nil {
		t.Errorf("ServiceAccountMails = %v, want nil", payload.ServiceAccountMails)
	}
	if payload.Labels != nil {
		t.Errorf("Labels = %v, want nil", payload.Labels)
	}
	if payload.BootVolume != nil {
		t.Errorf("BootVolume = %v, want nil", payload.BootVolume)
	}
}

func TestBuildCreatePayload_Optional(t *testing.T) {
	deleteOnTerm := false
	spec := computev1alpha1.ServerSpec{
		MachineType:      "c1.2",
		ImageId:          "22222222-2222-2222-2222-222222222222",
		NetworkId:        "33333333-3333-3333-3333-333333333333",
		AvailabilityZone: "eu01-1",
		KeypairName:      "my-key",
		UserData:         "#cloud-config\nfoo: bar",
		SecurityGroups:   []string{"sg-1", "sg-2"},
		ServiceAccountMails: []string{
			"sa1@example.com", "sa2@example.com",
		},
		Labels: map[string]string{"team": "infra"},
		BootVolume: computev1alpha1.BootVolumeSpec{
			Size:                42,
			PerformanceClass:    "storage_premium_perf1",
			DeleteOnTermination: &deleteOnTerm,
		},
	}

	payload := BuildCreatePayload("my-server", spec, spec.ImageId, spec.NetworkId, "")

	if payload.AvailabilityZone == nil || *payload.AvailabilityZone != spec.AvailabilityZone {
		t.Errorf("AvailabilityZone = %v, want %q", payload.AvailabilityZone, spec.AvailabilityZone)
	}
	if payload.KeypairName == nil || *payload.KeypairName != spec.KeypairName {
		t.Errorf("KeypairName = %v, want %q", payload.KeypairName, spec.KeypairName)
	}

	wantUserData := base64.StdEncoding.EncodeToString([]byte(spec.UserData))
	if payload.UserData == nil || *payload.UserData != wantUserData {
		t.Errorf("UserData = %v, want base64 %q", payload.UserData, wantUserData)
	}

	if len(payload.SecurityGroups) != 2 || payload.SecurityGroups[0] != "sg-1" || payload.SecurityGroups[1] != "sg-2" {
		t.Errorf("SecurityGroups = %v, want %v", payload.SecurityGroups, spec.SecurityGroups)
	}
	if len(payload.ServiceAccountMails) != 2 {
		t.Errorf("ServiceAccountMails = %v, want 2 entries", payload.ServiceAccountMails)
	}

	if payload.Labels["team"] != "infra" {
		t.Errorf("Labels = %v, want team=infra", payload.Labels)
	}

	if payload.BootVolume == nil {
		t.Fatal("BootVolume = nil, want non-nil")
	}
	if payload.BootVolume.Source == nil || payload.BootVolume.Source.Id != spec.ImageId || payload.BootVolume.Source.Type != "image" {
		t.Errorf("BootVolume.Source = %+v, want image %q", payload.BootVolume.Source, spec.ImageId)
	}
	if payload.BootVolume.Size == nil || *payload.BootVolume.Size != spec.BootVolume.Size {
		t.Errorf("BootVolume.Size = %v, want %d", payload.BootVolume.Size, spec.BootVolume.Size)
	}
	if payload.BootVolume.PerformanceClass == nil || *payload.BootVolume.PerformanceClass != spec.BootVolume.PerformanceClass {
		t.Errorf("BootVolume.PerformanceClass = %v, want %q", payload.BootVolume.PerformanceClass, spec.BootVolume.PerformanceClass)
	}
	if payload.BootVolume.DeleteOnTermination == nil || *payload.BootVolume.DeleteOnTermination != false {
		t.Errorf("BootVolume.DeleteOnTermination = %v, want false", payload.BootVolume.DeleteOnTermination)
	}
}

func TestBuildCreatePayload_ResolvedBootVolumeID(t *testing.T) {
	// When a resolvedBootVolumeID is supplied (i.e. spec.bootVolumeRef was
	// resolved to an existing Volume), the boot volume must reference that
	// volume directly (Source.Type "volume") instead of creating a new one
	// from the image, and must ignore spec.BootVolume.Size/PerformanceClass
	// since the volume already exists.
	deleteOnTerm := true
	spec := computev1alpha1.ServerSpec{
		MachineType: "c1.2",
		ImageId:     "22222222-2222-2222-2222-222222222222",
		NetworkId:   "33333333-3333-3333-3333-333333333333",
		BootVolume: computev1alpha1.BootVolumeSpec{
			Size:                999, // must be ignored
			PerformanceClass:    "ignored-class",
			DeleteOnTermination: &deleteOnTerm,
		},
	}
	resolvedBootVolumeID := "55555555-5555-5555-5555-555555555555"

	payload := BuildCreatePayload("my-server", spec, spec.ImageId, spec.NetworkId, resolvedBootVolumeID)

	if payload.BootVolume == nil {
		t.Fatal("BootVolume = nil, want non-nil")
	}
	if payload.BootVolume.Source == nil || payload.BootVolume.Source.Id != resolvedBootVolumeID || payload.BootVolume.Source.Type != "volume" {
		t.Errorf("BootVolume.Source = %+v, want {Id: %q, Type: volume}", payload.BootVolume.Source, resolvedBootVolumeID)
	}
	if payload.BootVolume.Size != nil {
		t.Errorf("BootVolume.Size = %v, want nil (ignored when booting from an existing volume)", payload.BootVolume.Size)
	}
	if payload.BootVolume.PerformanceClass != nil {
		t.Errorf("BootVolume.PerformanceClass = %v, want nil (ignored when booting from an existing volume)", payload.BootVolume.PerformanceClass)
	}
	if payload.BootVolume.DeleteOnTermination == nil || *payload.BootVolume.DeleteOnTermination != deleteOnTerm {
		t.Errorf("BootVolume.DeleteOnTermination = %v, want %v", payload.BootVolume.DeleteOnTermination, deleteOnTerm)
	}
}

func TestBuildCreatePayload_BootVolumeOnlyDeleteOnTermination(t *testing.T) {
	// A DeleteOnTermination-only BootVolumeSpec (size and class both zero)
	// must still produce a BootVolume, since a nil *bool has meaning
	// distinct from an unset field.
	deleteOnTerm := true
	spec := computev1alpha1.ServerSpec{
		MachineType: "c1.2",
		ImageId:     "22222222-2222-2222-2222-222222222222",
		NetworkId:   "33333333-3333-3333-3333-333333333333",
		BootVolume: computev1alpha1.BootVolumeSpec{
			DeleteOnTermination: &deleteOnTerm,
		},
	}

	payload := BuildCreatePayload("my-server", spec, spec.ImageId, spec.NetworkId, "")

	if payload.BootVolume == nil {
		t.Fatal("BootVolume = nil, want non-nil")
	}
	if payload.BootVolume.Size != nil {
		t.Errorf("BootVolume.Size = %v, want nil", payload.BootVolume.Size)
	}
	if payload.BootVolume.PerformanceClass != nil {
		t.Errorf("BootVolume.PerformanceClass = %v, want nil", payload.BootVolume.PerformanceClass)
	}
}

func TestBuildUpdatePayload(t *testing.T) {
	tests := []struct {
		name       string
		newName    string
		labels     map[string]string
		wantName   *string
		wantLabels bool
	}{
		{"empty", "", nil, nil, false},
		{"name only", "new-name", nil, utils.Ptr("new-name"), false},
		{"labels only", "", map[string]string{"a": "b"}, nil, true},
		{"both", "new-name", map[string]string{"a": "b"}, utils.Ptr("new-name"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := BuildUpdatePayload(tt.newName, tt.labels)

			if tt.wantName == nil {
				if payload.Name != nil {
					t.Errorf("Name = %v, want nil", payload.Name)
				}
			} else if payload.Name == nil || *payload.Name != *tt.wantName {
				t.Errorf("Name = %v, want %v", payload.Name, tt.wantName)
			}

			if tt.wantLabels {
				if payload.Labels == nil || payload.Labels["a"] != "b" {
					t.Errorf("Labels = %v, want a=b", payload.Labels)
				}
			} else if payload.Labels != nil {
				t.Errorf("Labels = %v, want nil", payload.Labels)
			}
		})
	}
}
