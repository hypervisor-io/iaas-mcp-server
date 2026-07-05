package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// adminMock serves the admin API under /v1/... (the client prepends /v1 to
// admin paths, on top of the mock base URL). Lists return raw paginators; shows
// return bare models. The /v1/hypervisor/forbidden path returns 403 to exercise
// the admin-scope hint mapping.
func adminMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/instances", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{
				map[string]any{"id": "inst-1", "hostname": "web"},
				map[string]any{"id": "inst-2", "hostname": "db"},
			},
		})
	})
	mux.HandleFunc("GET /v1/instance/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Instance not found"})
			return
		}
		// Bare model (no envelope).
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "hostname": "web", "maintenance": 0})
	})
	mux.HandleFunc("GET /v1/hypervisors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "hv-1", "name": "HV-01", "maintenance": 0}},
		})
	})
	mux.HandleFunc("GET /v1/hypervisor/forbidden", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "Unauthorized!"})
	})
	mux.HandleFunc("GET /v1/hypervisor/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "name": "HV-01"})
	})
	mux.HandleFunc("PATCH /v1/hypervisor/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Only the maintenance field must be sent by the safe-mutation client method.
		if _, ok := body["maintenance"]; !ok {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "missing maintenance"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": r.PathValue("id"), "maintenance": body["maintenance"]})
	})
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "task-1", "status": "completed"}},
		})
	})
	mux.HandleFunc("GET /v1/users", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "user-1", "email": "a@b.c"}},
		})
	})
	mux.HandleFunc("GET /v1/subnets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "sub-1", "name": "public"}},
		})
	})
	mux.HandleFunc("GET /v1/instance/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{"id": "plan-1", "name": "2vCPU"}})
	})
	mux.HandleFunc("POST /v1/dns/reverse/request/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "action": body["action"]})
	})
	return mux
}

func TestAdmin_Reads(t *testing.T) {
	cs := connectSession(t, adminMock())

	res := callTool(t, cs, "admin.instance.list", map[string]any{})
	var list tools.AdminListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("admin.instance.list count = %d, want 2", list.Count)
	}

	res = callTool(t, cs, "admin.instance.get", map[string]any{"id": "inst-9"})
	var item tools.AdminItemResult
	unmarshalResult(t, res, &item)
	if item.Item["id"] != "inst-9" {
		t.Errorf("admin.instance.get id = %v, want inst-9", item.Item["id"])
	}

	res = callTool(t, cs, "admin.hypervisor.list", map[string]any{})
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Items[0]["id"] != "hv-1" {
		t.Errorf("admin.hypervisor.list = %v", list.Items)
	}

	res = callTool(t, cs, "admin.instance_plan.list", map[string]any{})
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("admin.instance_plan.list count = %d, want 1", list.Count)
	}

	// Bare-array vs paginator both handled; user + subnet + task reads work.
	for _, name := range []string{"admin.user.list", "admin.subnet.list", "admin.task.list"} {
		res = callTool(t, cs, name, map[string]any{})
		unmarshalResult(t, res, &list)
		if list.Count != 1 {
			t.Errorf("%s count = %d, want 1", name, list.Count)
		}
	}
}

func TestAdmin_NotFoundMapping(t *testing.T) {
	cs := connectSession(t, adminMock())
	res := callTool(t, cs, "admin.instance.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("admin.instance.get missing: want not found, got %q", resultText(t, res))
	}
}

func TestAdmin_ForbiddenMapsToAdminScopeHint(t *testing.T) {
	cs := connectSession(t, adminMock())
	// hv id "forbidden" returns 403; the admin hint must surface.
	res := callTool(t, cs, "admin.hypervisor.get", map[string]any{"id": "forbidden"})
	if !res.IsError {
		t.Fatalf("expected an error result for 403")
	}
	msg := resultText(t, res)
	if !strings.Contains(msg, "admin-scoped") || !strings.Contains(msg, "admin authorization failed") {
		t.Errorf("403 message = %q, want an admin-scope/IP-lock hint", msg)
	}
}

func TestAdmin_SetMaintenanceConfirmGate(t *testing.T) {
	cs := connectSession(t, adminMock())

	res := callTool(t, cs, "admin.hypervisor.set_maintenance", map[string]any{"id": "hv-1", "enabled": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("set_maintenance without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "admin.hypervisor.set_maintenance", map[string]any{"id": "hv-1", "enabled": true, "confirm": true})
	var item tools.AdminItemResult
	unmarshalResult(t, res, &item)
	if item.Item["maintenance"] != true {
		t.Errorf("set_maintenance result maintenance = %v, want true", item.Item["maintenance"])
	}
}

func TestAdmin_RdnsProcessConfirmAndValidation(t *testing.T) {
	cs := connectSession(t, adminMock())

	// Confirm gate.
	res := callTool(t, cs, "admin.rdns_request.process", map[string]any{"id": "req-1", "action": "approve"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("process without confirm should refuse; got %q", resultText(t, res))
	}
	// Invalid action rejected by the handler.
	res = callTool(t, cs, "admin.rdns_request.process", map[string]any{"id": "req-1", "action": "delete", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "approve") {
		t.Errorf("invalid action: want a validation error mentioning approve, got %q", resultText(t, res))
	}
	// Valid approve.
	res = callTool(t, cs, "admin.rdns_request.process", map[string]any{"id": "req-1", "action": "approve", "confirm": true})
	var item tools.AdminItemResult
	unmarshalResult(t, res, &item)
	if item.Item["action"] != "approve" {
		t.Errorf("process approve result = %v", item.Item)
	}
}
