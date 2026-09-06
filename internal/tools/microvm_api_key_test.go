package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// microvmApiKeyMock mocks the MV1-26 MicroVM Sandboxes API key endpoints.
func microvmApiKeyMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/api-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"keys": []any{
				map[string]any{"id": "k-1", "name": "CI", "prefix": "vc_sb_ab12"},
				map[string]any{"id": "k-2", "name": "staging", "prefix": "vc_sb_cd34"},
			},
		})
	})

	mux.HandleFunc("POST /microvm/api-keys", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success":   true,
			"key":       map[string]any{"id": "k-new", "name": body["name"], "prefix": "vc_sb_ab12"},
			"plaintext": "vc_sb_ab12cdef",
		})
	})

	mux.HandleFunc("DELETE /microvm/api-key/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "API key revoked."})
	})

	return mux
}

func TestCreateMicrovmApiKeyTool_ReturnsPlaintext(t *testing.T) {
	cs := connectSession(t, microvmApiKeyMock())

	res := callTool(t, cs, "user.microvm.api_key.create", map[string]any{"name": "CI"})
	var result tools.MicrovmApiKeyResult
	unmarshalResult(t, res, &result)
	if result.Plaintext == "" {
		t.Fatal("expected a plaintext key in the result")
	}
	if result.Plaintext != "vc_sb_ab12cdef" {
		t.Errorf("plaintext = %q, want vc_sb_ab12cdef", result.Plaintext)
	}
	if result.Key["id"] != "k-new" || result.Key["name"] != "CI" {
		t.Errorf("key = %v, want {k-new CI}", result.Key)
	}
}

func TestListMicrovmApiKeys_ReturnsCountAndItems(t *testing.T) {
	cs := connectSession(t, microvmApiKeyMock())

	res := callTool(t, cs, "user.microvm.api_key.list", map[string]any{})
	var list tools.MicrovmApiKeyListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("expected 2 keys, got %d", list.Count)
	}
	if list.Keys[0]["id"] != "k-1" {
		t.Errorf("keys[0].id = %v, want k-1", list.Keys[0]["id"])
	}
	// The listing never carries key material.
	for _, key := range list.Keys {
		if _, present := key["plaintext"]; present {
			t.Errorf("list result must NOT include plaintext; got %v", key)
		}
	}
}

func TestDeleteMicrovmApiKey_RequiresConfirmation(t *testing.T) {
	cs := connectSession(t, microvmApiKeyMock())

	res := callTool(t, cs, "user.microvm.api_key.delete", map[string]any{"id": "k-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.api_key.delete", map[string]any{"id": "k-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "k-1" {
		t.Errorf("delete result = %+v, want {k-1 true}", del)
	}
}
