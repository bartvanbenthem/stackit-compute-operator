package stackit

import (
	"context"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-vm-operator/api/v1alpha1"
)

// BuildVolumeCreatePayload translates a Volume's spec into a STACKIT
// CreateVolumePayload.
func BuildVolumeCreatePayload(name string, spec computev1alpha1.VolumeSpec) iaas.CreateVolumePayload {
	payload := iaas.CreateVolumePayload{
		AvailabilityZone: spec.AvailabilityZone,
		Name:             utils.Ptr(name),
	}

	if spec.Size > 0 {
		payload.Size = utils.Ptr(spec.Size)
	}
	if spec.PerformanceClass != "" {
		payload.PerformanceClass = utils.Ptr(spec.PerformanceClass)
	}
	if spec.Bootable != nil {
		payload.Bootable = spec.Bootable
	}
	if spec.Description != "" {
		payload.Description = utils.Ptr(spec.Description)
	}
	if spec.Source != nil {
		payload.Source = &iaas.VolumeSource{
			Id:   spec.Source.Id,
			Type: spec.Source.Type,
		}
	}
	if len(spec.Labels) > 0 {
		payload.Labels = toInterfaceMap(spec.Labels)
	}

	return payload
}

// BuildVolumeUpdatePayload translates the fields that differ between the desired
// spec and the observed STACKIT volume into an UpdateVolumePayload. An
// empty name or nil labels leave the corresponding field unset.
func BuildVolumeUpdatePayload(name string, description string, bootable *bool, labels map[string]string) iaas.UpdateVolumePayload {
	payload := iaas.UpdateVolumePayload{}
	if name != "" {
		payload.Name = utils.Ptr(name)
	}
	if description != "" {
		payload.Description = utils.Ptr(description)
	}
	if bootable != nil {
		payload.Bootable = bootable
	}
	if labels != nil {
		payload.Labels = toInterfaceMap(labels)
	}
	return payload
}

// CreateVolume triggers creation of a new volume and returns the initial
// (still-provisioning) volume object.
func CreateVolume(ctx context.Context, client *iaas.APIClient, projectID, region string, payload iaas.CreateVolumePayload) (*iaas.Volume, error) {
	return client.DefaultAPI.CreateVolume(ctx, projectID, region).CreateVolumePayload(payload).Execute()
}

// GetVolume fetches the current state of a volume from STACKIT.
func GetVolume(ctx context.Context, client *iaas.APIClient, projectID, region, volumeID string) (*iaas.Volume, error) {
	return client.DefaultAPI.GetVolume(ctx, projectID, region, volumeID).Execute()
}

// UpdateVolume applies name/label/metadata changes to an existing volume.
func UpdateVolume(ctx context.Context, client *iaas.APIClient, projectID, region, volumeID string, payload iaas.UpdateVolumePayload) (*iaas.Volume, error) {
	return client.DefaultAPI.UpdateVolume(ctx, projectID, region, volumeID).UpdateVolumePayload(payload).Execute()
}

// ResizeVolume grows a volume to the given size in GB.
func ResizeVolume(ctx context.Context, client *iaas.APIClient, projectID, region, volumeID string, size int64) error {
	payload := iaas.ResizeVolumePayload{Size: size}
	return client.DefaultAPI.ResizeVolume(ctx, projectID, region, volumeID).ResizeVolumePayload(payload).Execute()
}

// DeleteVolume permanently deletes a volume.
func DeleteVolume(ctx context.Context, client *iaas.APIClient, projectID, region, volumeID string) error {
	return client.DefaultAPI.DeleteVolume(ctx, projectID, region, volumeID).Execute()
}
