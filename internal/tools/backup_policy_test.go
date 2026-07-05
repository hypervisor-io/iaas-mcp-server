package tools_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func backupPolicyMock() http.Handler {
	mux := http.NewServeMux()
	// Instance backup policies.
	mux.HandleFunc("POST /backup-policies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "policy": map[string]any{"id": "ibp-1", "name": "daily"}})
	})
	mux.HandleFunc("GET /backup-policies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "ibp-1", "name": "daily"}},
		})
	})
	mux.HandleFunc("DELETE /backup-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("POST /backup-policy/{id}/attach", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "attached"})
	})
	// DB backup policies.
	mux.HandleFunc("POST /networking/db-backup-policies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "policy": map[string]any{"id": "dbp-1", "name": "s3-daily"}})
	})
	mux.HandleFunc("GET /networking/db-backup-policies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "dbp-1", "name": "s3-daily"}},
		})
	})
	mux.HandleFunc("POST /networking/db-backup-policy/{id}/attach", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "attached"})
	})
	return mux
}

func TestInstanceBackupPolicy_CreateAndAttach(t *testing.T) {
	cs := connectSession(t, backupPolicyMock())

	res := callTool(t, cs, "user.instance_backup_policy.create", map[string]any{
		"name": "daily", "full_backup_frequency": "daily", "full_backup_time": "02:00",
		"max_incremental_chain": 7, "retention_count": 14, "backup_device": "primary",
	})
	var created tools.BackupPolicyResult
	unmarshalResult(t, res, &created)
	if created.Policy["id"] != "ibp-1" {
		t.Fatalf("create id = %v, want ibp-1", created.Policy["id"])
	}

	res = callTool(t, cs, "user.instance_backup_policy.list", map[string]any{})
	var list tools.BackupPolicyListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.instance_backup_policy.attach_instance", map[string]any{"policy_id": "ibp-1", "instance_id": "inst-1"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("attach_instance ok = false")
	}

	res = callTool(t, cs, "user.instance_backup_policy.delete", map[string]any{"id": "ibp-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
}

func TestDBBackupPolicy_CreateAndAttach(t *testing.T) {
	cs := connectSession(t, backupPolicyMock())

	res := callTool(t, cs, "user.db_backup_policy.create", map[string]any{
		"name": "s3-daily", "s3_endpoint": "https://s3.example.com", "s3_bucket": "backups",
		"s3_region": "us-east-1", "s3_access_key": "ak", "s3_secret_key": "sk",
		"full_backup_frequency": "daily", "full_backup_time": "03:00", "incremental_frequency": "6h",
		"retention_full_count": 7, "retention_incremental_days": 3, "retention_pitr_hours": 24,
	})
	var created tools.BackupPolicyResult
	unmarshalResult(t, res, &created)
	if created.Policy["id"] != "dbp-1" {
		t.Fatalf("create id = %v, want dbp-1", created.Policy["id"])
	}

	res = callTool(t, cs, "user.db_backup_policy.attach_database", map[string]any{"policy_id": "dbp-1", "database_id": "db-1"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("attach_database ok = false")
	}
}
