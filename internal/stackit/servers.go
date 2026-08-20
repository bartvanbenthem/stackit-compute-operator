package stackit

import (
	"context"
	"encoding/base64"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-compute-operator/api/v1alpha1"
)

// BuildCreatePayload translates a Server's spec into a STACKIT
// CreateServerPayload. resolvedImageID and resolvedNetworkID are the IDs
// already resolved by the controller from spec.imageId/spec.imageRef and
// spec.networkId/spec.networkRef respectively. resolvedBootVolumeID, when
// non-empty, is the ID of an already-existing Volume (resolved from
// spec.bootVolumeRef) to boot from instead of creating a new boot volume
// from resolvedImageID; in that case spec.bootVolume.size/performanceClass
// are ignored, since the volume already exists.
func BuildCreatePayload(name string, spec computev1alpha1.ServerSpec, resolvedImageID, resolvedNetworkID, resolvedBootVolumeID string) iaas.CreateServerPayload {
	payload := iaas.CreateServerPayload{
		Name:        name,
		MachineType: spec.MachineType,
		ImageId:     utils.Ptr(resolvedImageID),
		Networking: iaas.CreateServerPayloadAllOfNetworking{
			CreateServerNetworking: &iaas.CreateServerNetworking{
				NetworkId: utils.Ptr(resolvedNetworkID),
			},
		},
	}

	if spec.AvailabilityZone != "" {
		payload.AvailabilityZone = utils.Ptr(spec.AvailabilityZone)
	}
	if spec.KeypairName != "" {
		payload.KeypairName = utils.Ptr(spec.KeypairName)
	}
	if spec.UserData != "" {
		// STACKIT requires user data to be base64-encoded cloud-init.
		payload.UserData = utils.Ptr(base64.StdEncoding.EncodeToString([]byte(spec.UserData)))
	}
	if len(spec.SecurityGroups) > 0 {
		payload.SecurityGroups = append([]string(nil), spec.SecurityGroups...)
	}
	if len(spec.ServiceAccountMails) > 0 {
		payload.ServiceAccountMails = append([]string(nil), spec.ServiceAccountMails...)
	}
	if len(spec.Labels) > 0 {
		payload.Labels = toInterfaceMap(spec.Labels)
	}

	switch {
	case resolvedBootVolumeID != "":
		payload.BootVolume = &iaas.BootVolume{
			Source: &iaas.BootVolumeSource{
				Id:   resolvedBootVolumeID,
				Type: "volume",
			},
			DeleteOnTermination: spec.BootVolume.DeleteOnTermination,
		}
	case spec.BootVolume.Size > 0 || spec.BootVolume.PerformanceClass != "" || spec.BootVolume.DeleteOnTermination != nil:
		bootVolume := &iaas.BootVolume{
			Source: &iaas.BootVolumeSource{
				Id:   resolvedImageID,
				Type: "image",
			},
			DeleteOnTermination: spec.BootVolume.DeleteOnTermination,
		}
		if spec.BootVolume.Size > 0 {
			bootVolume.Size = utils.Ptr(spec.BootVolume.Size)
		}
		if spec.BootVolume.PerformanceClass != "" {
			bootVolume.PerformanceClass = utils.Ptr(spec.BootVolume.PerformanceClass)
		}
		payload.BootVolume = bootVolume
	}

	return payload
}

// BuildUpdatePayload translates the fields that differ between the desired
// spec and the observed STACKIT server into an UpdateServerPayload. An
// empty name or nil labels leave the corresponding field unset.
func BuildUpdatePayload(name string, labels map[string]string) iaas.UpdateServerPayload {
	payload := iaas.UpdateServerPayload{}
	if name != "" {
		payload.Name = utils.Ptr(name)
	}
	if labels != nil {
		payload.Labels = toInterfaceMap(labels)
	}
	return payload
}

func toInterfaceMap(in map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// CreateServer triggers creation of a new server and returns the initial
// (still-provisioning) server object.
func CreateServer(ctx context.Context, client *iaas.APIClient, projectID, region string, payload iaas.CreateServerPayload) (*iaas.Server, error) {
	return client.DefaultAPI.CreateServer(ctx, projectID, region).CreateServerPayload(payload).Execute()
}

// GetServer fetches the current state of a server from STACKIT.
func GetServer(ctx context.Context, client *iaas.APIClient, projectID, region, serverID string) (*iaas.Server, error) {
	return client.DefaultAPI.GetServer(ctx, projectID, region, serverID).Execute()
}

// UpdateServer applies name/label/metadata changes to an existing server.
func UpdateServer(ctx context.Context, client *iaas.APIClient, projectID, region, serverID string, payload iaas.UpdateServerPayload) (*iaas.Server, error) {
	return client.DefaultAPI.UpdateServer(ctx, projectID, region, serverID).UpdateServerPayload(payload).Execute()
}

// ResizeServer changes a server's machine type.
func ResizeServer(ctx context.Context, client *iaas.APIClient, projectID, region, serverID, machineType string) error {
	payload := iaas.ResizeServerPayload{MachineType: machineType}
	return client.DefaultAPI.ResizeServer(ctx, projectID, region, serverID).ResizeServerPayload(payload).Execute()
}

// StartServer powers on a stopped server.
func StartServer(ctx context.Context, client *iaas.APIClient, projectID, region, serverID string) error {
	return client.DefaultAPI.StartServer(ctx, projectID, region, serverID).Execute()
}

// StopServer powers off a running server.
func StopServer(ctx context.Context, client *iaas.APIClient, projectID, region, serverID string) error {
	return client.DefaultAPI.StopServer(ctx, projectID, region, serverID).Execute()
}

// DeleteServer permanently deletes a server.
func DeleteServer(ctx context.Context, client *iaas.APIClient, projectID, region, serverID string) error {
	return client.DefaultAPI.DeleteServer(ctx, projectID, region, serverID).Execute()
}
