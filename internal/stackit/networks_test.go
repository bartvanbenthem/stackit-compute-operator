package stackit

import (
	"context"
	"errors"
	"testing"

	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	computev1alpha1 "github.com/bartvanbenthem/stackit-vm-operator/api/v1alpha1"
)

const testNetworkID = "77777777-7777-7777-7777-777777777777"

func TestBuildNetworkCreatePayload_Required(t *testing.T) {
	spec := computev1alpha1.NetworkSpec{}

	payload := BuildNetworkCreatePayload("my-network", spec)

	if payload.Name != "my-network" {
		t.Errorf("Name = %q, want %q", payload.Name, "my-network")
	}
	if payload.Dhcp != nil {
		t.Errorf("Dhcp = %v, want nil", payload.Dhcp)
	}
	if payload.Ipv4 != nil {
		t.Errorf("Ipv4 = %v, want nil", payload.Ipv4)
	}
}

func TestBuildNetworkCreatePayload_Optional(t *testing.T) {
	dhcp, routed := true, true
	spec := computev1alpha1.NetworkSpec{
		Dhcp:           &dhcp,
		Routed:         &routed,
		RoutingTableId: "88888888-8888-8888-8888-888888888888",
		VpcId:          "99999999-9999-9999-9999-999999999999",
		Ipv4: &computev1alpha1.NetworkIPv4Spec{
			PrefixLength: 24,
			Nameservers:  []string{"1.1.1.1"},
		},
		Ipv6: &computev1alpha1.NetworkIPv6Spec{
			PrefixLength: 64,
		},
		Labels: map[string]string{"team": "infra"},
	}

	payload := BuildNetworkCreatePayload("my-network", spec)

	if payload.Dhcp == nil || *payload.Dhcp != dhcp {
		t.Errorf("Dhcp = %v, want %v", payload.Dhcp, dhcp)
	}
	if payload.Routed == nil || *payload.Routed != routed {
		t.Errorf("Routed = %v, want %v", payload.Routed, routed)
	}
	if payload.RoutingTableId == nil || *payload.RoutingTableId != spec.RoutingTableId {
		t.Errorf("RoutingTableId = %v, want %q", payload.RoutingTableId, spec.RoutingTableId)
	}
	if payload.Ipv4 == nil || payload.Ipv4.CreateNetworkIPv4WithPrefixLength == nil ||
		payload.Ipv4.CreateNetworkIPv4WithPrefixLength.PrefixLength != 24 {
		t.Errorf("Ipv4 = %+v, want prefixLength 24", payload.Ipv4)
	}
	if payload.Ipv6 == nil || payload.Ipv6.CreateNetworkIPv6WithPrefixLength == nil ||
		payload.Ipv6.CreateNetworkIPv6WithPrefixLength.PrefixLength != 64 {
		t.Errorf("Ipv6 = %+v, want prefixLength 64", payload.Ipv6)
	}
	if payload.Labels["team"] != "infra" {
		t.Errorf("Labels = %v, want team=infra", payload.Labels)
	}
}

func TestBuildNetworkUpdatePayload(t *testing.T) {
	dhcp, routed := true, false
	payload := BuildNetworkUpdatePayload("new-name", &dhcp, &routed, "table-1", map[string]string{"a": "b"})

	if payload.Name == nil || *payload.Name != "new-name" {
		t.Errorf("Name = %v, want %q", payload.Name, "new-name")
	}
	if payload.Dhcp == nil || *payload.Dhcp != dhcp {
		t.Errorf("Dhcp = %v, want %v", payload.Dhcp, dhcp)
	}
	if payload.Routed == nil || *payload.Routed != routed {
		t.Errorf("Routed = %v, want %v", payload.Routed, routed)
	}
	if payload.RoutingTableId == nil || *payload.RoutingTableId != "table-1" {
		t.Errorf("RoutingTableId = %v, want %q", payload.RoutingTableId, "table-1")
	}
	if payload.Labels["a"] != "b" {
		t.Errorf("Labels = %v, want a=b", payload.Labels)
	}

	empty := BuildNetworkUpdatePayload("", nil, nil, "", nil)
	if empty.Name != nil || empty.Dhcp != nil || empty.Routed != nil || empty.RoutingTableId != nil || empty.Labels != nil {
		t.Errorf("empty BuildNetworkUpdatePayload() = %+v, want all nil", empty)
	}
}

// TestNetworkAPIWrappers exercises the thin CreateNetwork/GetNetwork/.../
// DeleteNetwork wrappers against the SDK's own DefaultAPIServiceMock,
// confirming each wires up to the right SDK method and passes
// results/errors through untouched. UpdateNetwork (PartialUpdateNetwork)
// returns only an error, unlike the other resources' update calls.
func TestNetworkAPIWrappers(t *testing.T) {
	wantErr := errors.New("boom")
	wantNetwork := &iaas.Network{Id: testNetworkID}

	mock := &iaas.DefaultAPIServiceMock{}
	createCalled, getCalled, updateCalled, deleteCalled := false, false, false, false

	createFn := func(r iaas.ApiCreateNetworkRequest) (*iaas.Network, error) {
		createCalled = true
		return wantNetwork, nil
	}
	mock.CreateNetworkExecuteMock = &createFn

	getFn := func(r iaas.ApiGetNetworkRequest) (*iaas.Network, error) {
		getCalled = true
		return nil, wantErr
	}
	mock.GetNetworkExecuteMock = &getFn

	updateFn := func(r iaas.ApiPartialUpdateNetworkRequest) error {
		updateCalled = true
		return wantErr
	}
	mock.PartialUpdateNetworkExecuteMock = &updateFn

	deleteFn := func(r iaas.ApiDeleteNetworkRequest) error {
		deleteCalled = true
		return wantErr
	}
	mock.DeleteNetworkExecuteMock = &deleteFn

	client := &iaas.APIClient{DefaultAPI: mock}
	ctx := context.Background()

	if got, err := CreateNetwork(ctx, client, testProjectID, testRegion, iaas.CreateNetworkPayload{}); !createCalled || err != nil || got != wantNetwork {
		t.Errorf("CreateNetwork() = %v, %v; called=%v, want %v, nil", got, err, createCalled, wantNetwork)
	}
	if _, err := GetNetwork(ctx, client, testProjectID, testRegion, testNetworkID); !getCalled || err != wantErr {
		t.Errorf("GetNetwork() error = %v; called=%v, want %v", err, getCalled, wantErr)
	}
	if err := UpdateNetwork(ctx, client, testProjectID, testRegion, testNetworkID, iaas.PartialUpdateNetworkPayload{}); !updateCalled || err != wantErr {
		t.Errorf("UpdateNetwork() error = %v; called=%v, want %v", err, updateCalled, wantErr)
	}
	if err := DeleteNetwork(ctx, client, testProjectID, testRegion, testNetworkID); !deleteCalled || err != wantErr {
		t.Errorf("DeleteNetwork() error = %v; called=%v, want %v", err, deleteCalled, wantErr)
	}
}
