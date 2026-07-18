package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func wafMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /load-balancer/{id}/waf/policy", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("PUT /load-balancer/{id}/waf/policy", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("DELETE /load-balancer/{id}/waf/policy", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "WAF disabled"})
	})
	mux.HandleFunc("GET /load-balancer/{id}/waf/rules", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST /load-balancer/{id}/waf/rules", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("PATCH /load-balancer/{id}/waf/rule/{ruleId}", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("DELETE /load-balancer/{id}/waf/rule/{ruleId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Rule deleted"})
	})
	return mux
}

func TestWaf_GetPolicy(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.get_policy", map[string]any{"load_balancer_id": "lb-1"})
	var got tools.WafPolicyResult
	unmarshalResult(t, res, &got)
	if got.Policy["mode"] != "detect" {
		t.Errorf("policy.mode = %v; want detect", got.Policy["mode"])
	}
	if _, present := got.Policy["paranoia_level"]; present {
		t.Errorf("policy must not carry the raw DB column paranoia_level: %v", got.Policy)
	}
}

func TestWaf_GetPolicy_NullWhenNoneConfigured(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.get_policy", map[string]any{"load_balancer_id": "no-policy"})
	if res.IsError {
		t.Fatalf("get_policy on an LB with no policy must not error; got %q", resultText(t, res))
	}
	var got tools.WafPolicyResult
	unmarshalResult(t, res, &got)
	if got.Policy != nil {
		t.Errorf("policy = %v; want nil", got.Policy)
	}
}

func TestWaf_SetPolicy(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.set_policy", map[string]any{
		"load_balancer_id": "lb-1", "enabled": true, "mode": "block", "sensitivity": 2,
		"exclusions": []any{942100},
	})
	var got tools.WafPolicyResult
	unmarshalResult(t, res, &got)
	if got.Policy["mode"] != "block" {
		t.Errorf("policy.mode = %v; want block", got.Policy["mode"])
	}
}

func TestWaf_SetPolicy_ValidationError(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.set_policy", map[string]any{
		"load_balancer_id": "lb-1", "enabled": true, "mode": "nuke",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation") {
		t.Fatalf("expected a validation error; got %q", resultText(t, res))
	}
}

func TestWaf_DisablePolicy_RequiresConfirm(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.disable_policy", map[string]any{"load_balancer_id": "lb-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("disable without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.load_balancer.waf.disable_policy", map[string]any{"load_balancer_id": "lb-1", "confirm": true})
	var got tools.OKResult
	unmarshalResult(t, res, &got)
	if !got.OK {
		t.Errorf("confirmed disable did not succeed")
	}
}

func TestWaf_ListRules(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.list_rules", map[string]any{"load_balancer_id": "lb-1"})
	var got tools.ItemsResult
	unmarshalResult(t, res, &got)
	if got.Count != 1 || got.Items[0]["rule_id"] != float64(190010) {
		t.Fatalf("items = %v; want one rule with rule_id=190010", got.Items)
	}
}

func TestWaf_CreateRule(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.create_rule", map[string]any{
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

func TestWaf_CreateRule_RejectsIdOutOfRange(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.create_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": 200000, "name": "x",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation") {
		t.Fatalf("expected a validation error; got %q", resultText(t, res))
	}
}

func TestWaf_UpdateRule(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.update_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": "rule-1", "name": "renamed",
	})
	var got tools.RuleResult
	unmarshalResult(t, res, &got)
	if got.Rule["name"] != "renamed" {
		t.Errorf("rule.name = %v; want renamed", got.Rule["name"])
	}
}

func TestWaf_DeleteRule_RequiresConfirm(t *testing.T) {
	cs := connectSession(t, wafMock())

	res := callTool(t, cs, "user.load_balancer.waf.delete_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": "rule-1",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.load_balancer.waf.delete_rule", map[string]any{
		"load_balancer_id": "lb-1", "rule_id": "rule-1", "confirm": true,
	})
	var got tools.DeleteResult
	unmarshalResult(t, res, &got)
	if !got.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
