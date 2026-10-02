package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestManagedDatabaseBackup_BackupTypePassedThrough(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /database/{id}/backup", func(w http.ResponseWriter, r *http.Request) {
		got = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["backup_type"] == "incremental" && r.PathValue("id") == "db-pg" {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "incremental_requires_pitr", "message": "needs point-in-time recovery"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Backup initiated"})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.managed_database.backup", map[string]any{"id": "db-1", "backup_type": "incremental"})
	if res.IsError || got["backup_type"] != "incremental" {
		t.Fatalf("incremental: isError=%v body=%v", res.IsError, got)
	}

	got = nil
	res = callTool(t, cs, "user.managed_database.backup", map[string]any{"id": "db-1", "backup_type": "full"})
	if res.IsError || got["backup_type"] != "full" {
		t.Fatalf("full: isError=%v body=%v", res.IsError, got)
	}

	got = nil
	res = callTool(t, cs, "user.managed_database.backup", map[string]any{"id": "db-1"})
	if res.IsError {
		t.Fatalf("default backup failed: %s", resultText(t, res))
	}
	if _, ok := got["backup_type"]; ok {
		t.Errorf("omitted backup_type must not be sent, got %v", got)
	}

	res = callTool(t, cs, "user.managed_database.backup", map[string]any{"id": "db-pg", "backup_type": "incremental"})
	if !res.IsError || !strings.Contains(resultText(t, res), "point-in-time") {
		t.Fatalf("a 409 refusal must surface its message; got %q", resultText(t, res))
	}
}

func TestManagedDatabaseBackup_RejectsUnknownBackupType(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("POST /database/{id}/backup", func(w http.ResponseWriter, r *http.Request) { called = true })
	cs := connectSession(t, mux)
	res := callTool(t, cs, "user.managed_database.backup", map[string]any{"id": "db-1", "backup_type": "differential"})
	if !res.IsError || called {
		t.Fatalf("unknown backup_type must be refused before any request; isError=%v called=%v", res.IsError, called)
	}
}
