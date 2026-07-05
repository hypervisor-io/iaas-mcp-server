package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func sshKeyMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["public_key"] == "" || body["public_key"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"public_key": []string{"The public key field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "ssh_key": map[string]any{"id": "key-1", "name": body["name"]},
		})
	})
	mux.HandleFunc("GET /ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "key-1", "name": "laptop"}},
		})
	})
	mux.HandleFunc("GET /ssh-key/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "SSH key not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "ssh_key": map[string]any{"id": id, "name": "laptop"}})
	})
	mux.HandleFunc("PATCH /ssh-key/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "ssh_key": map[string]any{"id": id, "name": "renamed"}})
	})
	mux.HandleFunc("DELETE /ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestSSHKey_CRUD(t *testing.T) {
	cs := connectSession(t, sshKeyMock())

	res := callTool(t, cs, "user.ssh_key.create", map[string]any{"name": "laptop", "public_key": "ssh-ed25519 AAAA"})
	var created tools.SSHKeyResult
	unmarshalResult(t, res, &created)
	if created.SSHKey["id"] != "key-1" {
		t.Fatalf("create id = %v, want key-1", created.SSHKey["id"])
	}

	res = callTool(t, cs, "user.ssh_key.list", map[string]any{})
	var list tools.SSHKeyListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.ssh_key.get", map[string]any{"id": "key-1"})
	var got tools.SSHKeyResult
	unmarshalResult(t, res, &got)
	if got.SSHKey["id"] != "key-1" {
		t.Errorf("get id = %v, want key-1", got.SSHKey["id"])
	}

	res = callTool(t, cs, "user.ssh_key.update", map[string]any{"id": "key-1", "name": "renamed"})
	var upd tools.SSHKeyResult
	unmarshalResult(t, res, &upd)
	if upd.SSHKey["name"] != "renamed" {
		t.Errorf("update name = %v, want renamed", upd.SSHKey["name"])
	}
}

func TestSSHKey_DeleteConfirmAndErrors(t *testing.T) {
	cs := connectSession(t, sshKeyMock())

	res := callTool(t, cs, "user.ssh_key.delete", map[string]any{"id": "key-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.ssh_key.delete", map[string]any{"id": "key-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}

	res = callTool(t, cs, "user.ssh_key.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.ssh_key.create", map[string]any{"name": "x", "public_key": ""})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Errorf("create no key: want validation failed, got %q", resultText(t, res))
	}
}
