package tools_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestManagedDatabaseAdmission_RefusalsAreToolErrors(t *testing.T) {
	for _, tc := range []struct {
		tool, method, path, reason string
		args                       map[string]any
	}{
		{"backup", "POST", "/backup", "backup_in_progress", map[string]any{"backup_type": "full"}},
		{"backup", "POST", "/backup", "database_busy", map[string]any{"backup_type": "incremental"}},
		{"backup", "POST", "/backup", "restore_not_activated", nil},
		{"backup", "POST", "/backup", "pitr_reopen_in_progress", nil},
		{"backup", "POST", "/backup", "recovery_in_progress", nil},
		{"restart", "POST", "/restart", "restore_not_activated", nil},
		{"reset_password", "POST", "/reset-password", "restore_not_activated", nil},
		{"resize", "PATCH", "/resize", "restore_not_activated", map[string]any{"db_plan_id": "plan-2"}},
		{"apply_parameter_group", "PATCH", "/parameter-group", "restore_not_activated", map[string]any{"parameter_group_id": "pg-2"}},
	} {
		t.Run(tc.tool+"/"+tc.reason, func(t *testing.T) {
			calls := 0
			mux := http.NewServeMux()
			mux.HandleFunc(tc.method+" /database/{id}"+tc.path, func(w http.ResponseWriter, r *http.Request) {
				calls++
				writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": tc.reason, "message": "Operation refused: " + tc.reason})
			})
			args := map[string]any{"id": "db-1"}
			for k, v := range tc.args {
				args[k] = v
			}
			res := callTool(t, connectSession(t, mux), "user.managed_database."+tc.tool, args)
			if !res.IsError || !strings.Contains(resultText(t, res), "Operation refused: "+tc.reason) || calls != 1 {
				t.Fatalf("refusal returned success/lost message/replayed: result=%s calls=%d", resultText(t, res), calls)
			}
		})
	}
}

func TestManagedDatabaseBackup_UncertainDispatchIsNotReplayed(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /database/{id}/backup", func(w http.ResponseWriter, r *http.Request) {
				calls++
				writeJSON(w, status, map[string]any{"success": false, "message": "Backup dispatch acknowledgement is uncertain. Await the node callback before retrying."})
			})
			res := callTool(t, connectSession(t, mux), "user.managed_database.backup", map[string]any{"id": "db-1", "backup_type": "full"})
			if !res.IsError || !strings.Contains(resultText(t, res), "uncertain") || calls != 1 {
				t.Fatalf("uncertain backup returned success or replayed: result=%s calls=%d", resultText(t, res), calls)
			}
		})
	}
}

func TestManagedDatabaseBackup_FailedResponseIsToolError(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		incomplete          bool
	}{
		{"user-dispatch-failure", `{"success":false,"message":"Backup dispatch acknowledgement is uncertain. Await the node callback before retrying."}`, "uncertain", false},
		{"invalid-json", `{`, "decoding response", false},
		{"interrupted-response", `{`, "reading response body", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			mux := http.NewServeMux()
			mux.HandleFunc("POST /database/{id}/backup", func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tc.incomplete {
					w.Header().Set("Content-Length", "100")
				}
				_, _ = w.Write([]byte(tc.body))
			})
			res := callTool(t, connectSession(t, mux), "user.managed_database.backup", map[string]any{"id": "db-1"})
			if !res.IsError || !strings.Contains(resultText(t, res), tc.message) || calls != 1 {
				t.Fatalf("failed backup returned success/lost error/replayed: result=%s calls=%d", resultText(t, res), calls)
			}
		})
	}
}
