package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestVolumePolicyPatchNullableFalseZero(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /storage/volume-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSON(w, 200, map[string]any{"success": true, "policy": map[string]any{"id": r.PathValue("id")}})
	})
	cs := connectSession(t, mux)
	result := callTool(t, cs, "user.volume_policy.update", map[string]any{"id": "policy", "timezone": nil, "backup_enabled": false, "keep_last": 0, "schedule_day": 0})
	if result.IsError {
		t.Fatalf("update error: %s", resultText(t, result))
	}
	if v, ok := got["timezone"]; !ok || v != nil {
		t.Fatalf("null timezone lost: %v", got)
	}
	if got["backup_enabled"] != false || got["keep_last"] != float64(0) || got["schedule_day"] != float64(0) {
		t.Fatalf("false/zero lost: %v", got)
	}
	if _, ok := got["snapshot_enabled"]; ok {
		t.Fatalf("omitted field sent: %v", got)
	}
	got = nil
	result = callTool(t, cs, "user.volume_policy.update", map[string]any{"id": "policy"})
	if result.IsError || len(got) != 0 {
		t.Fatalf("empty patch=%v error=%s", got, resultText(t, result))
	}
}
func TestVolumePolicyGetIncludesMembershipAndDeleteConfirmation(t *testing.T) {
	deletes := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /storage/volume-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "policy": map[string]any{"id": "policy", "volumes": []any{map[string]any{"id": "attached"}}, "recent_runs": []any{map[string]any{"reason": "volume_busy"}}}, "attachable_volumes": []any{map[string]any{"id": "available"}}})
	})
	mux.HandleFunc("DELETE /storage/volume-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
		deletes++
		writeJSON(w, 200, map[string]any{"success": true})
	})
	cs := connectSession(t, mux)
	result := callTool(t, cs, "user.volume_policy.get", map[string]any{"id": "policy"})
	text := resultText(t, result)
	if result.IsError || !strings.Contains(text, "available") || !strings.Contains(text, "volume_busy") || !strings.Contains(text, "attached") {
		t.Fatalf("result=%s", text)
	}
	result = callTool(t, cs, "user.volume_policy.delete", map[string]any{"id": "policy"})
	if !result.IsError || deletes != 0 {
		t.Fatal("delete confirmation not enforced")
	}
	result = callTool(t, cs, "user.volume_policy.delete", map[string]any{"id": "policy", "confirm": true})
	if result.IsError || deletes != 1 {
		t.Fatalf("delete=%s calls=%d", resultText(t, result), deletes)
	}
}
