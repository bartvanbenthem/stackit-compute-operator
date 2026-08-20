package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImageChecksumSpec verifies the integrity of uploaded image data. Only used
// at creation time; STACKIT does not support changing a checksum
// afterwards.
type ImageChecksumSpec struct {
	// Algorithm used for the checksum. One of "md5", "sha512".
	// +kubebuilder:validation:Enum=md5;sha512
	Algorithm string `json:"algorithm"`

	// Digest is the hex-encoded checksum of the image data.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]+$`
	Digest string `json:"digest"`
}

// ImageConfigSpec mirrors STACKIT's image boot/device configuration.
// Fields that are nullable-strings in the STACKIT API (allowing an explicit
// "unset this back to default" null) are simplified here to plain optional
// strings; an empty value is treated as "not specified", not as an explicit
// null.
type ImageConfigSpec struct {
	// Architecture of the image's CPU. One of "arm64", "x86".
	// +optional
	Architecture string `json:"architecture,omitempty"`

	// BootMenu enables the BIOS boot menu.
	// +optional
	BootMenu *bool `json:"bootMenu,omitempty"`

	// CdromBus sets the CDROM bus controller type, e.g. "virtio".
	// +optional
	CdromBus string `json:"cdromBus,omitempty"`

	// DiskBus sets the disk bus controller type, e.g. "virtio".
	// +optional
	DiskBus string `json:"diskBus,omitempty"`

	// NicModel sets the virtual NIC model, e.g. "virtio".
	// +optional
	NicModel string `json:"nicModel,omitempty"`

	// OperatingSystem enables OS-specific optimizations. One of "windows",
	// "linux".
	// +optional
	OperatingSystem string `json:"operatingSystem,omitempty"`

	// OperatingSystemDistro is the OS distribution, e.g. "ubuntu".
	// +optional
	OperatingSystemDistro string `json:"operatingSystemDistro,omitempty"`

	// OperatingSystemVersion is the OS version, e.g. "22.04".
	// +optional
	OperatingSystemVersion string `json:"operatingSystemVersion,omitempty"`

	// RescueBus sets the device bus used when the image is used as a rescue
	// image, e.g. "virtio".
	// +optional
	RescueBus string `json:"rescueBus,omitempty"`

	// RescueDevice sets the device used when the image is used as a rescue
	// image. One of "cdrom", "disk".
	// +optional
	RescueDevice string `json:"rescueDevice,omitempty"`

	// SecureBoot enables Secure Boot.
	// +optional
	SecureBoot *bool `json:"secureBoot,omitempty"`

	// Uefi configures UEFI boot.
	// +optional
	Uefi *bool `json:"uefi,omitempty"`

	// VideoModel sets the graphics device model, e.g. "virtio".
	// +optional
	VideoModel string `json:"videoModel,omitempty"`

	// VirtioScsi enables VirtIO SCSI for block device access instead of
	// VirtIO Block.
	// +optional
	VirtioScsi *bool `json:"virtioScsi,omitempty"`
}

// ImageSpec defines the desired state of a STACKIT image.
//
// Creating an image only registers its metadata in STACKIT and returns an
// upload URL (reflected in status.uploadUrl); the operator has no
// declarative way to upload the image bytes themselves, so the image stays
// not-Ready until they are uploaded out-of-band and STACKIT reports the
// image as available. Most real usage is expected to reference an
// already-prepared image via spec.existingId instead of creating one.
type ImageSpec struct {
	// ProjectId is the UUID of the STACKIT project the image belongs to.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ProjectId string `json:"projectId"`

	// Region the image is created in, e.g. "eu01".
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// Name of the image as it will appear in STACKIT. Defaults to the
	// resource's metadata.name when unset.
	// +optional
	Name string `json:"name,omitempty"`

	// DiskFormat of the image. One of "raw", "qcow2", "iso". Required unless
	// existingId is set, since it is only used when creating a new image.
	// +optional
	// +kubebuilder:validation:Enum=raw;qcow2;iso
	DiskFormat string `json:"diskFormat,omitempty"`

	// MinDiskSize is the minimum disk size in GB required to boot the
	// image.
	// +optional
	MinDiskSize int64 `json:"minDiskSize,omitempty"`

	// MinRam is the minimum RAM in MB required to boot the image.
	// +optional
	MinRam int64 `json:"minRam,omitempty"`

	// Protected prevents the image from being deleted.
	// +optional
	Protected *bool `json:"protected,omitempty"`

	// Labels are applied to the image as STACKIT resource labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Checksum verifies the uploaded image data. Only used at creation
	// time.
	// +optional
	Checksum *ImageChecksumSpec `json:"checksum,omitempty"`

	// Config sets the image's boot/device configuration.
	// +optional
	Config *ImageConfigSpec `json:"config,omitempty"`

	// ExistingID references an image that already exists in STACKIT by its
	// UUID. When set, the operator only observes the image: it never
	// creates, updates, or deletes it, and never adds a finalizer. All other
	// spec fields except ProjectId/Region are ignored. Changing this field
	// after the resource has already been created or adopted is unsupported.
	// +optional
	ExistingID *string `json:"existingId,omitempty"`
}

// ImageStatus reflects the last observed state of a STACKIT image.
type ImageStatus struct {
	// ImageId is the UUID of the image in STACKIT, whether created by this
	// resource or adopted via spec.existingId.
	// +optional
	ImageId string `json:"imageId,omitempty"`

	// State mirrors the STACKIT image status, e.g. AVAILABLE, CREATING,
	// ERROR.
	// +optional
	State string `json:"state,omitempty"`

	// UploadUrl is the signed URL image bytes must be PUT to after
	// creation. Only populated immediately after creation; not re-fetched
	// afterwards.
	// +optional
	UploadUrl string `json:"uploadUrl,omitempty"`

	// ImportProgress indicates image import progress in percent.
	// +optional
	ImportProgress int64 `json:"importProgress,omitempty"`

	// Size currently observed on the image in STACKIT, in bytes.
	// +optional
	Size int64 `json:"size,omitempty"`

	// ObservedGeneration is the most recent spec generation reconciled by
	// the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the
	// image's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="ImageId",type=string,JSONPath=`.status.imageId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Image is the Schema for the images API and represents the lifecycle of a
// single STACKIT image, or a reference to one that already exists.
type Image struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageSpec   `json:"spec,omitempty"`
	Status ImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageList contains a list of Image.
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Image `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Image{}, &ImageList{})
}
