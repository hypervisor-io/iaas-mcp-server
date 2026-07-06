package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Security group tools, mirroring the iaas_security_group provider resource and
// the SecurityGroupController. A security group is a named collection of
// firewall rules; instances attach to it many-to-many. All writes are
// synchronous (no waiter). Rules and attached instances are managed via child
// actions. Only the whole-group delete is confirm-gated; the granular rule /
// attachment actions are reversible and ungated.

func init() {
	toolRegistrars = append(toolRegistrars, registerSecurityGroupTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type CreateSecurityGroupInput struct {
	Name        string `json:"name" jsonschema:"name of the security group"`
	Description string `json:"description,omitempty" jsonschema:"optional description"`
}

type GetSecurityGroupInput struct {
	ID string `json:"id" jsonschema:"UUID of the security group"`
}

type ListSecurityGroupsInput struct{}

type DeleteSecurityGroupInput struct {
	ID string `json:"id" jsonschema:"UUID of the security group to delete"`
	Confirmation
}

type AddSecurityGroupRuleInput struct {
	SecurityGroupID string `json:"security_group_id" jsonschema:"UUID of the security group"`
	Direction       string `json:"direction" jsonschema:"ingress or egress"`
	Protocol        string `json:"protocol" jsonschema:"tcp, udp, icmp, icmpv6, or all"`
	IPVersion       string `json:"ip_version" jsonschema:"ipv4 or ipv6"`
	PortRangeMin    *int   `json:"port_range_min,omitempty" jsonschema:"lowest port (required for tcp/udp)"`
	PortRangeMax    *int   `json:"port_range_max,omitempty" jsonschema:"highest port (required for tcp/udp)"`
	Cidr            string `json:"cidr,omitempty" jsonschema:"source/dest CIDR (mutually exclusive with remote_group_id/ip_set_id)"`
	RemoteGroupID   string `json:"remote_group_id,omitempty" jsonschema:"UUID of another security group as the peer"`
	IPSetID         string `json:"ip_set_id,omitempty" jsonschema:"UUID of an IP set as the peer"`
	Description     string `json:"description,omitempty" jsonschema:"optional rule description"`
}

type RemoveSecurityGroupRuleInput struct {
	SecurityGroupID string `json:"security_group_id" jsonschema:"UUID of the security group"`
	RuleID          string `json:"rule_id" jsonschema:"UUID of the rule to remove"`
	Confirmation
}

type SecurityGroupInstancesInput struct {
	SecurityGroupID string   `json:"security_group_id" jsonschema:"UUID of the security group"`
	InstanceIDs     []string `json:"instance_ids" jsonschema:"UUIDs of the instances to attach or detach"`
}

type SecurityGroupResult struct {
	SecurityGroup     map[string]any   `json:"security_group"`
	AttachedInstances []map[string]any `json:"attached_instances,omitempty"`
}

type SecurityGroupListResult struct {
	SecurityGroups []map[string]any `json:"security_groups"`
	Count          int              `json:"count"`
}

type RuleResult struct {
	Rule map[string]any `json:"rule"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func createSecurityGroup(ctx context.Context, cl *client.Client, in CreateSecurityGroupInput) (SecurityGroupResult, error) {
	body := map[string]any{"name": in.Name}
	if in.Description != "" {
		body["description"] = in.Description
	}
	obj, err := cl.CreateSecurityGroup(ctx, body)
	if err != nil {
		return SecurityGroupResult{}, err
	}
	return SecurityGroupResult{SecurityGroup: obj}, nil
}

func getSecurityGroup(ctx context.Context, cl *client.Client, in GetSecurityGroupInput) (SecurityGroupResult, error) {
	env, err := cl.GetSecurityGroupEnvelope(ctx, in.ID)
	if err != nil {
		return SecurityGroupResult{}, err
	}
	sg, _ := env["security_group"].(map[string]any)
	return SecurityGroupResult{
		SecurityGroup:     sg,
		AttachedInstances: asObjectList(env["attached_instances"]),
	}, nil
}

func listSecurityGroups(ctx context.Context, cl *client.Client, _ ListSecurityGroupsInput) (SecurityGroupListResult, error) {
	items, err := cl.ListSecurityGroups(ctx)
	if err != nil {
		return SecurityGroupListResult{}, err
	}
	return SecurityGroupListResult{SecurityGroups: items, Count: len(items)}, nil
}

func deleteSecurityGroup(ctx context.Context, cl *client.Client, in DeleteSecurityGroupInput) (DeleteResult, error) {
	if err := cl.DeleteSecurityGroup(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func addSecurityGroupRule(ctx context.Context, cl *client.Client, in AddSecurityGroupRuleInput) (RuleResult, error) {
	body := map[string]any{
		"direction":  in.Direction,
		"protocol":   in.Protocol,
		"ip_version": in.IPVersion,
	}
	if in.PortRangeMin != nil {
		body["port_range_min"] = *in.PortRangeMin
	}
	if in.PortRangeMax != nil {
		body["port_range_max"] = *in.PortRangeMax
	}
	if in.Cidr != "" {
		body["cidr"] = in.Cidr
	}
	if in.RemoteGroupID != "" {
		body["remote_group_id"] = in.RemoteGroupID
	}
	if in.IPSetID != "" {
		body["ip_set_id"] = in.IPSetID
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	obj, err := cl.AddSecurityGroupRule(ctx, in.SecurityGroupID, body)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: obj}, nil
}

func removeSecurityGroupRule(ctx context.Context, cl *client.Client, in RemoveSecurityGroupRuleInput) (OKResult, error) {
	if err := cl.DeleteSecurityGroupRule(ctx, in.SecurityGroupID, in.RuleID); err != nil {
		return OKResult{}, err
	}
	return okResult("rule removed"), nil
}

func attachSecurityGroupInstances(ctx context.Context, cl *client.Client, in SecurityGroupInstancesInput) (OKResult, error) {
	if err := cl.AttachSecurityGroupInstances(ctx, in.SecurityGroupID, in.InstanceIDs); err != nil {
		return OKResult{}, err
	}
	return okResult("instances attached"), nil
}

func detachSecurityGroupInstances(ctx context.Context, cl *client.Client, in SecurityGroupInstancesInput) (OKResult, error) {
	if err := cl.DetachSecurityGroupInstances(ctx, in.SecurityGroupID, in.InstanceIDs); err != nil {
		return OKResult{}, err
	}
	return okResult("instances detached"), nil
}

func registerSecurityGroupTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.security_group.create",
		Description: "Create a security group (a named collection of firewall rules).",
	}, createSecurityGroup)

	Register(s, deps, Spec{
		Name:        "user.security_group.list",
		Description: "List all security groups visible to the caller (own and global).",
	}, listSecurityGroups)

	Register(s, deps, Spec{
		Name:        "user.security_group.get",
		Description: "Get a security group by UUID, including its rules and attached instances.",
	}, getSecurityGroup)

	Register(s, deps, Spec{
		Name:        "user.security_group.delete",
		Description: "Delete a security group. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteSecurityGroup)

	Register(s, deps, Spec{
		Name:        "user.security_group.add_rule",
		Description: "Add a single firewall rule to a security group.",
	}, addSecurityGroupRule)

	Register(s, deps, Spec{
		Name:        "user.security_group.remove_rule",
		Description: "Remove a single firewall rule from a security group by rule UUID. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, removeSecurityGroupRule)

	Register(s, deps, Spec{
		Name:        "user.security_group.attach_instances",
		Description: "Attach a security group to one or more instances.",
	}, attachSecurityGroupInstances)

	Register(s, deps, Spec{
		Name:        "user.security_group.detach_instances",
		Description: "Detach a security group from one or more instances.",
	}, detachSecurityGroupInstances)
}
