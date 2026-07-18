package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// adminWafMock serves the admin api/v1 WAF mirror under /v1/... (S6 Task 4,
// spec 17 manifest entries 10-18). Same response envelopes as the user WAF
// mock (wafMock() in waf_test.go) - the admin controller is a structural
// mirror of the user_api one - just under the /v1 prefix the client's Admin*
// methods target.
func adminWafMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/load-balancer/{id}/waf/policy", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "no-policy" {
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "policy": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"policy": map[string]any{
				"id": "pol-1", "enabled": true, "mode": "detect", "fail_mode": "open",
				"crs_enabled": true, "sensitivity": 1, "response_inspection": false,
				"full_audit": false, "exclusions": []any{},
			},
		})
	})
	mux.HandleFunc("PUT /v1/load-balancer/{id}/waf/policy", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["mode"] == "nuke" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"mode": []string{"The selected mode is invalid."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"policy": map[string]any{
				"id": "pol-1", "enabled": body["enabled"], "mode": body["mode"],
				"sensitivity": body["sensitivity"], "exclusions": body["exclusions"],
			},
		})
	})
	mux.HandleFunc("GET /v1/load-balancer/{id}/waf/rules", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"rules": []any{
				map[string]any{
					"id": "rule-1", "rule_id": 190010, "name": "block-sqli",
					"seclang": "SecRule ARGS \"@rx union select\" \"id:190010,phase:2,deny\"",
					"enabled": true, "sort_order": 10,
				},
			},
		})
	})
	mux.HandleFunc("POST /v1/load-balancer/{id}/waf/rules", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if ruleID, ok := body["rule_id"].(float64); ok && (ruleID < 190000 || ruleID > 199999) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"rule_id": []string{"The rule id must be between 190000 and 199999."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"rule": map[string]any{
				"id": "rule-new", "rule_id": body["rule_id"], "name": body["name"],
				"seclang": "SecRule ARGS ...", "enabled": true, "sort_order": 10,
			},
		})
	})
	mux.HandleFunc("PATCH /v1/load-balancer/{id}/waf/rule/{ruleId}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"rule": map[string]any{
				"id": r.PathValue("ruleId"), "rule_id": 190010, "name": body["name"],
				"seclang": "SecRule ARGS ...", "enabled": true, "sort_order": 10,
			},
		})
	})
	mux.HandleFunc("GET /v1/load-balancer/{id}/waf/events", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"events": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{
					map[string]any{"id": "ev-1", "lb_id": r.PathValue("id"), "client_ip": "203.0.113.9", "action": "blocked"},
				},
			},
		})
	})
	mux.HandleFunc("POST /v1/load-balancer/{id}/waf/events/export", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "download_url": "https://example.test/export.ndjson"})
	})
	return mux
}

func TestAdminWaf_GetPolicy(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.get_policy", map[string]any{"load_balancer_id": "lb-1"})
	var got tools.WafPolicyResult
	unmarshalResult(t, res, &got)
	if got.Policy["mode"] != "detect" {
		t.Errorf("policy.mode = %v; want detect", got.Policy["mode"])
	}
}

func TestAdminWaf_GetPolicy_NullWhenNoneConfigured(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.get_policy", map[string]any{"load_balancer_id": "no-policy"})
	if res.IsError {
		t.Fatalf("get_policy on an LB with no policy must not error; got %q", resultText(t, res))
	}
	var got tools.WafPolicyResult
	unmarshalResult(t, res, &got)
	if got.Policy != nil {
		t.Errorf("policy = %v; want nil", got.Policy)
	}
}

func TestAdminWaf_SetPolicy(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.set_policy", map[string]any{
		"load_balancer_id": "lb-1", "enabled": true, "mode": "block", "sensitivity": 2,
		"exclusions": []any{942100},
	})
	var got tools.WafPolicyResult
	unmarshalResult(t, res, &got)
	if got.Policy["mode"] != "block" {
		t.Errorf("policy.mode = %v; want block", got.Policy["mode"])
	}
}

func TestAdminWaf_SetPolicy_ValidationError(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.set_policy", map[string]any{
		"load_balancer_id": "lb-1", "enabled": true, "mode": "nuke",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation") {
		t.Fatalf("expected a validation error; got %q", resultText(t, res))
	}
}

func TestAdminWaf_ListRules(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.list_rules", map[string]any{"load_balancer_id": "lb-1"})
	var got tools.ItemsResult
	unmarshalResult(t, res, &got)
	if got.Count != 1 || got.Items[0]["rule_id"] != float64(190010) {
		t.Fatalf("items = %v; want one rule with rule_id=190010", got.Items)
	}
}

func TestAdminWaf_CreateRule(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.create_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": 190010, "name": "block-sqli",
		"target": "ARGS", "operator": "@rx", "match_value": "union select",
		"action": "deny", "priority": 100,
	})
	var got tools.RuleResult
	unmarshalResult(t, res, &got)
	if got.Rule["rule_id"] != float64(190010) {
		t.Errorf("rule.rule_id = %v; want 190010", got.Rule["rule_id"])
	}
}

func TestAdminWaf_CreateRule_RejectsIdOutOfRange(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.create_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": 200000, "name": "x",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation") {
		t.Fatalf("expected a validation error; got %q", resultText(t, res))
	}
}

func TestAdminWaf_UpdateRule(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.update_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": "rule-1", "name": "renamed",
	})
	var got tools.RuleResult
	unmarshalResult(t, res, &got)
	if got.Rule["name"] != "renamed" {
		t.Errorf("rule.name = %v; want renamed", got.Rule["name"])
	}
}

func TestAdminWaf_QueryEvents(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.query_events", map[string]any{"load_balancer_id": "lb-1"})
	var got tools.ItemsResult
	unmarshalResult(t, res, &got)
	if got.Count != 1 || got.Items[0]["client_ip"] != "203.0.113.9" {
		t.Fatalf("items = %v; want one event with client_ip=203.0.113.9", got.Items)
	}
}

func TestAdminWaf_ExportEvents(t *testing.T) {
	cs := connectSession(t, adminWafMock())

	res := callTool(t, cs, "admin.load_balancer.waf.export_events", map[string]any{
		"load_balancer_id": "lb-1", "from": "2026-01-01",
	})
	var got tools.WafExportResult
	unmarshalResult(t, res, &got)
	if got.DownloadURL == "" {
		t.Errorf("download_url is empty")
	}
}

// TestAdminWaf_NoDestructiveTools proves the D3 curation held: no
// admin.load_balancer.waf.disable_policy / .delete_rule tool is registered
// (both are excluded from the safe allowlist - see manifest entries 12/16).
func TestAdminWaf_NoDestructiveTools(t *testing.T) {
	for _, name := range []string{
		"admin.load_balancer.waf.disable_policy",
		"admin.load_balancer.waf.delete_rule",
	} {
		found := false
		for _, n := range tools.RegisteredToolNames() {
			if n == name {
				found = true
				break
			}
		}
		if found {
			t.Errorf("destructive admin WAF tool %q must not be registered (D3 safe allowlist)", name)
		}
	}
}
