package stackit

import (
	"context"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-vm-operator/api/v1alpha1"
)

// BuildImageCreatePayload translates an Image's spec into a STACKIT
// CreateImagePayload.
func BuildImageCreatePayload(name string, spec computev1alpha1.ImageSpec) iaas.CreateImagePayload {
	payload := iaas.CreateImagePayload{
		Name:       name,
		DiskFormat: spec.DiskFormat,
	}

	if spec.MinDiskSize > 0 {
		payload.MinDiskSize = utils.Ptr(spec.MinDiskSize)
	}
	if spec.MinRam > 0 {
		payload.MinRam = utils.Ptr(spec.MinRam)
	}
	if spec.Protected != nil {
		payload.Protected = spec.Protected
	}
	if len(spec.Labels) > 0 {
		payload.Labels = toInterfaceMap(spec.Labels)
	}
	if spec.Checksum != nil {
		payload.Checksum = &iaas.ImageChecksum{
			Algorithm: spec.Checksum.Algorithm,
			Digest:    spec.Checksum.Digest,
		}
	}
	if spec.Config != nil {
		payload.Config = toImageConfig(spec.Config)
	}

	return payload
}

// BuildImageUpdatePayload translates the fields that differ between the desired
// spec and the observed STACKIT image into an UpdateImagePayload. Note that
// Checksum is create-only and has no equivalent here.
func BuildImageUpdatePayload(spec computev1alpha1.ImageSpec) iaas.UpdateImagePayload {
	payload := iaas.UpdateImagePayload{}
	if spec.Name != "" {
		payload.Name = utils.Ptr(spec.Name)
	}
	if spec.DiskFormat != "" {
		payload.DiskFormat = utils.Ptr(spec.DiskFormat)
	}
	if spec.MinDiskSize > 0 {
		payload.MinDiskSize = utils.Ptr(spec.MinDiskSize)
	}
	if spec.MinRam > 0 {
		payload.MinRam = utils.Ptr(spec.MinRam)
	}
	if spec.Protected != nil {
		payload.Protected = spec.Protected
	}
	if len(spec.Labels) > 0 {
		payload.Labels = toInterfaceMap(spec.Labels)
	}
	if spec.Config != nil {
		payload.Config = toImageConfig(spec.Config)
	}
	return payload
}

// toImageConfig translates an ImageConfigSpec into the SDK's ImageConfig,
// wrapping the fields the SDK models as tri-state NullableString.
func toImageConfig(spec *computev1alpha1.ImageConfigSpec) *iaas.ImageConfig {
	cfg := &iaas.ImageConfig{
		BootMenu:   spec.BootMenu,
		SecureBoot: spec.SecureBoot,
		Uefi:       spec.Uefi,
		VirtioScsi: spec.VirtioScsi,
	}
	if spec.Architecture != "" {
		cfg.Architecture = utils.Ptr(spec.Architecture)
	}
	if spec.OperatingSystem != "" {
		cfg.OperatingSystem = utils.Ptr(spec.OperatingSystem)
	}
	if spec.CdromBus != "" {
		cfg.CdromBus = *iaas.NewNullableString(utils.Ptr(spec.CdromBus))
	}
	if spec.DiskBus != "" {
		cfg.DiskBus = *iaas.NewNullableString(utils.Ptr(spec.DiskBus))
	}
	if spec.NicModel != "" {
		cfg.NicModel = *iaas.NewNullableString(utils.Ptr(spec.NicModel))
	}
	if spec.OperatingSystemDistro != "" {
		cfg.OperatingSystemDistro = *iaas.NewNullableString(utils.Ptr(spec.OperatingSystemDistro))
	}
	if spec.OperatingSystemVersion != "" {
		cfg.OperatingSystemVersion = *iaas.NewNullableString(utils.Ptr(spec.OperatingSystemVersion))
	}
	if spec.RescueBus != "" {
		cfg.RescueBus = *iaas.NewNullableString(utils.Ptr(spec.RescueBus))
	}
	if spec.RescueDevice != "" {
		cfg.RescueDevice = *iaas.NewNullableString(utils.Ptr(spec.RescueDevice))
	}
	if spec.VideoModel != "" {
		cfg.VideoModel = *iaas.NewNullableString(utils.Ptr(spec.VideoModel))
	}
	return cfg
}

// CreateImage registers a new image and returns its ID and the upload URL
// image bytes must be PUT to out-of-band; it does not return a full Image.
func CreateImage(ctx context.Context, client *iaas.APIClient, projectID, region string, payload iaas.CreateImagePayload) (*iaas.ImageCreateResponse, error) {
	return client.DefaultAPI.CreateImage(ctx, projectID, region).CreateImagePayload(payload).Execute()
}

// GetImage fetches the current state of an image from STACKIT.
func GetImage(ctx context.Context, client *iaas.APIClient, projectID, region, imageID string) (*iaas.Image, error) {
	return client.DefaultAPI.GetImage(ctx, projectID, region, imageID).Execute()
}

// UpdateImage applies metadata changes to an existing image.
func UpdateImage(ctx context.Context, client *iaas.APIClient, projectID, region, imageID string, payload iaas.UpdateImagePayload) (*iaas.Image, error) {
	return client.DefaultAPI.UpdateImage(ctx, projectID, region, imageID).UpdateImagePayload(payload).Execute()
}

// DeleteImage permanently deletes an image.
func DeleteImage(ctx context.Context, client *iaas.APIClient, projectID, region, imageID string) error {
	return client.DefaultAPI.DeleteImage(ctx, projectID, region, imageID).Execute()
}
