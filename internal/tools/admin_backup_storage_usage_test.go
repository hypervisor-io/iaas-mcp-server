package tools_test

import (
	"net/http"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// PBS-53b: usage readings are nullable, admin-only destination metadata.
// Both tools retain all fields from the API envelope without truncating bytes.
func TestAdminBackupStorageUsagePassthrough(t *testing.T) {
	for _, known := range []bool{true, false} {
		t.Run(map[bool]string{true: "known", false: "unavailable"}[known], func(t *testing.T) {
			item := map[string]any{"id": "pbs-1", "storage_type": "pbs", "usage_total_bytes": nil, "usage_used_bytes": nil, "usage_checked_at": nil}
			if known {
				item["usage_total_bytes"] = float64(10995116277760)
				item["usage_used_bytes"] = float64(2199023255552)
				item["usage_checked_at"] = "2026-09-27T01:02:03.000000Z"
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v1/hypervisor/backup-storage/{id}", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": item})
			})
			mux.HandleFunc("GET /v1/hypervisor/backup-storages", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": []any{item}, "meta": map[string]any{"current_page": 1, "last_page": 1, "per_page": 10, "total": 1}})
			})
			cs := connectSession(t, mux)
			var got tools.AdminItemResult
			unmarshalResult(t, callTool(t, cs, "admin.backup_storage.get", map[string]any{"id": "pbs-1"}), &got)
			var list tools.AdminListResult
			unmarshalResult(t, callTool(t, cs, "admin.backup_storage.list", map[string]any{}), &list)
			if list.Count != 1 {
				t.Fatalf("count=%d", list.Count)
			}
			getData, ok := got.Item["data"].(map[string]any)
			if !ok {
				t.Fatalf("GET envelope missing data: %v", got.Item)
			}
			for _, body := range []map[string]any{getData, list.Items[0]} {
				for _, key := range []string{"usage_total_bytes", "usage_used_bytes", "usage_checked_at"} {
					value, present := body[key]
					if !present || value != item[key] {
						t.Errorf("%s=%v present=%v, want %v", key, value, present, item[key])
					}
				}
			}
		})
	}
}

func TestAdminBackupStorageUsageLegacyBareGet(t *testing.T) {
	item := map[string]any{"id": "pbs-1", "usage_total_bytes": float64(10995116277760), "usage_used_bytes": float64(2199023255552), "usage_checked_at": "2026-09-27T01:02:03.000000Z"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/hypervisor/backup-storage/{id}", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, item) })
	var got tools.AdminItemResult
	unmarshalResult(t, callTool(t, connectSession(t, mux), "admin.backup_storage.get", map[string]any{"id": "pbs-1"}), &got)
	for _, key := range []string{"usage_total_bytes", "usage_used_bytes", "usage_checked_at"} {
		if got.Item[key] != item[key] {
			t.Errorf("%s=%v, want %v", key, got.Item[key], item[key])
		}
	}
}
