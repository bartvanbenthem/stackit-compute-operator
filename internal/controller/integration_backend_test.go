//go:build integration

package controller

import (
	"sync"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"
)

const fakeStackitServerID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

// fakeStackitBackend is a minimal, single-server, stateful stand-in for the
// STACKIT IaaS API. The real SDK's request builder types keep their fields
// unexported (see api_default.go's ApiXxxRequest structs), so callbacks
// wired into iaas.DefaultAPIServiceMock cannot introspect what payload was
// sent - only that a given operation was invoked. That's enough to drive a
// believable async server lifecycle (CREATING -> ACTIVE, stop/start,
// DELETING -> gone) for exercising the reconciler end-to-end; payload
// construction itself is covered by the stackit package's unit tests.
type fakeStackitBackend struct {
	mu sync.Mutex

	exists      bool
	status      string
	powerStatus string
	machineType string

	stopRequested   bool
	startRequested  bool
	deleteRequested bool
	getsSinceDelete int
}

func newFakeStackitBackend() *fakeStackitBackend {
	return &fakeStackitBackend{}
}

func (b *fakeStackitBackend) mock() *iaas.DefaultAPIServiceMock {
	mock := &iaas.DefaultAPIServiceMock{}

	createFn := func(r iaas.ApiCreateServerRequest) (*iaas.Server, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.exists = true
		b.status = "CREATING"
		b.powerStatus = "STARTING"
		b.machineType = "c1.2"
		b.stopRequested = false
		b.startRequested = false
		b.deleteRequested = false
		b.getsSinceDelete = 0
		return b.snapshotLocked(), nil
	}
	mock.CreateServerExecuteMock = &createFn

	getFn := func(r iaas.ApiGetServerRequest) (*iaas.Server, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.exists {
			return nil, oapierror.NewError(404, "server not found")
		}
		if b.deleteRequested {
			b.getsSinceDelete++
			if b.getsSinceDelete >= 1 {
				b.exists = false
				return nil, oapierror.NewError(404, "server not found")
			}
		}
		if b.status == "CREATING" {
			b.status = "ACTIVE"
			b.powerStatus = "RUNNING"
		}
		if b.stopRequested {
			b.status = "INACTIVE"
			b.powerStatus = "STOPPED"
		}
		if b.startRequested {
			b.status = "ACTIVE"
			b.powerStatus = "RUNNING"
		}
		return b.snapshotLocked(), nil
	}
	mock.GetServerExecuteMock = &getFn

	stopFn := func(r iaas.ApiStopServerRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.stopRequested = true
		b.startRequested = false
		return nil
	}
	mock.StopServerExecuteMock = &stopFn

	startFn := func(r iaas.ApiStartServerRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.startRequested = true
		b.stopRequested = false
		return nil
	}
	mock.StartServerExecuteMock = &startFn

	deleteFn := func(r iaas.ApiDeleteServerRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.deleteRequested = true
		b.status = "DELETING"
		return nil
	}
	mock.DeleteServerExecuteMock = &deleteFn

	return mock
}

// snapshotLocked builds the Server response for the current state. Callers
// must hold b.mu.
func (b *fakeStackitBackend) snapshotLocked() *iaas.Server {
	return &iaas.Server{
		Id:          utils.Ptr(fakeStackitServerID),
		Name:        "it-server",
		Status:      utils.Ptr(b.status),
		PowerStatus: utils.Ptr(b.powerStatus),
		MachineType: b.machineType,
		Labels:      map[string]interface{}{},
	}
}

// fakeVolumeBackend is fakeStackitBackend's counterpart for Volume: a
// minimal, single-volume, stateful stand-in for STACKIT's volume API.
type fakeVolumeBackend struct {
	mu sync.Mutex

	exists bool
	id     string
	status string
	size   int64

	deleteRequested bool
	getsSinceDelete int
}

func newFakeVolumeBackend() *fakeVolumeBackend {
	return &fakeVolumeBackend{}
}

// seedExisting marks a volume as already present in STACKIT without going
// through CreateVolume, for adopt-mode ("existingId") tests.
func (b *fakeVolumeBackend) seedExisting(id string, size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.exists = true
	b.id = id
	b.status = "AVAILABLE"
	b.size = size
	b.deleteRequested = false
	b.getsSinceDelete = 0
}

