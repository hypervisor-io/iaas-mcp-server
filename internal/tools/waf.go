package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Load balancer WAF policy tools (S6 Task 1). The policy is a singleton child
// of a load balancer; all writes are synchronous (the API regenerates config
// and reloads the appliance internally, or is a safe no-op when the load
// balancer isn't active yet).
//
// Field names mirror the wire contract's customer-facing names, not the raw
// `lb_waf_policies` DB columns: "sensitivity" (not "paranoia_level") and
// "exclusions" (not "crs_exclusions") - product-identity lock (no "paranoia"
// in any payload), see docs/superpowers/plans/LB-WAF-COORDINATION.md in the
// Master repo. There is no retention_days/s3_archive field on the policy -
// those are global settings, not per-policy.

func init() {
	toolRegistrars = append(toolRegistrars, registerWafTools)
}

// Policy is omitempty: the SDK auto-generates the tool's output JSON schema
// from this struct, and a plain `map[string]any` field's generated schema
// requires type "object" - it does NOT allow JSON null. get_policy legitimately
// returns no policy (mirrors the API's 200 {"policy":null} for an LB with no
// WAF configured), so the field must be omitted from the response entirely in
// that case rather than marshaled as `"policy":null`, or output-schema
// validation rejects the tool's own successful response (verified: without
// omitempty, a nil Policy fails with "validating /properties/policy: type:
// ... has type \"null\", want \"object\"").
type WafPolicyResult struct {
	Policy map[string]any `json:"policy,omitempty"`
}

type GetWafPolicyInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
}

type SetWafPolicyInput struct {
	LoadBalancerID     string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	Enabled            bool   `json:"enabled" jsonschema:"whether the WAF is active"`
	Mode               string `json:"mode" jsonschema:"off, detect, or block"`
	FailMode           string `json:"fail_mode,omitempty" jsonschema:"open (default) or close"`
	Sensitivity        *int   `json:"sensitivity,omitempty" jsonschema:"rule sensitivity level 1-4 (default 1)"`
	ResponseInspection *bool  `json:"response_inspection,omitempty" jsonschema:"inspect response bodies"`
	CrsEnabled         *bool  `json:"crs_enabled,omitempty" jsonschema:"enable the managed core rule set"`
	FullAudit          *bool  `json:"full_audit,omitempty" jsonschema:"write heavy per-transaction audit"`
	Exclusions         []int  `json:"exclusions,omitempty" jsonschema:"managed rule ids to exclude from evaluation"`
}

type DisableWafInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	Confirmation
}

func getWafPolicy(ctx context.Context, cl *client.Client, in GetWafPolicyInput) (WafPolicyResult, error) {
	obj, err := cl.GetLBWafPolicy(ctx, in.LoadBalancerID)
	if err != nil {
		if client.IsNotFound(err) {
			return WafPolicyResult{Policy: nil}, nil
		}
		return WafPolicyResult{}, err
	}
	return WafPolicyResult{Policy: obj}, nil
}

func setWafPolicy(ctx context.Context, cl *client.Client, in SetWafPolicyInput) (WafPolicyResult, error) {
	body := map[string]any{"enabled": in.Enabled, "mode": in.Mode}
	if in.FailMode != "" {
		body["fail_mode"] = in.FailMode
	}
	if in.Sensitivity != nil {
		body["sensitivity"] = *in.Sensitivity
	}
	if in.ResponseInspection != nil {
		body["response_inspection"] = *in.ResponseInspection
	}
	if in.CrsEnabled != nil {
		body["crs_enabled"] = *in.CrsEnabled
	}
	if in.FullAudit != nil {
		body["full_audit"] = *in.FullAudit
	}
	if in.Exclusions != nil {
		body["exclusions"] = in.Exclusions
	}
	obj, err := cl.PutLBWafPolicy(ctx, in.LoadBalancerID, body)
	if err != nil {
		return WafPolicyResult{}, err
	}
	return WafPolicyResult{Policy: obj}, nil
}

func disableWaf(ctx context.Context, cl *client.Client, in DisableWafInput) (OKResult, error) {
	if err := cl.DeleteLBWafPolicy(ctx, in.LoadBalancerID); err != nil {
		return OKResult{}, err
	}
	return okResult("WAF disabled"), nil
}

func registerWafTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.load_balancer.waf.get_policy", Description: "Get the WAF policy of a load balancer (null if none)."}, getWafPolicy)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.set_policy", Description: "Create or update the load balancer WAF policy and reload the appliance."}, setWafPolicy)
	Register(s, deps, Spec{
		Name:        "user.load_balancer.waf.disable_policy",
		Description: "Disable the WAF on a load balancer. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, disableWaf)
}
