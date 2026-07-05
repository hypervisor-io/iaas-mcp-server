package tools_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func staticIPMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /static-ips/allocate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"static_ip": map[string]any{
				"id": "sip-1", "status": "allocated",
				"ip": map[string]any{"ip": "203.0.113.5"},
			},
		})
	})
	mux.HandleFunc("GET /static-ips", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{
				map[string]any{"id": "sip-1", "status": "allocated"},
				map[string]any{"id": "sip-2", "status": "attached"},
			},
		})
	})
	mux.HandleFunc("DELETE /static-ip/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "released"})
	})
	return mux
}

func TestStaticIP_AllocateListGet(t *testing.T) {
	cs := connectSession(t, staticIPMock())

	res := callTool(t, cs, "user.static_ip.allocate", map[string]any{
		"ip_id": "pool-1", "hypervisor_group_id": "hg-1",
	})
	var alloc tools.StaticIPResult
	unmarshalResult(t, res, &alloc)
	if alloc.StaticIP["id"] != "sip-1" {
		t.Fatalf("allocate id = %v, want sip-1", alloc.StaticIP["id"])
	}

	res = callTool(t, cs, "user.static_ip.list", map[string]any{})
	var list tools.StaticIPListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Errorf("list count = %d, want 2", list.Count)
	}

	// get uses list-and-filter (no SHOW route).
	res = callTool(t, cs, "user.static_ip.get", map[string]any{"id": "sip-2"})
	var got tools.StaticIPResult
	unmarshalResult(t, res, &got)
	if got.StaticIP["id"] != "sip-2" {
		t.Errorf("get id = %v, want sip-2", got.StaticIP["id"])
	}

	// get of an unknown id maps to not found (client synthesizes a 404).
	res = callTool(t, cs, "user.static_ip.get", map[string]any{"id": "nope"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}
}

func TestStaticIP_DeallocateConfirmGate(t *testing.T) {
	cs := connectSession(t, staticIPMock())
	res := callTool(t, cs, "user.static_ip.deallocate", map[string]any{"id": "sip-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("deallocate without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.static_ip.deallocate", map[string]any{"id": "sip-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed deallocate did not succeed")
	}
}
