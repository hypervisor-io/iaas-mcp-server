package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// NAT gateway tools, mirroring the iaas_nat_gateway resource. A NAT gateway is
// a child of a VPC (at most one per VPC), so every call carries vpc_id. Create
// is async: poll the gateway status to "active". Enable/disable and
// attach/detach subnet are synchronous actions. Delete is synchronous.

func init() {
	toolRegistrars = append(toolRegistrars, registerNatGatewayTools)
}

type CreateNatGatewayInput struct {
	VPCID      string   `json:"vpc_id" jsonschema:"UUID of the VPC to create the NAT gateway in"`
	Name       string   `json:"name,omitempty" jsonschema:"optional name"`
	NatEnabled *bool    `json:"nat_enabled,omitempty" jsonschema:"whether NAT is enabled"`
	SubnetIDs  []string `json:"subnet_ids,omitempty" jsonschema:"optional subnet UUIDs to attach (omit to attach all private subnets)"`
}

type GetNatGatewayInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	ID    string `json:"id" jsonschema:"UUID of the NAT gateway"`
}

// GetVpcNatGatewayInput fetches the single NAT gateway of a VPC (no id needed).
type GetVpcNatGatewayInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the VPC"`
}

type DeleteNatGatewayInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	ID    string `json:"id" jsonschema:"UUID of the NAT gateway to delete"`
	Confirmation
}

type NatGatewayEnableInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	ID    string `json:"id" jsonschema:"UUID of the NAT gateway"`
}

type NatGatewaySubnetInput struct {
	VPCID    string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	ID       string `json:"id" jsonschema:"UUID of the NAT gateway"`
	SubnetID string `json:"subnet_id" jsonschema:"UUID of the subnet to attach or detach"`
}

type NatGatewayResult struct {
	Gateway map[string]any `json:"gateway"`
}

func createNatGateway(ctx context.Context, cl *client.Client, in CreateNatGatewayInput) (NatGatewayResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.NatEnabled != nil {
		body["nat_enabled"] = *in.NatEnabled
	}
	if in.SubnetIDs != nil {
		body["subnet_ids"] = in.SubnetIDs
	}
	created, err := cl.CreateNatGateway(ctx, in.VPCID, body)
	if err != nil {
		return NatGatewayResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return NatGatewayResult{}, fmt.Errorf("create response did not include a nat gateway id")
	}
	// Async: converge on status "active" (fail on failed/error).
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetNatGateway(ctx, in.VPCID, id) },
		"status", []string{"active"}, []string{"failed", "error"}, defaultCreateTimeout)
	if err != nil {
		return NatGatewayResult{}, fmt.Errorf("nat gateway %s did not become active: %w", id, err)
	}
	obj, err := cl.GetNatGateway(ctx, in.VPCID, id)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func getNatGateway(ctx context.Context, cl *client.Client, in GetNatGatewayInput) (NatGatewayResult, error) {
	obj, err := cl.GetNatGateway(ctx, in.VPCID, in.ID)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func getVpcNatGateway(ctx context.Context, cl *client.Client, in GetVpcNatGatewayInput) (NatGatewayResult, error) {
	obj, err := cl.GetVpcNatGateway(ctx, in.VPCID)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func deleteNatGateway(ctx context.Context, cl *client.Client, in DeleteNatGatewayInput) (DeleteResult, error) {
	if err := cl.DeleteNatGateway(ctx, in.VPCID, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func enableNatGateway(ctx context.Context, cl *client.Client, in NatGatewayEnableInput) (NatGatewayResult, error) {
	obj, err := cl.EnableNatGateway(ctx, in.VPCID, in.ID)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func disableNatGateway(ctx context.Context, cl *client.Client, in NatGatewayEnableInput) (NatGatewayResult, error) {
	obj, err := cl.DisableNatGateway(ctx, in.VPCID, in.ID)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func attachNatGatewaySubnet(ctx context.Context, cl *client.Client, in NatGatewaySubnetInput) (NatGatewayResult, error) {
	obj, err := cl.AttachNatGatewaySubnet(ctx, in.VPCID, in.ID, in.SubnetID)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func detachNatGatewaySubnet(ctx context.Context, cl *client.Client, in NatGatewaySubnetInput) (NatGatewayResult, error) {
	obj, err := cl.DetachNatGatewaySubnet(ctx, in.VPCID, in.ID, in.SubnetID)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func registerNatGatewayTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.create",
		Description: "Create a NAT gateway in a VPC and wait until it is active.",
	}, createNatGateway)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.get",
		Description: "Get a NAT gateway by VPC UUID and gateway UUID.",
	}, getNatGateway)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.get_for_vpc",
		Description: "Get the single NAT gateway of a VPC (by VPC UUID).",
	}, getVpcNatGateway)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.delete",
		Description: "Delete a VPC's NAT gateway. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteNatGateway)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.enable",
		Description: "Enable NAT on a gateway.",
	}, enableNatGateway)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.disable",
		Description: "Disable NAT on a gateway.",
	}, disableNatGateway)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.attach_subnet",
		Description: "Attach a subnet to a NAT gateway.",
	}, attachNatGatewaySubnet)
	Register(s, deps, Spec{
		Name:        "user.nat_gateway.detach_subnet",
		Description: "Detach a subnet from a NAT gateway.",
	}, detachNatGatewaySubnet)
}
