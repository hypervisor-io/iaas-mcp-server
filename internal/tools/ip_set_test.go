package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func ipSetMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ip-sets", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["ip_version"] == "" || body["ip_version"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"ip_version": []string{"The ip version field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "ip_set": map[string]any{"id": "set-1", "name": body["name"], "ip_version": body["ip_version"]},
		})
	})
	mux.HandleFunc("GET /ip-sets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "set-1", "name": "office", "entries_count": 3}},
		})
	})
	mux.HandleFunc("GET /ip-set/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "IP set not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"ip_set": map[string]any{
				"id": id, "name": "office",
				"entries": []any{map[string]any{"id": "e-1", "cidr": "10.0.0.0/24"}},
			},
		})
	})
	mux.HandleFunc("DELETE /ip-set/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("POST /ip-set/{id}/entries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "entry": map[string]any{"id": "e-9", "cidr": "192.168.1.0/24"},
		})
	})
	mux.HandleFunc("POST /ip-set/{id}/bulk-add", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"created": []any{map[string]any{"id": "e-10", "cidr": "1.1.1.0/24"}},
			"errors":  []any{},
		})
	})
	mux.HandleFunc("DELETE /ip-set/{id}/entry/{entryId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "removed"})
	})
	return mux
}

func TestIPSet_CRUDAndEntries(t *testing.T) {
	cs := connectSession(t, ipSetMock())

	res := callTool(t, cs, "user.ip_set.create", map[string]any{"name": "office", "ip_version": "ipv4"})
	var created tools.IPSetResult
	unmarshalResult(t, res, &created)
	if created.IPSet["id"] != "set-1" {
		t.Fatalf("create id = %v, want set-1", created.IPSet["id"])
	}

	res = callTool(t, cs, "user.ip_set.list", map[string]any{})
	var list tools.IPSetListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.ip_set.get", map[string]any{"id": "set-1"})
	var got tools.IPSetResult
	unmarshalResult(t, res, &got)
	if got.IPSet["id"] != "set-1" {
		t.Errorf("get id = %v, want set-1", got.IPSet["id"])
	}

	res = callTool(t, cs, "user.ip_set.add_entry", map[string]any{"ip_set_id": "set-1", "cidr": "192.168.1.0/24"})
	var entry tools.EntryResult
	unmarshalResult(t, res, &entry)
	if entry.Entry["id"] != "e-9" {
		t.Errorf("add_entry id = %v, want e-9", entry.Entry["id"])
	}

	res = callTool(t, cs, "user.ip_set.bulk_add", map[string]any{"ip_set_id": "set-1", "cidrs": []any{"1.1.1.0/24"}})
	var bulk tools.BulkAddResult
	unmarshalResult(t, res, &bulk)
	if len(bulk.Created) != 1 {
		t.Errorf("bulk_add created = %v, want 1", bulk.Created)
	}
}

func TestIPSet_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, ipSetMock())
	res := callTool(t, cs, "user.ip_set.delete", map[string]any{"id": "set-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.ip_set.delete", map[string]any{"id": "set-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}

func TestIPSet_ErrorMapping(t *testing.T) {
	cs := connectSession(t, ipSetMock())
	res := callTool(t, cs, "user.ip_set.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.ip_set.create", map[string]any{"name": "x", "ip_version": ""})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Errorf("create no ip_version: want validation failed, got %q", resultText(t, res))
	}
}