func (b *fakeVolumeBackend) mock() *iaas.DefaultAPIServiceMock {
	mock := &iaas.DefaultAPIServiceMock{}

	createFn := func(r iaas.ApiCreateVolumeRequest) (*iaas.Volume, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.exists = true
		b.id = fakeStackitVolumeID
		b.status = "CREATING"
		b.size = 32
		b.deleteRequested = false
		b.getsSinceDelete = 0
		return b.snapshotLocked(), nil
	}
	mock.CreateVolumeExecuteMock = &createFn

	getFn := func(r iaas.ApiGetVolumeRequest) (*iaas.Volume, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.exists {
			return nil, oapierror.NewError(404, "volume not found")
		}
		if b.deleteRequested {
			b.getsSinceDelete++
			if b.getsSinceDelete >= 1 {
				b.exists = false
				return nil, oapierror.NewError(404, "volume not found")
			}
		}
		if b.status == "CREATING" {
			b.status = "AVAILABLE"
		}
		return b.snapshotLocked(), nil
	}
	mock.GetVolumeExecuteMock = &getFn

	resizeFn := func(r iaas.ApiResizeVolumeRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.size = 64
		return nil
	}
	mock.ResizeVolumeExecuteMock = &resizeFn

	deleteFn := func(r iaas.ApiDeleteVolumeRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.deleteRequested = true
		b.status = "DELETING"
		return nil
	}
	mock.DeleteVolumeExecuteMock = &deleteFn

	return mock
}

func (b *fakeVolumeBackend) snapshotLocked() *iaas.Volume {
	return &iaas.Volume{
		Id:               utils.Ptr(b.id),
		AvailabilityZone: "eu01-1",
		Name:             utils.Ptr("it-volume"),
		Status:           utils.Ptr(b.status),
		Size:             utils.Ptr(b.size),
		Labels:           map[string]interface{}{},
	}
}

func (b *fakeVolumeBackend) existsLocked() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exists
}

// fakeImageBackend is fakeStackitBackend's counterpart for Image. Real
// STACKIT leaves a created image in CREATING until its bytes are uploaded
// out-of-band (see README's "Images and the upload-bytes gap"); this fake
// auto-transitions to AVAILABLE on the first Get instead, since exercising
// that manual step isn't feasible in an automated test and the reconciler
// mechanics being tested here (finalizer/status/drift wiring) don't depend
// on it.
type fakeImageBackend struct {
	mu sync.Mutex

	exists bool
	id     string
	status string

	deleteRequested bool
	getsSinceDelete int
}

func newFakeImageBackend() *fakeImageBackend {
	return &fakeImageBackend{}
}

func (b *fakeImageBackend) seedExisting(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.exists = true
	b.id = id
	b.status = "AVAILABLE"
	b.deleteRequested = false
	b.getsSinceDelete = 0
}

func (b *fakeImageBackend) mock() *iaas.DefaultAPIServiceMock {
	mock := &iaas.DefaultAPIServiceMock{}

	createFn := func(r iaas.ApiCreateImageRequest) (*iaas.ImageCreateResponse, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.exists = true
		b.id = fakeStackitImageID
		b.status = "CREATING"
		b.deleteRequested = false
		b.getsSinceDelete = 0
		return &iaas.ImageCreateResponse{Id: b.id, UploadUrl: "https://upload.example.com"}, nil
	}
	mock.CreateImageExecuteMock = &createFn

	getFn := func(r iaas.ApiGetImageRequest) (*iaas.Image, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.exists {
			return nil, oapierror.NewError(404, "image not found")
		}
		if b.deleteRequested {
			b.getsSinceDelete++
			if b.getsSinceDelete >= 1 {
				b.exists = false
				return nil, oapierror.NewError(404, "image not found")
			}
		}
		if b.status == "CREATING" {
			b.status = "AVAILABLE"
		}
		return b.snapshotLocked(), nil
	}
	mock.GetImageExecuteMock = &getFn

	deleteFn := func(r iaas.ApiDeleteImageRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.deleteRequested = true
		b.status = "DELETING"
		return nil
	}
	mock.DeleteImageExecuteMock = &deleteFn

	return mock
}

func (b *fakeImageBackend) snapshotLocked() *iaas.Image {
	return &iaas.Image{
		Id:         utils.Ptr(b.id),
		Name:       "it-image",
		DiskFormat: "qcow2",
		Status:     utils.Ptr(b.status),
		Labels:     map[string]interface{}{},
	}
}

func (b *fakeImageBackend) existsLocked() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exists
}

// fakeNetworkBackend is fakeStackitBackend's counterpart for Network.
type fakeNetworkBackend struct {
	mu sync.Mutex

	exists bool
	id     string
	status string

	deleteRequested bool
	getsSinceDelete int
}

func newFakeNetworkBackend() *fakeNetworkBackend {
	return &fakeNetworkBackend{}
}

