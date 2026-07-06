package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Load balancer security-group rule, sync, and Let's Encrypt certificate tools.
// All synchronous.

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

type LBLetsEncryptInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	Domains        string `json:"domains" jsonschema:"comma-separated domains (first is the CN, rest are SANs)"`
}

type LBCertificateRetryInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	CertificateID  string `json:"certificate_id" jsonschema:"UUID of the certificate to retry"`
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

func lbLetsEncryptCertificate(ctx context.Context, cl *client.Client, in LBLetsEncryptInput) (LBCertificateResult, error) {
	obj, err := cl.CreateLBLetsEncryptCertificate(ctx, in.LoadBalancerID, map[string]any{"domains": in.Domains})
	if err != nil {
		return LBCertificateResult{}, err
	}
	return LBCertificateResult{Certificate: obj}, nil
}

func lbCertificateRetry(ctx context.Context, cl *client.Client, in LBCertificateRetryInput) (OKResult, error) {
	if err := cl.RetryLBCertificate(ctx, in.LoadBalancerID, in.CertificateID); err != nil {
		return OKResult{}, err
	}
	return okResult("certificate issuance retry requested"), nil
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
	Register(s, deps, Spec{Name: "user.load_balancer.le_certificate", Description: "Issue a Let's Encrypt certificate for domains on a load balancer."}, lbLetsEncryptCertificate)
	Register(s, deps, Spec{Name: "user.load_balancer.certificate_retry", Description: "Retry issuing a failed load balancer certificate."}, lbCertificateRetry)
}
