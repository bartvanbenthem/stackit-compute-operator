package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetworkIPv4Spec configures a network's IPv4 subnet. STACKIT auto-allocates
// a subnet of the given prefix length; specifying an explicit prefix is not
// currently supported by this operator.
type NetworkIPv4Spec struct {
	// PrefixLength of the auto-allocated IPv4 subnet, e.g. 24.
	// +kubebuilder:validation:Minimum=1
	PrefixLength int64 `json:"prefixLength"`

	// Nameservers lists DNS servers for the subnet.
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`

	// VpcNetworkRangeId is the UUID of the VPC network range the subnet is
	// allocated from.
	// +optional
	VpcNetworkRangeId string `json:"vpcNetworkRangeId,omitempty"`
}

// NetworkIPv6Spec configures a network's IPv6 subnet. STACKIT auto-allocates
// a subnet of the given prefix length; specifying an explicit prefix is not
// currently supported by this operator.
type NetworkIPv6Spec struct {
	// PrefixLength of the auto-allocated IPv6 subnet, e.g. 64.
	// +kubebuilder:validation:Minimum=1
	PrefixLength int64 `json:"prefixLength"`

	// Nameservers lists DNS servers for the subnet.
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`

	// VpcNetworkRangeId is the UUID of the VPC network range the subnet is
	// allocated from.
	// +optional
	VpcNetworkRangeId string `json:"vpcNetworkRangeId,omitempty"`
}

// NetworkSpec defines the desired state of a STACKIT network.
type NetworkSpec struct {
	// ProjectId is the UUID of the STACKIT project the network belongs to.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	ProjectId string `json:"projectId"`

	// Region the network is created in, e.g. "eu01".
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// Name of the network as it will appear in STACKIT. Defaults to the
	// resource's metadata.name when unset.
	// +optional
	Name string `json:"name,omitempty"`

	// Dhcp enables or disables DHCP for the network.
	// +optional
	Dhcp *bool `json:"dhcp,omitempty"`

	// Routed indicates whether the network is accessible from other
	// networks.
	// +optional
	Routed *bool `json:"routed,omitempty"`

	// RoutingTableId is the UUID of the routing table attached to the
	// network.
	// +optional
	RoutingTableId string `json:"routingTableId,omitempty"`

	// VpcId is the UUID of the STACKIT VPC the network belongs to.
	// +optional
	VpcId string `json:"vpcId,omitempty"`

	// Ipv4 configures the network's IPv4 subnet. Only used at creation
	// time; STACKIT does not support changing a network's subnet
	// afterwards.
	// +optional
	Ipv4 *NetworkIPv4Spec `json:"ipv4,omitempty"`

	// Ipv6 configures the network's IPv6 subnet. Only used at creation
	// time; STACKIT does not support changing a network's subnet
	// afterwards.
	// +optional
	Ipv6 *NetworkIPv6Spec `json:"ipv6,omitempty"`

	// Labels are applied to the network as STACKIT resource labels.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// ExistingID references a network that already exists in STACKIT by its
	// UUID. When set, the operator only observes the network: it never
	// creates, updates, or deletes it, and never adds a finalizer. All other
	// spec fields except ProjectId/Region are ignored. Changing this field
	// after the resource has already been created or adopted is unsupported.
	// +optional
	ExistingID *string `json:"existingId,omitempty"`
}

// NetworkStatus reflects the last observed state of a STACKIT network.
type NetworkStatus struct {
	// NetworkId is the UUID of the network in STACKIT, whether created by
	// this resource or adopted via spec.existingId.
	// +optional
	NetworkId string `json:"networkId,omitempty"`

	// State mirrors the STACKIT network status, e.g. CREATED, UPDATED,
	// FAILED.
	// +optional
	State string `json:"state,omitempty"`

	// Ipv4Prefixes lists the IPv4 subnet prefixes observed on the network.
	// +optional
	Ipv4Prefixes []string `json:"ipv4Prefixes,omitempty"`

	// Ipv4Gateway is the IPv4 gateway observed on the network.
	// +optional
	Ipv4Gateway string `json:"ipv4Gateway,omitempty"`

	// ObservedGeneration is the most recent spec generation reconciled by
	// the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the
	// network's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="NetworkId",type=string,JSONPath=`.status.networkId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Network is the Schema for the networks API and represents the lifecycle
// of a single STACKIT network, or a reference to one that already exists.
type Network struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkSpec   `json:"spec,omitempty"`
	Status NetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkList contains a list of Network.
type NetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Network `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Network{}, &NetworkList{})
}
