package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Instance-VPC attachment IP management, mirroring the iaas_instance_vpc_
// attachment resource. The golden user.vpc.attach_instance tool covers enable;
// these tools cover disable and per-IP management. All synchronous (no waiter).
// A VPC IP is referenced by its pool-row UUID (vpc_ip_id), obtainable from
// list_ips / list_available_ips - there is no free-form dotted-quad field.

func init() {
	toolRegistrars = append(toolRegistrars, registerInstanceVpcTools)
}

type InstanceVpcDisableInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance to detach from its VPC"`
}

type InstanceVpcListInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
}

// AddInstanceVpcIPInput adds an IP to the instance's attached subnet. Provide
// either ip_id (a free pool-row UUID from list_available_ips) or random:true.
type AddInstanceVpcIPInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	IPID       string `json:"ip_id,omitempty" jsonschema:"UUID of a free pool IP to attach"`
	Random     bool   `json:"random,omitempty" jsonschema:"set true to attach a random free IP instead of ip_id"`
}

type InstanceVpcIPInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	VpcIPID    string `json:"vpc_ip_id" jsonschema:"UUID of the attached VPC IP row"`
}

// RemoveInstanceVpcIPInput mirrors InstanceVpcIPInput but embeds Confirmation,
// so the confirm gate applies to remove_ip (destructive) but not to
// set_primary_ip (reversible), which shares the plain InstanceVpcIPInput.
type RemoveInstanceVpcIPInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	VpcIPID    string `json:"vpc_ip_id" jsonschema:"UUID of the attached VPC IP row to remove"`
	Confirmation
}

type VpcIPResult struct {
	VpcIP map[string]any `json:"vpc_ip"`
}

type VpcIPListResult struct {
	IPs   []map[string]any `json:"ips"`
	Count int              `json:"count"`
}

func disableInstanceVpc(ctx context.Context, cl *client.Client, in InstanceVpcDisableInput) (OKResult, error) {
	if err := cl.DisableInstanceVpc(ctx, in.InstanceID); err != nil {
		return OKResult{}, err
	}
	return okResult("vpc detached from instance"), nil
}

func listInstanceVpcIPs(ctx context.Context, cl *client.Client, in InstanceVpcListInput) (VpcIPListResult, error) {
	items, err := cl.ListInstanceVpcIPs(ctx, in.InstanceID)
	if err != nil {
		return VpcIPListResult{}, err
	}
	return VpcIPListResult{IPs: items, Count: len(items)}, nil
}

func listInstanceAvailableVpcIPs(ctx context.Context, cl *client.Client, in InstanceVpcListInput) (VpcIPListResult, error) {
	items, err := cl.ListInstanceAvailableVpcIPs(ctx, in.InstanceID)
	if err != nil {
		return VpcIPListResult{}, err
	}
	return VpcIPListResult{IPs: items, Count: len(items)}, nil
}

func addInstanceVpcIP(ctx context.Context, cl *client.Client, in AddInstanceVpcIPInput) (VpcIPResult, error) {
	body := map[string]any{}
	switch {
	case in.Random:
		body["random"] = true
	case in.IPID != "":
		body["ip_id"] = in.IPID
	default:
		return VpcIPResult{}, fmt.Errorf("provide either ip_id or random:true")
	}
	obj, err := cl.AddInstanceVpcIP(ctx, in.InstanceID, body)
	if err != nil {
		return VpcIPResult{}, err
	}
	return VpcIPResult{VpcIP: obj}, nil
}

func setPrimaryInstanceVpcIP(ctx context.Context, cl *client.Client, in InstanceVpcIPInput) (OKResult, error) {
	if err := cl.SetPrimaryInstanceVpcIP(ctx, in.InstanceID, in.VpcIPID); err != nil {
		return OKResult{}, err
	}
	return okResult("primary vpc ip set"), nil
}

func removeInstanceVpcIP(ctx context.Context, cl *client.Client, in RemoveInstanceVpcIPInput) (OKResult, error) {
	if err := cl.RemoveInstanceVpcIP(ctx, in.InstanceID, in.VpcIPID); err != nil {
		return OKResult{}, err
	}
	return okResult("vpc ip removed"), nil
}

func registerInstanceVpcTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.instance_vpc.disable",
		Description: "Detach an instance from its VPC (releases all attached VPC IPs).",
	}, disableInstanceVpc)
	Register(s, deps, Spec{
		Name:        "user.instance_vpc.list_ips",
		Description: "List the VPC IPs currently attached to an instance.",
	}, listInstanceVpcIPs)
	Register(s, deps, Spec{
		Name:        "user.instance_vpc.list_available_ips",
		Description: "List free VPC IPs available to attach in the instance's current subnet.",
	}, listInstanceAvailableVpcIPs)
	Register(s, deps, Spec{
		Name:        "user.instance_vpc.add_ip",
		Description: "Attach an additional VPC IP to an instance (by ip_id or random).",
	}, addInstanceVpcIP)
	Register(s, deps, Spec{
		Name:        "user.instance_vpc.set_primary_ip",
		Description: "Set an attached VPC IP as the instance's primary.",
	}, setPrimaryInstanceVpcIP)
	Register(s, deps, Spec{
		Name:        "user.instance_vpc.remove_ip",
		Description: "Remove an attached VPC IP from an instance (cannot remove the last one). DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, removeInstanceVpcIP)
}
