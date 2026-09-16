package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// microvmSettingsMock keeps one in-memory default_network value, mirroring
// UpdateMicrovmSettingsRequest: a vpc entry must name vpc_subnet_id
// (mock-enforced as "vpc-owned" vs "vpc-foreign" to exercise the 422 path),
// and a public entry needs no subnet_id at all (C8.6).
func microvmSettingsMock() http.Handler {
	var stored map[string]any // nil == no default set

	mux := http.NewServeMux()
	mux.HandleFunc("GET /microvm/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"default_network": stored})
	})
	mux.HandleFunc("PUT /microvm/settings", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		raw, hasKey := body["default_network"]
		if !hasKey || raw == nil {
			stored = nil
			writeJSON(w, http.StatusOK, map[string]any{"default_network": nil})
			return
		}

		entry, _ := raw.(map[string]any)
		if entry["kind"] == "vpc" && entry["vpc_subnet_id"] == "vpc-foreign" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The selected vpc subnet does not belong to your account.",
				"errors":  map[string]any{"default_network.vpc_subnet_id": []string{"The selected vpc subnet does not belong to your account."}},
			})
			return
		}
		if entry["kind"] == "public" {
			if _, hasSubnet := entry["subnet_id"]; hasSubnet {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "unexpected subnet_id"})
				return
			}
		}
		stored = entry
		writeJSON(w, http.StatusOK, map[string]any{"default_network": stored})
	})
	return mux
}

// dn dereferences a MicrovmSettingsResult.DefaultNetwork (a *map[string]any
// so the tool's schema allows an explicit JSON null), returning an empty map
// rather than panicking when it is nil.
func dn(r tools.MicrovmSettingsResult) map[string]any {
	if r.DefaultNetwork == nil {
		return map[string]any{}
	}
	return *r.DefaultNetwork
}

func TestMicrovmSettings_GetInitiallyNull(t *testing.T) {
	cs := connectSession(t, microvmSettingsMock())

	res := callTool(t, cs, "user.microvm.settings.get", map[string]any{})
	if res.IsError {
		t.Fatalf("get failed: %s", resultText(t, res))
	}
	var got tools.MicrovmSettingsResult
	unmarshalResult(t, res, &got)
	if got.DefaultNetwork != nil {
		t.Errorf("default_network = %v, want nil (no default set)", got.DefaultNetwork)
	}
}

func TestMicrovmSettings_SetPublicNeedsNoSubnet(t *testing.T) {
	cs := connectSession(t, microvmSettingsMock())

	res := callTool(t, cs, "user.microvm.settings.set", map[string]any{
		"default_network": map[string]any{"kind": "public"},
	})
	if res.IsError {
		t.Fatalf("set public failed: %s", resultText(t, res))
	}
	var got tools.MicrovmSettingsResult
	unmarshalResult(t, res, &got)
	if dn(got)["kind"] != "public" {
		t.Fatalf("default_network.kind = %v, want public", dn(got)["kind"])
	}
	if _, hasSubnet := dn(got)["subnet_id"]; hasSubnet {
		t.Errorf("public default carried a subnet_id it was never given: %v", got.DefaultNetwork)
	}

	// GET now round-trips the stored default.
	res = callTool(t, cs, "user.microvm.settings.get", map[string]any{})
	unmarshalResult(t, res, &got)
	if dn(got)["kind"] != "public" {
		t.Errorf("get after set = %v, want kind public", got.DefaultNetwork)
	}
}

func TestMicrovmSettings_SetVpcAndForeignSubnetIsValidationError(t *testing.T) {
	cs := connectSession(t, microvmSettingsMock())

	res := callTool(t, cs, "user.microvm.settings.set", map[string]any{
		"default_network": map[string]any{"kind": "vpc", "vpc_subnet_id": "vpc-owned"},
	})
	if res.IsError {
		t.Fatalf("set vpc failed: %s", resultText(t, res))
	}
	var got tools.MicrovmSettingsResult
	unmarshalResult(t, res, &got)
	if dn(got)["vpc_subnet_id"] != "vpc-owned" {
		t.Errorf("vpc_subnet_id = %v, want vpc-owned", dn(got)["vpc_subnet_id"])
	}

	res = callTool(t, cs, "user.microvm.settings.set", map[string]any{
		"default_network": map[string]any{"kind": "vpc", "vpc_subnet_id": "vpc-foreign"},
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Fatalf("set foreign vpc subnet: want validation failed, got %q", resultText(t, res))
	}
}

func TestMicrovmSettings_Clear(t *testing.T) {
	cs := connectSession(t, microvmSettingsMock())

	res := callTool(t, cs, "user.microvm.settings.set", map[string]any{
		"default_network": map[string]any{"kind": "public"},
	})
	if res.IsError {
		t.Fatalf("set failed: %s", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.settings.set", map[string]any{"clear": true})
	if res.IsError {
		t.Fatalf("clear failed: %s", resultText(t, res))
	}
	var got tools.MicrovmSettingsResult
	unmarshalResult(t, res, &got)
	if got.DefaultNetwork != nil {
		t.Errorf("default_network after clear = %v, want nil", got.DefaultNetwork)
	}

	res = callTool(t, cs, "user.microvm.settings.get", map[string]any{})
	unmarshalResult(t, res, &got)
	if got.DefaultNetwork != nil {
		t.Errorf("get after clear = %v, want nil", got.DefaultNetwork)
	}
}
