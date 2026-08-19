package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PowerState is the desired power state of a Server.
// +kubebuilder:validation:Enum=Active;Inactive
type PowerState string

const (
	// PowerStateActive requests that the server is running.
	PowerStateActive PowerState = "Active"
	// PowerStateInactive requests that the server is stopped.
	PowerStateInactive PowerState = "Inactive"
)

// BootVolumeSpec configures the root volume a Server is booted from.
type BootVolumeSpec struct {
	// Size of the boot volume in GB. Defaults to the image's minimum disk
	// size when unset.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Size int64 `json:"size,omitempty"`

	// PerformanceClass of the boot volume, e.g. "storage_premium_perf1".
	// +optional
	PerformanceClass string `json:"performanceClass,omitempty"`

	// DeleteOnTermination controls whether the boot volume is deleted
	// together with the server. STACKIT defaults this to true when unset.
	// +optional
	DeleteOnTermination *bool `json:"deleteOnTermination,omitempty"`
}

// ServerSpec defines the desired state of a STACKIT Compute Engine server
// (virtual machine).
type ServerSpec struct {
	// ProjectId is the UUID of the STACKIT project the server belongs to.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ProjectId string `json:"projectId"`

	// Region the server is created in, e.g. "eu01".
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// Name of the server as it will appear in STACKIT. Defaults to the
	// resource's metadata.name when unset.
	// +optional
	Name string `json:"name,omitempty"`

	// MachineType (flavor) of the server, e.g. "c1.2".
	// +kubebuilder:validation:MinLength=1
	MachineType string `json:"machineType"`

	// AvailabilityZone the server is placed in, e.g. "eu01-1". If unset,
	// STACKIT chooses one automatically.
	// +optional
	AvailabilityZone string `json:"availabilityZone,omitempty"`

	// ImageId is the UUID of the image the boot volume is created from.
	// Mutually exclusive with ImageRef; exactly one of the two must be set.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	// +optional
	ImageId string `json:"imageId,omitempty"`

	// ImageRef references an Image resource whose status.imageId is used
	// instead of ImageId. Mutually exclusive with ImageId; exactly one of
	// the two must be set.
	// +optional
	ImageRef *LocalObjectReference `json:"imageRef,omitempty"`

	// BootVolume configures the server's root volume created from ImageId
	// or ImageRef. Ignored (Size/PerformanceClass) when BootVolumeRef is
	// set, since the volume already exists in that case.
	// +optional
	BootVolume BootVolumeSpec `json:"bootVolume,omitempty"`

	// BootVolumeRef references an existing Volume resource to boot from
	// instead of creating a new boot volume from ImageId/ImageRef. The
	// referenced Volume must already exist (be Ready) before the server can
	// be created.
	// +optional
	BootVolumeRef *LocalObjectReference `json:"bootVolumeRef,omitempty"`

	// NetworkId is the UUID of the network the server's primary NIC is
	// attached to. Mutually exclusive with NetworkRef; exactly one of the
	// two must be set.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	// +optional
	NetworkId string `json:"networkId,omitempty"`

	// NetworkRef references a Network resource whose status.networkId is
	// used instead of NetworkId. Mutually exclusive with NetworkId; exactly
	// one of the two must be set.
	// +optional
	NetworkRef *LocalObjectReference `json:"networkRef,omitempty"`

	// KeypairName is the name of an existing SSH keypair to inject into the
	// server.
	// +optional
	KeypairName string `json:"keypairName,omitempty"`

	// SecurityGroups is a list of security group IDs attached to the
	// server's NIC on creation.
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// ServiceAccountMails lists STACKIT service account emails attached to
	// the server.
	// +optional
	ServiceAccountMails []string `json:"serviceAccountMails,omitempty"`

	// Labels are applied to the server as STACKIT resource labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// UserData is cloud-init user data injected into the server at boot. It
	// is base64-encoded by the controller before being sent to STACKIT.
	// +optional
	UserData string `json:"userData,omitempty"`

	// PowerState is the desired power state of the server. Changing this
	// field starts or stops the server without recreating it.
	// +kubebuilder:default=Active
	// +optional
	PowerState PowerState `json:"powerState,omitempty"`
}

// NicStatus mirrors a network interface attached to the server.
type NicStatus struct {
	NetworkId string `json:"networkId,omitempty"`
	Ipv4      string `json:"ipv4,omitempty"`
	Ipv6      string `json:"ipv6,omitempty"`
	Mac       string `json:"mac,omitempty"`
}

// ServerStatus reflects the last observed state of a STACKIT Compute Engine
// server.
type ServerStatus struct {
	// ServerId is the UUID assigned by STACKIT once the server has been
	// created.
	// +optional
	ServerId string `json:"serverId,omitempty"`

	// State mirrors the STACKIT server status, e.g. ACTIVE, INACTIVE,
	// CREATING, ERROR.
	// +optional
	State string `json:"state,omitempty"`

	// PowerStatus mirrors the STACKIT server power status, e.g. RUNNING,
	// STOPPED.
	// +optional
	PowerStatus string `json:"powerStatus,omitempty"`

	// MachineType currently observed on the server in STACKIT.
	// +optional
	MachineType string `json:"machineType,omitempty"`

	// Nics lists the network interfaces attached to the server.
	// +optional
	Nics []NicStatus `json:"nics,omitempty"`

	// ObservedGeneration is the most recent spec generation reconciled by
	// the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the
	// server's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Power",type=string,JSONPath=`.status.powerStatus`
// +kubebuilder:printcolumn:name="MachineType",type=string,JSONPath=`.spec.machineType`
// +kubebuilder:printcolumn:name="ServerId",type=string,JSONPath=`.status.serverId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Server is the Schema for the servers API and represents the lifecycle of a
// single STACKIT Compute Engine server (virtual machine).
type Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServerSpec   `json:"spec,omitempty"`
	Status ServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServerList contains a list of Server.
type ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Server `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Server{}, &ServerList{})
}
