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
	toolRegistrars = append(toolRegistrars, registerWafTools, registerAdminWafTools)
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

// Load balancer WAF custom-rule tools (S6 Task 2). A rule is authored either
// directly (rule_id + raw_seclang) or via a guided-builder convenience shape
// (target/operator/match_value/action/priority, rendered into `seclang`
// server-side when raw_seclang is omitted) - see Master's
// StoreWafRuleRequest docblock. The rule object returned by the API only
// ever carries the real `lb_waf_rules` columns: id, rule_id, name, seclang,
// enabled, sort_order.

// RuleResult (security_group.go) and ItemsResult/itemsResult
// (parity_types.go) are the shared generic result types + helper - reused
// here rather than redeclared.

type ListWafRulesInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
}

type CreateWafRuleInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	RuleID         *int   `json:"rule_id,omitempty" jsonschema:"SecLang rule id, must be 190000-199999 (auto-assigned if omitted, or extracted from an id: token in raw_seclang)"`
	Name           string `json:"name" jsonschema:"rule name"`
	Enabled        *bool  `json:"enabled,omitempty" jsonschema:"whether the rule is active (default true)"`
	RawSeclang     string `json:"raw_seclang,omitempty" jsonschema:"raw SecLang directive (advanced); any embedded id: must be 190000-199999. Takes precedence over the guided fields below"`
	Target         string `json:"target,omitempty" jsonschema:"guided-builder SecLang variable (e.g. ARGS)"`
	Operator       string `json:"operator,omitempty" jsonschema:"guided-builder SecLang operator token (e.g. @rx)"`
	MatchValue     string `json:"match_value,omitempty" jsonschema:"guided-builder match value"`
	Action         string `json:"action,omitempty" jsonschema:"deny, log, pass or drop (default deny)"`
	Priority       *int   `json:"priority,omitempty" jsonschema:"evaluation priority; maps to the real sort_order column"`
}

type UpdateWafRuleInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	RuleID         string `json:"rule_id" jsonschema:"UUID of the rule to update"`
	Name           string `json:"name,omitempty" jsonschema:"rule name"`
	Enabled        *bool  `json:"enabled,omitempty" jsonschema:"whether the rule is active"`
	RawSeclang     string `json:"raw_seclang,omitempty" jsonschema:"raw SecLang directive (advanced)"`
	Target         string `json:"target,omitempty" jsonschema:"guided-builder SecLang variable"`
	Operator       string `json:"operator,omitempty" jsonschema:"guided-builder SecLang operator token"`
	MatchValue     string `json:"match_value,omitempty" jsonschema:"guided-builder match value"`
	Action         string `json:"action,omitempty" jsonschema:"deny, log, pass or drop"`
	Priority       *int   `json:"priority,omitempty" jsonschema:"evaluation priority; maps to the real sort_order column"`
}

type DeleteWafRuleInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	RuleID         string `json:"rule_id" jsonschema:"UUID of the rule to delete"`
	Confirmation
}

func listWafRules(ctx context.Context, cl *client.Client, in ListWafRulesInput) (ItemsResult, error) {
	return itemsResult(cl.ListLBWafRules(ctx, in.LoadBalancerID))
}

func createWafRule(ctx context.Context, cl *client.Client, in CreateWafRuleInput) (RuleResult, error) {
	body := map[string]any{"name": in.Name}
	if in.RuleID != nil {
		body["rule_id"] = *in.RuleID
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.RawSeclang != "" {
		body["raw_seclang"] = in.RawSeclang
	}
	if in.Target != "" {
		body["target"] = in.Target
	}
	if in.Operator != "" {
		body["operator"] = in.Operator
	}
	if in.MatchValue != "" {
		body["match_value"] = in.MatchValue
	}
	if in.Action != "" {
		body["action"] = in.Action
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	obj, err := cl.CreateLBWafRule(ctx, in.LoadBalancerID, body)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: obj}, nil
}

func updateWafRule(ctx context.Context, cl *client.Client, in UpdateWafRuleInput) (RuleResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.RawSeclang != "" {
		body["raw_seclang"] = in.RawSeclang
	}
	if in.Target != "" {
		body["target"] = in.Target
	}
	if in.Operator != "" {
		body["operator"] = in.Operator
	}
	if in.MatchValue != "" {
		body["match_value"] = in.MatchValue
	}
	if in.Action != "" {
		body["action"] = in.Action
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	obj, err := cl.UpdateLBWafRule(ctx, in.LoadBalancerID, in.RuleID, body)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: obj}, nil
}

func deleteWafRule(ctx context.Context, cl *client.Client, in DeleteWafRuleInput) (DeleteResult, error) {
	if err := cl.DeleteLBWafRule(ctx, in.LoadBalancerID, in.RuleID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.RuleID, Deleted: true}, nil
}

func registerWafTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.load_balancer.waf.get_policy", Description: "Get the WAF policy of a load balancer (null if none)."}, getWafPolicy)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.set_policy", Description: "Create or update the load balancer WAF policy and reload the appliance."}, setWafPolicy)
	Register(s, deps, Spec{
		Name:        "user.load_balancer.waf.disable_policy",
		Description: "Disable the WAF on a load balancer. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, disableWaf)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.list_rules", Description: "List the custom WAF rules of a load balancer."}, listWafRules)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.create_rule", Description: "Create a custom WAF rule (rule_id must be 190000-199999)."}, createWafRule)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.update_rule", Description: "Update a custom WAF rule."}, updateWafRule)
	Register(s, deps, Spec{
		Name:        "user.load_balancer.waf.delete_rule",
		Description: "Delete a custom WAF rule. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteWafRule)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.query_events", Description: "Query the WAF event log of a load balancer (owner-scoped, paginated)."}, queryWafEvents)
	Register(s, deps, Spec{Name: "user.load_balancer.waf.export_events", Description: "Export the filtered WAF events to an archive and return a download URL."}, exportWafEvents)
}

