package stackit

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

const testClusterName = "my-cluster"

func TestBuildClusterPayload_Required(t *testing.T) {
	spec := computev1alpha1.ClusterSpec{
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
	}

	payload := BuildClusterPayload(spec)

	if payload.Kubernetes.Version != "1.29.3" {
		t.Errorf("Kubernetes.Version = %q, want %q", payload.Kubernetes.Version, "1.29.3")
	}
	if payload.Maintenance != nil {
		t.Errorf("Maintenance = %+v, want nil", payload.Maintenance)
	}
	if len(payload.Nodepools) != 1 {
		t.Fatalf("len(Nodepools) = %d, want 1", len(payload.Nodepools))
	}

	pool := payload.Nodepools[0]
	if pool.Name != "pool-1" {
		t.Errorf("Nodepools[0].Name = %q, want %q", pool.Name, "pool-1")
	}
	if pool.Machine.Type != "c1.2" {
		t.Errorf("Nodepools[0].Machine.Type = %q, want %q", pool.Machine.Type, "c1.2")
	}
	if pool.Machine.Image.Name != "flatcar" || pool.Machine.Image.Version != "3815.2.0" {
		t.Errorf("Nodepools[0].Machine.Image = %+v, want {flatcar 3815.2.0}", pool.Machine.Image)
	}
	if pool.Minimum != 1 || pool.Maximum != 3 {
		t.Errorf("Nodepools[0].{Minimum,Maximum} = {%d,%d}, want {1,3}", pool.Minimum, pool.Maximum)
	}
	if pool.Volume.Size != 32 {
		t.Errorf("Nodepools[0].Volume.Size = %d, want 32", pool.Volume.Size)
	}
	if pool.Volume.Type != nil {
		t.Errorf("Nodepools[0].Volume.Type = %v, want nil", pool.Volume.Type)
	}
	if len(pool.AvailabilityZones) != 1 || pool.AvailabilityZones[0] != "eu01-1" {
		t.Errorf("Nodepools[0].AvailabilityZones = %v, want [eu01-1]", pool.AvailabilityZones)
	}
	if pool.AllowSystemComponents != nil {
		t.Errorf("Nodepools[0].AllowSystemComponents = %v, want nil", pool.AllowSystemComponents)
	}
	if pool.Labels != nil {
		t.Errorf("Nodepools[0].Labels = %v, want nil", pool.Labels)
	}
}

func TestBuildClusterPayload_Optional(t *testing.T) {
	allowSystem := true
	start := metav1.NewTime(time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC))
	end := metav1.NewTime(time.Date(2024, 1, 1, 4, 0, 0, 0, time.UTC))
	autoUpdate := true

	spec := computev1alpha1.ClusterSpec{
		KubernetesVersion: "1.29.3",
		NodePools: []computev1alpha1.NodePoolSpec{
			{
				Name:                  "pool-1",
				MachineType:           "c1.2",
				MachineImageName:      "flatcar",
				MachineImageVersion:   "3815.2.0",
				AvailabilityZones:     []string{"eu01-1", "eu01-2"},
				Minimum:               1,
				Maximum:               3,
				Volume:                computev1alpha1.NodePoolVolumeSpec{Size: 32, Type: "storage_premium_perf1"},
				AllowSystemComponents: &allowSystem,
				Labels:                map[string]string{"team": "infra"},
			},
		},
		Maintenance: &computev1alpha1.ClusterMaintenanceSpec{
			AutoUpdateKubernetesVersion:   &autoUpdate,
			AutoUpdateMachineImageVersion: &autoUpdate,
			Start:                         start,
			End:                           end,
		},
	}

	payload := BuildClusterPayload(spec)

	pool := payload.Nodepools[0]
	if pool.Volume.Type == nil || *pool.Volume.Type != "storage_premium_perf1" {
		t.Errorf("Nodepools[0].Volume.Type = %v, want %q", pool.Volume.Type, "storage_premium_perf1")
	}
	if pool.AllowSystemComponents == nil || !*pool.AllowSystemComponents {
		t.Errorf("Nodepools[0].AllowSystemComponents = %v, want true", pool.AllowSystemComponents)
	}
	if pool.Labels == nil || (*pool.Labels)["team"] != "infra" {
		t.Errorf("Nodepools[0].Labels = %v, want team=infra", pool.Labels)
	}
	if len(pool.AvailabilityZones) != 2 {
		t.Errorf("Nodepools[0].AvailabilityZones = %v, want 2 entries", pool.AvailabilityZones)
	}

	if payload.Maintenance == nil {
		t.Fatal("Maintenance = nil, want set")
	}
	if payload.Maintenance.AutoUpdate.KubernetesVersion == nil || !*payload.Maintenance.AutoUpdate.KubernetesVersion {
		t.Errorf("Maintenance.AutoUpdate.KubernetesVersion = %v, want true", payload.Maintenance.AutoUpdate.KubernetesVersion)
	}
	if !payload.Maintenance.TimeWindow.Start.Equal(start.Time) {
		t.Errorf("Maintenance.TimeWindow.Start = %v, want %v", payload.Maintenance.TimeWindow.Start, start.Time)
	}
	if !payload.Maintenance.TimeWindow.End.Equal(end.Time) {
		t.Errorf("Maintenance.TimeWindow.End = %v, want %v", payload.Maintenance.TimeWindow.End, end.Time)
	}
}

// TestClusterAPIWrappers exercises the thin CreateOrUpdateCluster/GetCluster/
// DeleteCluster wrappers against the SDK's own DefaultAPIServiceMock,
// confirming each wires up to the right SDK method and passes
// results/errors through untouched.
func TestClusterAPIWrappers(t *testing.T) {
	wantErr := errors.New("boom")
	wantCluster := &ske.Cluster{Name: utils.Ptr(testClusterName)}

	mock := &ske.DefaultAPIServiceMock{}
	createCalled, getCalled, deleteCalled := false, false, false

	createFn := func(r ske.ApiCreateOrUpdateClusterRequest) (*ske.Cluster, error) {
		createCalled = true
		return wantCluster, nil
	}
	mock.CreateOrUpdateClusterExecuteMock = &createFn

	getFn := func(r ske.ApiGetClusterRequest) (*ske.Cluster, error) {
		getCalled = true
		return nil, wantErr
	}
	mock.GetClusterExecuteMock = &getFn

	deleteFn := func(r ske.ApiDeleteClusterRequest) (map[string]interface{}, error) {
		deleteCalled = true
		return nil, wantErr
	}
	mock.DeleteClusterExecuteMock = &deleteFn

	client := &ske.APIClient{DefaultAPI: mock}
	ctx := context.Background()

	if got, err := CreateOrUpdateCluster(ctx, client, testProjectID, testRegion, testClusterName, ske.CreateOrUpdateClusterPayload{}); !createCalled || err != nil || got != wantCluster {
		t.Errorf("CreateOrUpdateCluster() = %v, %v; called=%v, want %v, nil", got, err, createCalled, wantCluster)
	}
	if _, err := GetCluster(ctx, client, testProjectID, testRegion, testClusterName); !getCalled || err != wantErr {
		t.Errorf("GetCluster() error = %v; called=%v, want %v", err, getCalled, wantErr)
	}
	if err := DeleteCluster(ctx, client, testProjectID, testRegion, testClusterName); !deleteCalled || err != wantErr {
		t.Errorf("DeleteCluster() error = %v; called=%v, want %v", err, deleteCalled, wantErr)
	}
}