func (b *fakeNetworkBackend) seedExisting(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.exists = true
	b.id = id
	b.status = "CREATED"
	b.deleteRequested = false
	b.getsSinceDelete = 0
}

func (b *fakeNetworkBackend) mock() *iaas.DefaultAPIServiceMock {
	mock := &iaas.DefaultAPIServiceMock{}

	createFn := func(r iaas.ApiCreateNetworkRequest) (*iaas.Network, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.exists = true
		b.id = fakeStackitNetworkID
		b.status = "CREATING"
		b.deleteRequested = false
		b.getsSinceDelete = 0
		return b.snapshotLocked(), nil
	}
	mock.CreateNetworkExecuteMock = &createFn

	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.exists {
			return nil, oapierror.NewError(404, "network not found")
		}
		if b.deleteRequested {
			b.getsSinceDelete++
			if b.getsSinceDelete >= 1 {
				b.exists = false
				return nil, oapierror.NewError(404, "network not found")
			}
		}
		if b.status == "CREATING" {
			b.status = "CREATED"
		}
		return b.snapshotLocked(), nil
	}
	mock.GetNetworkExecuteMock = &getFn

	deleteFn := func(r iaas.ApiDeleteNetworkRequest) error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.deleteRequested = true
		b.status = "DELETING"
		return nil
	}
	mock.DeleteNetworkExecuteMock = &deleteFn

	return mock
}

func (b *fakeNetworkBackend) snapshotLocked() *iaas.Network {
	return &iaas.Network{
		Id:     b.id,
		Name:   "it-network",
		Status: b.status,
		Labels: map[string]interface{}{},
	}
}

func (b *fakeNetworkBackend) existsLocked() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exists
}

const (
	fakeStackitVolumeID  = "aaaaaaaa-bbbb-cccc-dddd-111111111111"
	fakeStackitImageID   = "aaaaaaaa-bbbb-cccc-dddd-222222222222"
	fakeStackitNetworkID = "aaaaaaaa-bbbb-cccc-dddd-333333333333"
)

// fakeClusterBackend is fakeStackitBackend's counterpart for Cluster: a
// minimal, single-cluster, stateful stand-in for STACKIT's SKE API. Unlike
// the IaaS resources, SKE clusters are keyed by name rather than a
// server-assigned UUID, and creation/update share one idempotent
// CreateOrUpdateCluster endpoint.
type fakeClusterBackend struct {
	mu sync.Mutex

	exists bool
	name   string
	state  ske.ClusterStatusState

	deleteRequested bool
	getsSinceDelete int
}

func newFakeClusterBackend() *fakeClusterBackend {
	return &fakeClusterBackend{}
}

// seedExisting marks a cluster as already present in STACKIT without going
// through CreateOrUpdateCluster, for adopt-mode ("existingClusterName")
// tests.
func (b *fakeClusterBackend) seedExisting(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.exists = true
	b.name = name
	b.state = ske.CLUSTERSTATUSSTATE_STATE_HEALTHY
	b.deleteRequested = false
	b.getsSinceDelete = 0
}

func (b *fakeClusterBackend) mock() *ske.DefaultAPIServiceMock {
	mock := &ske.DefaultAPIServiceMock{}

	createFn := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.exists = true
		b.name = fakeStackitClusterName
		b.state = ske.CLUSTERSTATUSSTATE_STATE_CREATING
		b.deleteRequested = false
		b.getsSinceDelete = 0
		return b.snapshotLocked(), nil
	}
	mock.CreateOrUpdateClusterExecuteMock = &createFn

	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.exists {
			return nil, oapierror.NewError(404, "cluster not found")
		}
		if b.deleteRequested {
			b.getsSinceDelete++
			if b.getsSinceDelete >= 1 {
				b.exists = false
				return nil, oapierror.NewError(404, "cluster not found")
			}
		}
		if b.state == ske.CLUSTERSTATUSSTATE_STATE_CREATING {
			b.state = ske.CLUSTERSTATUSSTATE_STATE_HEALTHY
		}
		return b.snapshotLocked(), nil
	}
	mock.GetClusterExecuteMock = &getFn

	deleteFn := func(r ske.ApiDeleteClusterRequest) (map[string]interface{}, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.deleteRequested = true
		b.state = ske.CLUSTERSTATUSSTATE_STATE_DELETING
		return nil, nil
	}
	mock.DeleteClusterExecuteMock = &deleteFn

	return mock
}

func (b *fakeClusterBackend) snapshotLocked() *ske.Cluster {
	state := b.state
	return &ske.Cluster{
		Name:       utils.Ptr(b.name),
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

func (b *fakeClusterBackend) existsLocked() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exists
}

const fakeStackitClusterName = "it-cluster"
