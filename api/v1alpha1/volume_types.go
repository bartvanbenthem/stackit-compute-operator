package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VolumeSourceSpec describes what a Volume is created from.
type VolumeSourceSpec struct {
	// Id is the UUID of the source object (image, volume, snapshot or
	// backup) the volume is created from.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	Id string `json:"id"`

	// Type of the source. One of "image", "volume", "snapshot", "backup".
	// +kubebuilder:validation:Enum=image;volume;snapshot;backup
	Type string `json:"type"`
}

// VolumeSpec defines the desired state of a STACKIT block storage volume.
type VolumeSpec struct {
	// ProjectId is the UUID of the STACKIT project the volume belongs to.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ProjectId string `json:"projectId"`

	// Region the volume is created in, e.g. "eu01".
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// Name of the volume as it will appear in STACKIT. Defaults to the
	// resource's metadata.name when unset.
	// +optional
	Name string `json:"name,omitempty"`

	// AvailabilityZone the volume is placed in, e.g. "eu01-1".
	// +kubebuilder:validation:MinLength=1
	AvailabilityZone string `json:"availabilityZone"`

	// Size of the volume in GB. Required for creation regardless of Source;
	// STACKIT does not default it from an image's minimum disk size.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Size int64 `json:"size,omitempty"`

	// PerformanceClass of the volume, e.g. "storage_premium_perf1".
	// +optional
	PerformanceClass string `json:"performanceClass,omitempty"`

	// Bootable indicates whether the volume can be used as a server's boot
	// volume.
	// +optional
	Bootable *bool `json:"bootable,omitempty"`

	// Description of the volume. Allows up to 255 characters.
	// +optional
	Description string `json:"description,omitempty"`

	// Source the volume is created from. Only used at creation time; STACKIT
	// does not support changing a volume's source afterwards.
	// +optional
	Source *VolumeSourceSpec `json:"source,omitempty"`

	// Labels are applied to the volume as STACKIT resource labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// ExistingID references a volume that already exists in STACKIT by its
	// UUID. When set, the operator only observes the volume: it never
	// creates, updates, or deletes it, and never adds a finalizer. All other
	// spec fields except ProjectId/Region are ignored. Changing this field
	// after the resource has already been created or adopted is unsupported.
	// +optional
	ExistingID *string `json:"existingId,omitempty"`
}

// VolumeStatus reflects the last observed state of a STACKIT volume.
type VolumeStatus struct {
	// VolumeId is the UUID of the volume in STACKIT, whether created by this
	// resource or adopted via spec.existingId.
	// +optional
	VolumeId string `json:"volumeId,omitempty"`

	// State mirrors the STACKIT volume status, e.g. AVAILABLE, ATTACHED,
	// CREATING, ERROR.
	// +optional
	State string `json:"state,omitempty"`

	// Size currently observed on the volume in STACKIT, in GB.
	// +optional
	Size int64 `json:"size,omitempty"`

	// ServerId is the UUID of the server the volume is attached to, if any.
	// +optional
	ServerId string `json:"serverId,omitempty"`

	// ObservedGeneration is the most recent spec generation reconciled by
	// the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the
	// volume's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Size",type=integer,JSONPath=`.status.size`
// +kubebuilder:printcolumn:name="ServerId",type=string,JSONPath=`.status.serverId`
// +kubebuilder:printcolumn:name="VolumeId",type=string,JSONPath=`.status.volumeId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Volume is the Schema for the volumes API and represents the lifecycle of a
// single STACKIT block storage volume, or a reference to one that already
// exists.
type Volume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VolumeSpec   `json:"spec,omitempty"`
	Status VolumeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VolumeList contains a list of Volume.
type VolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Volume `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Volume{}, &VolumeList{})
}
