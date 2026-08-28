package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePoolVolumeSpec configures the root volume of every node in a node
// pool.
type NodePoolVolumeSpec struct {
	// Size of each node's volume in GB.
	// +kubebuilder:validation:Minimum=1
	Size int64 `json:"size"`

	// Type of the volume, e.g. "storage_premium_perf1". Defaults to SKE's
	// standard volume type when unset.
	// +optional
	Type string `json:"type,omitempty"`
}

// NodePoolSpec configures one worker node pool of a Cluster.
type NodePoolSpec struct {
	// Name of the node pool. Maximum 15 characters.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	Name string `json:"name"`

	// MachineType (flavor) of nodes in this pool, e.g. "c1.2".
	// +kubebuilder:validation:MinLength=1
	MachineType string `json:"machineType"`

	// MachineImageName is the name of the machine image nodes boot from,
	// e.g. "flatcar". See STACKIT's SKE provider-options for valid values.
	// +kubebuilder:validation:MinLength=1
	MachineImageName string `json:"machineImageName"`

	// MachineImageVersion is the version of MachineImageName to use.
	// +kubebuilder:validation:MinLength=1
	MachineImageVersion string `json:"machineImageVersion"`

	// AvailabilityZones the pool's nodes are spread across, e.g. ["eu01-1"].
	// +kubebuilder:validation:MinItems=1
	AvailabilityZones []string `json:"availabilityZones"`

	// Minimum number of nodes in the pool.
	// +kubebuilder:validation:Minimum=0
	Minimum int64 `json:"minimum"`

	// Maximum number of nodes in the pool.
	// +kubebuilder:validation:Minimum=1
	Maximum int64 `json:"maximum"`

	// Volume configures each node's root volume.
	Volume NodePoolVolumeSpec `json:"volume"`

	// AllowSystemComponents allows SKE system components to be scheduled on
	// this pool's nodes. At least one node pool in the cluster must set this
	// to true.
	// +optional
	AllowSystemComponents *bool `json:"allowSystemComponents,omitempty"`

	// Labels are applied to every node in the pool as Kubernetes node
	// labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// ClusterMaintenanceSpec configures the cluster's maintenance window and
// auto-update behavior. Both AutoUpdate settings and the time window are
// required together; SKE applies its own default maintenance window when
// this whole section is left unset.
type ClusterMaintenanceSpec struct {
	// AutoUpdateKubernetesVersion enables automatic patch-version updates of
	// the cluster's Kubernetes version during the maintenance window.
	// +optional
	AutoUpdateKubernetesVersion *bool `json:"autoUpdateKubernetesVersion,omitempty"`

	// AutoUpdateMachineImageVersion enables automatic updates of node pools'
	// machine image version during the maintenance window.
	// +optional
	AutoUpdateMachineImageVersion *bool `json:"autoUpdateMachineImageVersion,omitempty"`

	// Start of the maintenance window, RFC3339, e.g.
	// "2024-01-01T02:00:00Z". Only the time-of-day and day-of-week are
	// meaningful to SKE; the date component is ignored.
	Start metav1.Time `json:"start"`

	// End of the maintenance window, RFC3339.
	End metav1.Time `json:"end"`
}

// ClusterSpec defines the desired state of a STACKIT Kubernetes Engine
// (SKE) cluster.
type ClusterSpec struct {
	// ProjectId is the UUID of the STACKIT project the cluster belongs to.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ProjectId string `json:"projectId"`

	// Region the cluster is created in, e.g. "eu01".
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// Name of the cluster as it will appear in STACKIT. Defaults to the
	// resource's metadata.name when unset. Unlike other resources, this
	// name is SKE's only identifier for the cluster (there is no separate
	// UUID), so it cannot be changed after creation.
	// +optional
	Name string `json:"name,omitempty"`

	// KubernetesVersion is the desired Kubernetes version, e.g. "1.29.3".
	// See STACKIT's SKE provider-options for valid versions. Required unless
	// ExistingClusterName is set, in which case it's ignored.
	// +kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+$`
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`

	// NodePools configures the cluster's worker node pools. At least one
	// pool must set allowSystemComponents to true. Required unless
	// ExistingClusterName is set, in which case it's ignored.
	// +optional
	NodePools []NodePoolSpec `json:"nodePools,omitempty"`

	// Maintenance configures the cluster's maintenance window and
	// auto-update behavior. SKE applies its own default when unset.
	// +optional
	Maintenance *ClusterMaintenanceSpec `json:"maintenance,omitempty"`

	// ExistingClusterName references a cluster that already exists in
	// STACKIT by its name. When set, the operator only observes that
	// cluster: it never creates, updates, or deletes it, and never adds a
	// finalizer. All other spec fields except ProjectId/Region are ignored.
	// Changing this field after the resource has already been created or
	// adopted is unsupported.
	// +optional
	ExistingClusterName *string `json:"existingClusterName,omitempty"`
}

// ClusterStatus reflects the last observed state of a STACKIT Kubernetes
// Engine (SKE) cluster.
type ClusterStatus struct {
	// ClusterName is the name of the cluster in STACKIT, whether created by
	// this resource or adopted via spec.existingClusterName. This doubles as
	// SKE's identifier for the cluster.
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// State mirrors the STACKIT cluster's aggregated status, e.g.
	// STATE_HEALTHY, STATE_CREATING, STATE_UNHEALTHY.
	// +optional
	State string `json:"state,omitempty"`

	// Hibernated reports whether the cluster is currently hibernated.
	// +optional
	Hibernated bool `json:"hibernated,omitempty"`

	// KubernetesVersion currently observed on the cluster in STACKIT.
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`

	// PodAddressRanges lists the CIDR ranges used by pods on the cluster.
	// +optional
	PodAddressRanges []string `json:"podAddressRanges,omitempty"`

	// EgressAddressRanges lists the outgoing CIDR ranges of traffic
	// originating from workload on the cluster.
	// +optional
	EgressAddressRanges []string `json:"egressAddressRanges,omitempty"`

	// Errors lists any cluster errors currently reported by SKE, formatted
	// as "<code>: <message>".
	// +optional
	Errors []string `json:"errors,omitempty"`

	// ObservedGeneration is the most recent spec generation reconciled by
	// the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the
	// cluster's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="K8sVersion",type=string,JSONPath=`.status.kubernetesVersion`
// +kubebuilder:printcolumn:name="Hibernated",type=boolean,JSONPath=`.status.hibernated`
// +kubebuilder:printcolumn:name="ClusterName",type=string,JSONPath=`.status.clusterName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Cluster is the Schema for the clusters API and represents the lifecycle
// of a single STACKIT Kubernetes Engine (SKE) cluster, or a reference to
// one that already exists.
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
