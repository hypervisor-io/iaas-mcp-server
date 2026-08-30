package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Load balancer security-group rule and sync tools. All synchronous.
//
// The LB-scoped certificate tools (le_certificate, certificate_retry, and
// load_balancer.go's certificate_create/certificate_get/certificate_delete)
// were removed (account-certificates Phase 3 cleanup): the underlying
// `/load-balancer/{lb}/certificate*` and `/load-balancer/{lb}/le-certificate`
// API shims are gone from the Master API entirely. Certificates are managed
// via the account-wide iaas_certificate resource / user.certificate.* tools
// and attached to a listener with iaas_lb_frontend's certificate_ids.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityLBTools)
}

type LBSecurityGroupRulesInput struct {
	LoadBalancerID  string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	SecurityGroupID string `json:"security_group_id" jsonschema:"UUID of the load balancer's security group"`
}

type AddLBSecurityGroupRuleInput struct {
	LoadBalancerID  string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	SecurityGroupID string `json:"security_group_id" jsonschema:"UUID of the load balancer's security group"`
	Direction       string `json:"direction" jsonschema:"ingress or egress"`
	Protocol        string `json:"protocol" jsonschema:"tcp, udp, icmp, icmpv6, or all"`
	IPVersion       string `json:"ip_version" jsonschema:"ipv4 or ipv6"`
	PortRangeMin    *int   `json:"port_range_min,omitempty" jsonschema:"lowest port"`
	PortRangeMax    *int   `json:"port_range_max,omitempty" jsonschema:"highest port"`
	Cidr            string `json:"cidr,omitempty" jsonschema:"source/dest CIDR"`
	RemoteGroupID   string `json:"remote_group_id,omitempty" jsonschema:"peer security group UUID"`
	IPSetID         string `json:"ip_set_id,omitempty" jsonschema:"peer IP set UUID"`
	Description     string `json:"description,omitempty" jsonschema:"optional description"`
}

type RemoveLBSecurityGroupRuleInput struct {
	LoadBalancerID  string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	SecurityGroupID string `json:"security_group_id" jsonschema:"UUID of the load balancer's security group"`
	RuleID          string `json:"rule_id" jsonschema:"UUID of the rule to remove"`
	Confirmation
}

type LBIDInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
}

func listLBSecurityGroupRules(ctx context.Context, cl *client.Client, in LBSecurityGroupRulesInput) (ItemsResult, error) {
	return itemsResult(cl.ListLBSecurityGroupRules(ctx, in.LoadBalancerID, in.SecurityGroupID))
}

func addLBSecurityGroupRule(ctx context.Context, cl *client.Client, in AddLBSecurityGroupRuleInput) (RuleResult, error) {
	body := map[string]any{"direction": in.Direction, "protocol": in.Protocol, "ip_version": in.IPVersion}
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
	obj, err := cl.AddLBSecurityGroupRule(ctx, in.LoadBalancerID, in.SecurityGroupID, body)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: obj}, nil
}

func removeLBSecurityGroupRule(ctx context.Context, cl *client.Client, in RemoveLBSecurityGroupRuleInput) (DeleteResult, error) {
	if err := cl.DeleteLBSecurityGroupRule(ctx, in.LoadBalancerID, in.SecurityGroupID, in.RuleID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.RuleID, Deleted: true}, nil
}

func syncLoadBalancer(ctx context.Context, cl *client.Client, in LBIDInput) (OKResult, error) {
	if err := cl.SyncLoadBalancer(ctx, in.LoadBalancerID); err != nil {
		return OKResult{}, err
	}
	return okResult("load balancer sync requested"), nil
}

func registerParityLBTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.load_balancer.list_security_group_rules", Description: "List a load balancer security group's rules."}, listLBSecurityGroupRules)
	Register(s, deps, Spec{Name: "user.load_balancer.add_security_group_rule", Description: "Add a rule to a load balancer's security group."}, addLBSecurityGroupRule)
	Register(s, deps, Spec{
		Name:        "user.load_balancer.remove_security_group_rule",
		Description: "Remove a rule from a load balancer's security group. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, removeLBSecurityGroupRule)
	Register(s, deps, Spec{Name: "user.load_balancer.sync", Description: "Force a load balancer config sync (HAProxy reload)."}, syncLoadBalancer)
}
