package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func alertRuleMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /alert-rules", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["metric"] == "" || body["metric"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"metric": []string{"The metric field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "alert_rule": map[string]any{"id": "ar-1", "name": body["name"], "metric": body["metric"]},
		})
	})
	mux.HandleFunc("GET /alert-rules", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"alert_rules": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{map[string]any{"id": "ar-1", "name": "cpu-high"}},
			},
		})
	})
	mux.HandleFunc("GET /alert-rule/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Alert rule not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "alert_rule": map[string]any{"id": id, "name": "cpu-high"}})
	})
	mux.HandleFunc("DELETE /alert-rule/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestAlertRule_CreateListGet(t *testing.T) {
	cs := connectSession(t, alertRuleMock())

	res := callTool(t, cs, "user.alert_rule.create", map[string]any{
		"name": "cpu-high", "resource_type": "instance", "metric": "cpu", "operator": "gt", "threshold": 90.0,
		"channel_ids": []any{"ch-1"},
	})
	var created tools.AlertRuleResult
	unmarshalResult(t, res, &created)
	if created.AlertRule["id"] != "ar-1" {
		t.Fatalf("create id = %v, want ar-1", created.AlertRule["id"])
	}

	res = callTool(t, cs, "user.alert_rule.list", map[string]any{})
	var list tools.AlertRuleListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.alert_rule.get", map[string]any{"id": "ar-1"})
	var got tools.AlertRuleResult
	unmarshalResult(t, res, &got)
	if got.AlertRule["id"] != "ar-1" {
		t.Errorf("get id = %v, want ar-1", got.AlertRule["id"])
	}
}

func TestAlertRule_DeleteConfirmAndErrors(t *testing.T) {
	cs := connectSession(t, alertRuleMock())
	res := callTool(t, cs, "user.alert_rule.delete", map[string]any{"id": "ar-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.alert_rule.delete", map[string]any{"id": "ar-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
	res = callTool(t, cs, "user.alert_rule.create", map[string]any{
		"name": "x", "resource_type": "instance", "metric": "", "operator": "gt", "threshold": 1.0,
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Errorf("create no metric: want validation failed, got %q", resultText(t, res))
	}
}
