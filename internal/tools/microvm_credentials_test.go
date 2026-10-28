package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func TestMicrovmSecretsToolsUseRealContractAndRedactAfterMapping(t *testing.T) {
	const value = "sentinel-secret"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /microvm/secrets", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("kind") != "all" {
			t.Error("kind filter lost")
		}
		writeJSON(w, 200, map[string]any{"success": true, "secrets": []any{map[string]any{"id": "s1", "name": "KEY", "kind": "registry", "registry": "ghcr.io", "username": "octocat", "value": value}}})
	})
	mux.HandleFunc("POST /microvm/secrets", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["value"] != value {
			t.Error("value not sent")
		}
		w.Header().Set("X-Request-Id", value)
		writeJSON(w, 422, map[string]any{"message": value, "reason": "secret_exists", "errors": map[string]any{"value": []string{value}}})
	})
	mux.HandleFunc("PUT /microvm/secret/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["value"]; ok {
			t.Error("rename sent absent value")
		}
		writeJSON(w, 200, map[string]any{"success": true, "secret": map[string]any{"id": "s1", "name": body["name"]}})
	})
	cs := connectSession(t, mux)
	res := callTool(t, cs, "user.microvm_secret.list", map[string]any{"kind": "all"})
	var list tools.MicrovmSecretListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Fatal("list envelope lost")
	}
	if strings.Contains(resultText(t, res), value) {
		t.Fatal("list leaked value")
	}
	res = callTool(t, cs, "user.microvm_secret.create", map[string]any{"name": "KEY", "value": value})
	if !res.IsError || strings.Contains(resultText(t, res), value) || !strings.Contains(resultText(t, res), "secret_exists") {
		t.Fatalf("unsafe mapped error: %s", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm_secret.update", map[string]any{"id": "s1", "name": "RENAMED"})
	var updated tools.MicrovmSecretResult
	unmarshalResult(t, res, &updated)
	if updated.Secret["name"] != "RENAMED" {
		t.Fatal("rename response")
	}
	res = callTool(t, cs, "user.microvm_secret.delete", map[string]any{"id": "s1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatal("delete must require confirmation")
	}
}
func TestMicrovmMcpToolsFalseEmptyRotateAndBrowserConnection(t *testing.T) {
	mux := http.NewServeMux()
	row := map[string]any{"id": "m1", "provider": "custom", "name": "tool", "label": "Tool", "enabled": false, "revision": map[string]any{"id": "r1", "revision": 1, "auth_kind": "bearer", "headers": []any{}, "static_token_preview": "masked"}, "connection": nil, "kit": map[string]any{"supported": nil, "mode": nil}}
	mux.HandleFunc("PUT /microvm/mcp-server/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["enabled"] != false {
			t.Error("explicit false dropped")
		}
		if headers, ok := body["headers"].(map[string]any); !ok || len(headers) != 0 {
			t.Error("empty header map dropped")
		}
		if _, ok := body["static_token"]; ok {
			t.Error("metadata update carried token")
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": row})
	})
	mux.HandleFunc("POST /microvm/mcp-server/{id}/rotate-token", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["static_token"] != "sentinel" {
			t.Error("rotation missing supplied token")
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": row})
	})
	mux.HandleFunc("POST /microvm/mcp-server/{id}/connect", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "authorize_url": "https://example.test/auth?state=unchanged"})
	})
	cs := connectSession(t, mux)
	res := callTool(t, cs, "user.microvm_mcp_server.update", map[string]any{"id": "m1", "enabled": false, "headers": map[string]any{}})
	var updated tools.MicrovmMcpServerResult
	unmarshalResult(t, res, &updated)
	if updated.Server["id"] != "m1" {
		t.Fatal("nested data lost")
	}
	res = callTool(t, cs, "user.microvm_mcp_server.rotate_token", map[string]any{"id": "m1", "static_token": "sentinel"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatal("rotation needs confirmation")
	}
	res = callTool(t, cs, "user.microvm_mcp_server.rotate_token", map[string]any{"id": "m1", "static_token": "sentinel", "confirm": true})
	unmarshalResult(t, res, &updated)
	if strings.Contains(resultText(t, res), "sentinel") {
		t.Fatal("token echoed")
	}
	res = callTool(t, cs, "user.microvm_mcp_server.connect", map[string]any{"id": "m1"})
	var connected tools.MicrovmMcpConnectResult
	unmarshalResult(t, res, &connected)
	if connected.AuthorizeURL != "https://example.test/auth?state=unchanged" || !strings.Contains(connected.Note, "same person") {
		t.Fatal("browser authorization contract lost")
	}
}
