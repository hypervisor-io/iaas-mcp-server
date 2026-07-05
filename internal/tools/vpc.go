package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Golden VPC tools. VPC create is SYNCHRONOUS: the create response already
// carries the new id and vni_number (no task, no waiter). user.vpc.create does
// a create-then-readback (GetVPC by the returned id) so the tool returns the
// full SHOW object, including the eager-loaded subnets, rather than just the
// thinner create envelope. attach_instance mirrors the iaas_instance_vpc_
// attachment resource's core enable + read-back of the attached IPs.

// ── inputs / outputs ────────────────────────────────────────────────────────

// CreateVPCInput is user.vpc.create's arguments. name, cidr, and
// hypervisor_group_id are required; description is optional (omitted, not sent
// as null, when empty).
type CreateVPCInput struct {
	Name              string `json:"name" jsonschema:"VPC name: max 16 chars, lowercase letters and digits only (^[a-z0-9]+$)"`
	Cidr              string `json:"cidr" jsonschema:"RFC1918 CIDR block, e.g. 10.0.0.0/24"`
	HypervisorGroupID string `json:"hypervisor_group_id" jsonschema:"UUID of the VPC-enabled hypervisor group (location)"`
	Description       string `json:"description,omitempty" jsonschema:"optional free-text description"`
}

// GetVPCInput identifies one VPC.
type GetVPCInput struct {
	ID string `json:"id" jsonschema:"UUID of the VPC"`
}

// ListVPCsInput takes no arguments (tenant scope is implicit in the token).
type ListVPCsInput struct{}

// AttachInstanceInput attaches an instance to a VPC subnet. All three ids are
// required, matching the controller's EnableVpcRequest (vpc_id and
// vpc_subnet_id are both required|uuid).
type AttachInstanceInput struct {
	InstanceID  string `json:"instance_id" jsonschema:"UUID of the instance to attach"`
	VPCID       string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	VPCSubnetID string `json:"vpc_subnet_id" jsonschema:"UUID of the VPC subnet to attach into"`
}

// VPCResult wraps a single VPC object.
type VPCResult struct {
	VPC map[string]any `json:"vpc"`
}

// VPCListResult wraps the VPC list plus its count.
type VPCListResult struct {
	VPCs  []map[string]any `json:"vpcs"`
	Count int              `json:"count"`
}

// AttachResult reports a VPC attachment and reads back the attached IPs.
type AttachResult struct {
	InstanceID  string           `json:"instance_id"`
	VPCID       string           `json:"vpc_id"`
	VPCSubnetID string           `json:"vpc_subnet_id"`
	Attached    bool             `json:"attached"`
	IPs         []map[string]any `json:"ips"`
}

// ── handlers ────────────────────────────────────────────────────────────────

// createVPC creates the VPC (sync) then reads it back by id so the result
// carries the full SHOW object (with subnets).
func createVPC(ctx context.Context, cl *client.Client, in CreateVPCInput) (VPCResult, error) {
	body := map[string]any{
		"name":                in.Name,
		"cidr":                in.Cidr,
		"hypervisor_group_id": in.HypervisorGroupID,
	}
	if in.Description != "" {
		body["description"] = in.Description
	}

	created, err := cl.CreateVPC(ctx, body)
	if err != nil {
		return VPCResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return VPCResult{}, fmt.Errorf("create response did not include a vpc id")
	}

	// Create-readback: return the full SHOW object (subnets eager-loaded).
	obj, err := cl.GetVPC(ctx, id)
	if err != nil {
		return VPCResult{}, err
	}
	return VPCResult{VPC: obj}, nil
}

// getVPC returns a single VPC; a 404 maps to a not-found error.
func getVPC(ctx context.Context, cl *client.Client, in GetVPCInput) (VPCResult, error) {
	obj, err := cl.GetVPC(ctx, in.ID)
	if err != nil {
		return VPCResult{}, err
	}
	return VPCResult{VPC: obj}, nil
}

// listVPCs returns every VPC visible to the token (auto-paginated).
func listVPCs(ctx context.Context, cl *client.Client, _ ListVPCsInput) (VPCListResult, error) {
	items, err := cl.ListVPCs(ctx)
	if err != nil {
		return VPCListResult{}, err
	}
	return VPCListResult{VPCs: items, Count: len(items)}, nil
}

// attachInstance enables the VPC on the instance (which auto-assigns the lowest
// free IP as primary, synchronously at the DB level) then reads back the
// attached IPs, mirroring the provider resource's enable + read-back.
func attachInstance(ctx context.Context, cl *client.Client, in AttachInstanceInput) (AttachResult, error) {
	if _, err := cl.EnableInstanceVpc(ctx, in.InstanceID, in.VPCID, in.VPCSubnetID); err != nil {
		return AttachResult{}, err
	}
	ips, err := cl.ListInstanceVpcIPs(ctx, in.InstanceID)
	if err != nil {
		return AttachResult{}, err
	}
	return AttachResult{
		InstanceID:  in.InstanceID,
		VPCID:       in.VPCID,
		VPCSubnetID: in.VPCSubnetID,
		Attached:    true,
		IPs:         ips,
	}, nil
}

// registerVPCTools registers the three golden VPC tools.
func registerVPCTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name: "user.vpc.create",
		Description: "Create a VPC (isolated private network) and return it with its server-assigned id, " +
			"VNI, and subnets.",
	}, createVPC)

	Register(s, deps, Spec{
		Name:        "user.vpc.list",
		Description: "List all VPCs owned by the caller.",
	}, listVPCs)

	Register(s, deps, Spec{
		Name:        "user.vpc.get",
		Description: "Get a single VPC by its UUID.",
	}, getVPC)

	Register(s, deps, Spec{
		Name: "user.vpc.attach_instance",
		Description: "Attach an instance to a VPC subnet. The server auto-assigns the lowest free IP as " +
			"primary; returns the attached IPs.",
	}, attachInstance)
}
