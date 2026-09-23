package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func microvmApiKeyMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/api-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"keys": []any{map[string]any{
				"id": "key-1", "name": "ci-runner-key", "prefix": "mvk_7Fa1", "last_used_at": nil,
			}},
		})
	})

	mux.HandleFunc("POST /microvm/api-keys", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] == nil || body["name"] == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The name field is required.",
				"errors":  map[string]any{"name": []string{"The name field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"key": map[string]any{
				"id": "key-2", "name": body["name"], "prefix": "mvk_9Zz2",
			},
			// plaintext is shown exactly once, at creation.
			"plaintext": "mvk_9Zz2b2c3d4e5f6a7b8c9d0e1f2a3b4c5",
		})
	})

	mux.HandleFunc("DELETE /microvm/api-key/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "API key not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "API key revoked."})
	})

	return mux
}

func TestMicrovmApiKey_ListSurfacesNoPlaintext(t *testing.T) {
	cs := connectSession(t, microvmApiKeyMock())

	res := callTool(t, cs, "user.microvm.api_key.list", map[string]any{})
	var list tools.MicrovmApiKeyListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Fatalf("count = %d, want 1", list.Count)
	}
	if list.Keys[0]["prefix"] != "mvk_7Fa1" {
		t.Errorf("prefix = %v, want mvk_7Fa1", list.Keys[0]["prefix"])
	}
	if _, ok := list.Keys[0]["plaintext"]; ok {
		t.Errorf("list result must never carry a plaintext field, got %v", list.Keys[0])
	}
}

func TestMicrovmApiKey_CreateReturnsPlaintextOnce(t *testing.T) {
	cs := connectSession(t, microvmApiKeyMock())

	res := callTool(t, cs, "user.microvm.api_key.create", map[string]any{})
	if !res.IsError || !strings.Contains(resultText(t, res), "name") {
		t.Fatalf("create without name: want a schema/validation error naming name, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.api_key.create", map[string]any{"name": "ci-runner-key"})
	var created tools.MicrovmApiKeyCreateResult
	unmarshalResult(t, res, &created)
	if created.Key["id"] != "key-2" {
		t.Errorf("key.id = %v, want key-2", created.Key["id"])
	}
	if created.Plaintext == "" {
		t.Errorf("plaintext must be returned on create")
	}
}

func TestMicrovmApiKey_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, microvmApiKeyMock())

	res := callTool(t, cs, "user.microvm.api_key.delete", map[string]any{"id": "key-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.api_key.delete", map[string]any{"id": "key-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}

	res = callTool(t, cs, "user.microvm.api_key.delete", map[string]any{"id": "missing", "confirm": true})
	if !res.IsError {
		t.Errorf("delete of a missing key should fail")
	}
}
