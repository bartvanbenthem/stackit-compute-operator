package stackit

import (
	"context"

	"github.com/stackitcloud/stackit-sdk-go/core/utils"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-vm-operator/api/v1alpha1"
)

// BuildNetworkCreatePayload translates a Network's spec into a STACKIT
// CreateNetworkPayload.
func BuildNetworkCreatePayload(name string, spec computev1alpha1.NetworkSpec) iaas.CreateNetworkPayload {
	payload := iaas.CreateNetworkPayload{
		Name: name,
	}

	if spec.Dhcp != nil {
		payload.Dhcp = spec.Dhcp
	}
	if spec.Routed != nil {
		payload.Routed = spec.Routed
	}
	if spec.RoutingTableId != "" {
		payload.RoutingTableId = utils.Ptr(spec.RoutingTableId)
	}
	if spec.VpcId != "" {
		payload.VpcId = utils.Ptr(spec.VpcId)
	}
	if spec.Ipv4 != nil {
		ipv4 := iaas.CreateNetworkIPv4WithPrefixLengthAsCreateNetworkIPv4(&iaas.CreateNetworkIPv4WithPrefixLength{
			PrefixLength: spec.Ipv4.PrefixLength,
			Nameservers:  spec.Ipv4.Nameservers,
		})
		if spec.Ipv4.VpcNetworkRangeId != "" {
			ipv4.CreateNetworkIPv4WithPrefixLength.VpcNetworkRangeId = utils.Ptr(spec.Ipv4.VpcNetworkRangeId)
		}
		payload.Ipv4 = &ipv4
	}
	if spec.Ipv6 != nil {
		ipv6 := iaas.CreateNetworkIPv6WithPrefixLengthAsCreateNetworkIPv6(&iaas.CreateNetworkIPv6WithPrefixLength{
			PrefixLength: spec.Ipv6.PrefixLength,
			Nameservers:  spec.Ipv6.Nameservers,
		})
		if spec.Ipv6.VpcNetworkRangeId != "" {
			ipv6.CreateNetworkIPv6WithPrefixLength.VpcNetworkRangeId = utils.Ptr(spec.Ipv6.VpcNetworkRangeId)
		}
		payload.Ipv6 = &ipv6
	}
	if len(spec.Labels) > 0 {
		payload.Labels = toInterfaceMap(spec.Labels)
	}

	return payload
}

// BuildNetworkUpdatePayload translates the fields that differ between the desired
// spec and the observed STACKIT network into a PartialUpdateNetworkPayload.
func BuildNetworkUpdatePayload(name string, dhcp, routed *bool, routingTableId string, labels map[string]string) iaas.PartialUpdateNetworkPayload {
	payload := iaas.PartialUpdateNetworkPayload{}
	if name != "" {
		payload.Name = utils.Ptr(name)
	}
	if dhcp != nil {
		payload.Dhcp = dhcp
	}
	if routed != nil {
		payload.Routed = routed
	}
	if routingTableId != "" {
		payload.RoutingTableId = utils.Ptr(routingTableId)
	}
	if labels != nil {
		payload.Labels = toInterfaceMap(labels)
	}
	return payload
}

// CreateNetwork triggers creation of a new network and returns the initial
// (still-provisioning) network object.
func CreateNetwork(ctx context.Context, client *iaas.APIClient, projectID, region string, payload iaas.CreateNetworkPayload) (*iaas.Network, error) {
	return client.DefaultAPI.CreateNetwork(ctx, projectID, region).CreateNetworkPayload(payload).Execute()
}

// GetNetwork fetches the current state of a network from STACKIT.
func GetNetwork(ctx context.Context, client *iaas.APIClient, projectID, region, networkID string) (*iaas.Network, error) {
	return client.DefaultAPI.GetNetwork(ctx, projectID, region, networkID).Execute()
}

// UpdateNetwork applies name/label/metadata changes to an existing network.
// Unlike the other resources' update calls, STACKIT's PartialUpdateNetwork
// returns no updated object - callers must re-fetch via GetNetwork to
// observe the applied change.
func UpdateNetwork(ctx context.Context, client *iaas.APIClient, projectID, region, networkID string, payload iaas.PartialUpdateNetworkPayload) error {
	return client.DefaultAPI.PartialUpdateNetwork(ctx, projectID, region, networkID).PartialUpdateNetworkPayload(payload).Execute()
}

// DeleteNetwork permanently deletes a network.
func DeleteNetwork(ctx context.Context, client *iaas.APIClient, projectID, region, networkID string) error {
	return client.DefaultAPI.DeleteNetwork(ctx, projectID, region, networkID).Execute()
}