// Load balancer WAF event log tools (S6 Task 3, manifest entries 8-9). Reads
// only - delegates to Master's owner-scoped LbWafLogService via the client's
// QueryLBWafEvents/ExportLBWafEvents. Both endpoints are OpenTofu-excluded
// (log/telemetry read + imperative export action, not declarative IaC state)
// but MCP-covered, per api-manifest.json.

type QueryWafEventsInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	From           string `json:"from,omitempty" jsonschema:"ISO-8601 lower time bound"`
	To             string `json:"to,omitempty" jsonschema:"ISO-8601 upper time bound"`
	ClientIP       string `json:"client_ip,omitempty" jsonschema:"filter by client IP"`
	RuleID         string `json:"rule_id,omitempty" jsonschema:"filter by matched rule id"`
	Action         string `json:"action,omitempty" jsonschema:"logged or blocked"`
	Page           string `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage        string `json:"per_page,omitempty" jsonschema:"page size (max 200)"`
}

type ExportWafEventsInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	From           string `json:"from,omitempty" jsonschema:"ISO-8601 lower time bound"`
	To             string `json:"to,omitempty" jsonschema:"ISO-8601 upper time bound"`
	ClientIP       string `json:"client_ip,omitempty" jsonschema:"filter by client IP"`
	RuleID         *int   `json:"rule_id,omitempty" jsonschema:"filter by matched rule id"`
	Action         string `json:"action,omitempty" jsonschema:"logged or blocked"`
}

type WafExportResult struct {
	DownloadURL string `json:"download_url"`
}

func queryWafEvents(ctx context.Context, cl *client.Client, in QueryWafEventsInput) (ItemsResult, error) {
	filters := map[string]string{
		"from": in.From, "to": in.To, "client_ip": in.ClientIP,
		"rule_id": in.RuleID, "action": in.Action, "page": in.Page, "per_page": in.PerPage,
	}
	return itemsResult(cl.QueryLBWafEvents(ctx, in.LoadBalancerID, filters))
}

func exportWafEvents(ctx context.Context, cl *client.Client, in ExportWafEventsInput) (WafExportResult, error) {
	body := map[string]any{}
	if in.From != "" {
		body["from"] = in.From
	}
	if in.To != "" {
		body["to"] = in.To
	}
	if in.ClientIP != "" {
		body["client_ip"] = in.ClientIP
	}
	if in.RuleID != nil {
		body["rule_id"] = *in.RuleID
	}
	if in.Action != "" {
		body["action"] = in.Action
	}
	u, err := cl.ExportLBWafEvents(ctx, in.LoadBalancerID, body)
	if err != nil {
		return WafExportResult{}, err
	}
	return WafExportResult{DownloadURL: u}, nil
}

// Admin WAF tools (S6 Task 4, spec 17 decision D3: a CURATED safe allowlist
// over the admin api/v1 surface - manifest entries 10-18). Reuses every
// input/output type already declared above for the user.* tools (the wire
// shapes are identical - the admin controller is a structural mirror of
// UserApi\LbWafController) and the client's Admin* methods (client/lb_waf.go),
// which target the admin api/v1 base path instead of the user path.
//
// Per D3, destructive admin ops are NOT exposed: DELETE policy
// (admin.load_balancer.waf.disable_policy) and DELETE rule
// (admin.load_balancer.waf.delete_rule) have no tool here - manifest entries
// 12 and 16 are mcp.status="excluded" with reason "destructive admin op
// excluded from the D3 safe allowlist". AdminDeleteLBWafPolicy/
// AdminDeleteLBWafRule client methods were deliberately not added either
// (see client/lb_waf.go's admin-section docblock) - there is nothing here to
// call them.

func adminGetWafPolicy(ctx context.Context, cl *client.Client, in GetWafPolicyInput) (WafPolicyResult, error) {
	obj, err := cl.AdminGetLBWafPolicy(ctx, in.LoadBalancerID)
	if err != nil {
		if client.IsNotFound(err) {
			return WafPolicyResult{Policy: nil}, nil
		}
		return WafPolicyResult{}, err
	}
	return WafPolicyResult{Policy: obj}, nil
}

func adminSetWafPolicy(ctx context.Context, cl *client.Client, in SetWafPolicyInput) (WafPolicyResult, error) {
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
	obj, err := cl.AdminPutLBWafPolicy(ctx, in.LoadBalancerID, body)
	if err != nil {
		return WafPolicyResult{}, err
	}
	return WafPolicyResult{Policy: obj}, nil
}

func adminListWafRules(ctx context.Context, cl *client.Client, in ListWafRulesInput) (ItemsResult, error) {
	return itemsResult(cl.AdminListLBWafRules(ctx, in.LoadBalancerID))
}

func adminCreateWafRule(ctx context.Context, cl *client.Client, in CreateWafRuleInput) (RuleResult, error) {
	body := map[string]any{"name": in.Name}
	if in.RuleID != nil {
		body["rule_id"] = *in.RuleID
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.RawSeclang != "" {
		body["raw_seclang"] = in.RawSeclang
	}
	if in.Target != "" {
		body["target"] = in.Target
	}
	if in.Operator != "" {
		body["operator"] = in.Operator
	}
	if in.MatchValue != "" {
		body["match_value"] = in.MatchValue
	}
	if in.Action != "" {
		body["action"] = in.Action
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	obj, err := cl.AdminCreateLBWafRule(ctx, in.LoadBalancerID, body)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: obj}, nil
}

func adminUpdateWafRule(ctx context.Context, cl *client.Client, in UpdateWafRuleInput) (RuleResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.RawSeclang != "" {
		body["raw_seclang"] = in.RawSeclang
	}
	if in.Target != "" {
		body["target"] = in.Target
	}
	if in.Operator != "" {
		body["operator"] = in.Operator
	}
	if in.MatchValue != "" {
		body["match_value"] = in.MatchValue
	}
	if in.Action != "" {
		body["action"] = in.Action
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	obj, err := cl.AdminUpdateLBWafRule(ctx, in.LoadBalancerID, in.RuleID, body)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: obj}, nil
}

func adminQueryWafEvents(ctx context.Context, cl *client.Client, in QueryWafEventsInput) (ItemsResult, error) {
	filters := map[string]string{
		"from": in.From, "to": in.To, "client_ip": in.ClientIP,
		"rule_id": in.RuleID, "action": in.Action, "page": in.Page, "per_page": in.PerPage,
	}
	return itemsResult(cl.AdminQueryLBWafEvents(ctx, in.LoadBalancerID, filters))
}

func adminExportWafEvents(ctx context.Context, cl *client.Client, in ExportWafEventsInput) (WafExportResult, error) {
	body := map[string]any{}
	if in.From != "" {
		body["from"] = in.From
	}
	if in.To != "" {
		body["to"] = in.To
	}
	if in.ClientIP != "" {
		body["client_ip"] = in.ClientIP
	}
	if in.RuleID != nil {
		body["rule_id"] = *in.RuleID
	}
	if in.Action != "" {
		body["action"] = in.Action
	}
	u, err := cl.AdminExportLBWafEvents(ctx, in.LoadBalancerID, body)
	if err != nil {
		return WafExportResult{}, err
	}
	return WafExportResult{DownloadURL: u}, nil
}

func registerAdminWafTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.get_policy", Description: "Get the WAF policy of any load balancer, regardless of owner (admin).", Admin: true}, adminGetWafPolicy)
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.set_policy", Description: "Create or update the WAF policy of any load balancer and reload the appliance (admin).", Admin: true}, adminSetWafPolicy)
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.list_rules", Description: "List the custom WAF rules of any load balancer (admin).", Admin: true}, adminListWafRules)
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.create_rule", Description: "Create a custom WAF rule on any load balancer (rule_id must be 190000-199999) (admin).", Admin: true}, adminCreateWafRule)
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.update_rule", Description: "Update a custom WAF rule on any load balancer (admin).", Admin: true}, adminUpdateWafRule)
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.query_events", Description: "Query the WAF event log of any load balancer, regardless of owner (admin, paginated).", Admin: true}, adminQueryWafEvents)
	Register(s, deps, Spec{Name: "admin.load_balancer.waf.export_events", Description: "Export the filtered WAF events of any load balancer to an archive and return a download URL (admin).", Admin: true}, adminExportWafEvents)
}
