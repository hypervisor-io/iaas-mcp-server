package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// VPC subnet tools, mirroring the iaas_vpc_subnet resource. A subnet is a child
// of a VPC (vpc_id in every path). All writes are synchronous. cidr is set at
// create and immutable; only name is mutable on update.

func init() {
	toolRegistrars = append(toolRegistrars, registerVPCSubnetTools)
}

type CreateVPCSubnetInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the parent VPC"`
	Cidr  string `json:"cidr" jsonschema:"IPv4 CIDR for the subnet, e.g. 10.0.1.0/24"`
	Name  string `json:"name,omitempty" jsonschema:"optional name"`
	Type  string `json:"type,omitempty" jsonschema:"public or private"`
}

type GetVPCSubnetInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the parent VPC"`
	ID    string `json:"id" jsonschema:"UUID of the subnet"`
}

type ListVPCSubnetsInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the parent VPC"`
}

type UpdateVPCSubnetInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the parent VPC"`
	ID    string `json:"id" jsonschema:"UUID of the subnet"`
	Name  string `json:"name" jsonschema:"new name (the only mutable field)"`
}

type DeleteVPCSubnetInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the parent VPC"`
	ID    string `json:"id" jsonschema:"UUID of the subnet to delete"`
	Confirmation
}

type VPCSubnetResult struct {
	Subnet map[string]any `json:"subnet"`
}

func createVPCSubnet(ctx context.Context, cl *client.Client, in CreateVPCSubnetInput) (VPCSubnetResult, error) {
	body := map[string]any{"cidr": in.Cidr}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Type != "" {
		body["type"] = in.Type
	}
	obj, err := cl.CreateVPCSubnet(ctx, in.VPCID, body)
	if err != nil {
		return VPCSubnetResult{}, err
	}
	return VPCSubnetResult{Subnet: obj}, nil
}

func getVPCSubnet(ctx context.Context, cl *client.Client, in GetVPCSubnetInput) (VPCSubnetResult, error) {
	obj, err := cl.GetVPCSubnet(ctx, in.VPCID, in.ID)
	if err != nil {
		return VPCSubnetResult{}, err
	}
	return VPCSubnetResult{Subnet: obj}, nil
}

func listVPCSubnets(ctx context.Context, cl *client.Client, in ListVPCSubnetsInput) (ItemsResult, error) {
	return itemsResult(cl.ListVPCSubnets(ctx, in.VPCID))
}

func updateVPCSubnet(ctx context.Context, cl *client.Client, in UpdateVPCSubnetInput) (VPCSubnetResult, error) {
	obj, err := cl.UpdateVPCSubnet(ctx, in.VPCID, in.ID, map[string]any{"name": in.Name})
	if err != nil {
		return VPCSubnetResult{}, err
	}
	return VPCSubnetResult{Subnet: obj}, nil
}

func deleteVPCSubnet(ctx context.Context, cl *client.Client, in DeleteVPCSubnetInput) (DeleteResult, error) {
	if err := cl.DeleteVPCSubnet(ctx, in.VPCID, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerVPCSubnetTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.vpc_subnet.create", Description: "Create a subnet in a VPC."}, createVPCSubnet)
	Register(s, deps, Spec{Name: "user.vpc_subnet.list", Description: "List the subnets of a VPC."}, listVPCSubnets)
	Register(s, deps, Spec{Name: "user.vpc_subnet.get", Description: "Get a VPC subnet by UUID."}, getVPCSubnet)
	Register(s, deps, Spec{Name: "user.vpc_subnet.update", Description: "Update a VPC subnet's name."}, updateVPCSubnet)
	Register(s, deps, Spec{
		Name:        "user.vpc_subnet.delete",
		Description: "Delete a VPC subnet. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVPCSubnet)
}
