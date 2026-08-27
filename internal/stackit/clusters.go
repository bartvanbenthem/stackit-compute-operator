package stackit

import (
	"context"

	ske "github.com/stackitcloud/stackit-sdk-go/services/ske/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

// BuildClusterPayload translates a Cluster's spec into a STACKIT
// CreateOrUpdateClusterPayload. It is used for both creation and drift
// correction: SKE's CreateOrUpdateCluster endpoint is an idempotent
// upsert keyed by the cluster name passed separately as a path parameter.
func BuildClusterPayload(spec computev1alpha1.ClusterSpec) ske.CreateOrUpdateClusterPayload {
	payload := ske.CreateOrUpdateClusterPayload{
		Kubernetes: ske.Kubernetes{Version: spec.KubernetesVersion},
		Nodepools:  buildNodepools(spec.NodePools),
	}

	if spec.Maintenance != nil {
		payload.Maintenance = &ske.Maintenance{
			AutoUpdate: ske.MaintenanceAutoUpdate{
				KubernetesVersion:   spec.Maintenance.AutoUpdateKubernetesVersion,
				MachineImageVersion: spec.Maintenance.AutoUpdateMachineImageVersion,
			},
			TimeWindow: ske.TimeWindow{
				Start: spec.Maintenance.Start.Time,
				End:   spec.Maintenance.End.Time,
			},
		}
	}

	return payload
}

func buildNodepools(specs []computev1alpha1.NodePoolSpec) []ske.Nodepool {
	nodepools := make([]ske.Nodepool, 0, len(specs))
	for _, np := range specs {
		pool := ske.Nodepool{
			AvailabilityZones: append([]string(nil), np.AvailabilityZones...),
			Machine: ske.Machine{
				Image: ske.Image{
					Name:    np.MachineImageName,
					Version: np.MachineImageVersion,
				},
				Type: np.MachineType,
			},
			Maximum: int32(np.Maximum),
			Minimum: int32(np.Minimum),
			Name:    np.Name,
			Volume: ske.Volume{
				Size: int32(np.Volume.Size),
			},
		}
		if np.Volume.Type != "" {
			pool.Volume.Type = &np.Volume.Type
		}
		if np.AllowSystemComponents != nil {
			pool.AllowSystemComponents = np.AllowSystemComponents
		}
		if len(np.Labels) > 0 {
			labels := np.Labels
			pool.Labels = &labels
		}
		nodepools = append(nodepools, pool)
	}
	return nodepools
}

// CreateOrUpdateCluster creates a new cluster, or updates an existing one,
// with the given name. SKE has no separate create/update endpoint: the same
// idempotent upsert call is used for both initial creation and later drift
// correction.
func CreateOrUpdateCluster(ctx context.Context, client *ske.APIClient, projectID, region, clusterName string, payload ske.CreateOrUpdateClusterPayload) (*ske.Cluster, error) {
	return client.DefaultAPI.CreateOrUpdateCluster(ctx, projectID, region, clusterName).CreateOrUpdateClusterPayload(payload).Execute()
}

// GetCluster fetches the current state of a cluster from STACKIT.
func GetCluster(ctx context.Context, client *ske.APIClient, projectID, region, clusterName string) (*ske.Cluster, error) {
	return client.DefaultAPI.GetCluster(ctx, projectID, region, clusterName).Execute()
}

// DeleteCluster permanently deletes a cluster.
func DeleteCluster(ctx context.Context, client *ske.APIClient, projectID, region, clusterName string) error {
	_, err := client.DefaultAPI.DeleteCluster(ctx, projectID, region, clusterName).Execute()
	return err
}
