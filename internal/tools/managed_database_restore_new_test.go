package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDatabaseRestoreToNewKeepsUnconfirmedIDsWithoutReplay(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /database/{id}/restore-to-new", func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["backup_id"] != "backup" || body["vpc_id"] != "vpc" || body["vpc_subnet_id"] != "subnet" {
			t.Errorf("body=%v", body)
		}
		writeJSON(w, 503, map[string]any{"success": false, "reason": "deploy_dispatch_unconfirmed", "message": "dispatch uncertain", "data": map[string]any{"managed_database_id": "new-database", "task_id": "restore-task"}})
	})
	cs := connectSession(t, mux)
	result := callTool(t, cs, "user.managed_database.restore_to_new", map[string]any{"id": "source", "backup_id": "backup", "vpc_id": "vpc", "vpc_subnet_id": "subnet"})
	text := resultText(t, result)
	if !result.IsError || calls != 1 {
		t.Fatalf("calls=%d result=%s", calls, text)
	}
	for _, part := range []string{"deploy_dispatch_unconfirmed", "new-database", "restore-task", "do not retry"} {
		if !strings.Contains(text, part) {
			t.Errorf("missing %q in %s", part, text)
		}
	}
}
func TestDatabaseRestoreToNewPitrAcceptedAndExclusiveChoice(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /database/{id}/restore-to-new", func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["target_time"] != "2026-10-28T00:00:00Z" {
			t.Errorf("body=%v", body)
		}
		if _, ok := body["backup_id"]; ok {
			t.Errorf("unexpected backup_id: %v", body)
		}
		writeJSON(w, 202, map[string]any{"success": true, "data": map[string]any{"managed_database_id": "new-database", "task_id": "restore-task", "status": "restoring"}})
	})
	cs := connectSession(t, mux)
	for _, input := range []map[string]any{{"id": "source"}, {"id": "source", "backup_id": "backup", "target_time": "2026-10-28T00:00:00Z"}} {
		result := callTool(t, cs, "user.managed_database.restore_to_new", input)
		if !result.IsError || calls != 0 {
			t.Fatal("invalid choice sent request")
		}
	}
	result := callTool(t, cs, "user.managed_database.restore_to_new", map[string]any{"id": "source", "target_time": "2026-10-28T00:00:00Z"})
	if result.IsError || calls != 1 || !strings.Contains(resultText(t, result), "restoring") {
		t.Fatalf("calls=%d result=%s", calls, resultText(t, result))
	}
}
