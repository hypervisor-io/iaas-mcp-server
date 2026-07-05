package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func managedDatabaseMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	polls := 0
	deleted := false

	mux.HandleFunc("POST /databases", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "managed_database": map[string]any{"id": "db-1", "status": "deploying"}})
	})
	mux.HandleFunc("GET /databases", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":           true,
			"managed_databases": map[string]any{"current_page": 1, "last_page": 1, "data": []any{map[string]any{"id": "db-1", "name": "prod"}}},
		})
	})
	mux.HandleFunc("GET /database/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gone := deleted
		polls++
		status := "active"
		if polls < 2 {
			status = "deploying"
		}
		mu.Unlock()
		if gone {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Database not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "managed_database": map[string]any{"id": r.PathValue("id"), "status": status, "admin_user": "root"},
		})
	})
	mux.HandleFunc("DELETE /database/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = true
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "queued"})
	})
	mux.HandleFunc("POST /database/{id}/restart", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "restarting"})
	})
	mux.HandleFunc("POST /database/{id}/reset-password", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ok", "password": "new-secret"})
	})
	mux.HandleFunc("POST /database/{id}/acknowledge-error", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ack"})
	})

	// Parameter groups.
	mux.HandleFunc("POST /db/parameter-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "parameter_group": map[string]any{"id": "pg-1", "name": "tuned"}})
	})
	mux.HandleFunc("GET /db/parameter-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "parameter_groups": []any{map[string]any{"id": "pg-1", "name": "tuned"}},
		})
	})
	mux.HandleFunc("DELETE /db/parameter-group/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestManagedDatabase_CreateConvergesAndActions(t *testing.T) {
	cs := connectSession(t, managedDatabaseMock())

	res := callTool(t, cs, "user.managed_database.create", map[string]any{
		"name": "prod", "engine": "postgresql", "engine_version": "16", "db_plan_id": "dp-1",
		"vpc_id": "vpc-1", "vpc_subnet_id": "sub-1",
	})
	var created tools.ManagedDatabaseResult
	unmarshalResult(t, res, &created)
	if created.ManagedDatabase["id"] != "db-1" || created.ManagedDatabase["status"] != "active" {
		t.Fatalf("create = %v, want db-1/active", created.ManagedDatabase)
	}

	res = callTool(t, cs, "user.managed_database.reset_password", map[string]any{"id": "db-1"})
	var pw tools.PasswordResult
	unmarshalResult(t, res, &pw)
	if pw.Password != "new-secret" {
		t.Errorf("reset_password = %q, want new-secret", pw.Password)
	}

	res = callTool(t, cs, "user.managed_database.restart", map[string]any{"id": "db-1"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("restart ok = false")
	}
}

func TestManagedDatabase_DeleteConfirmConverges(t *testing.T) {
	cs := connectSession(t, managedDatabaseMock())
	res := callTool(t, cs, "user.managed_database.delete", map[string]any{"id": "db-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.managed_database.delete", map[string]any{"id": "db-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not converge")
	}
}

func TestDBParameterGroup_CRUD(t *testing.T) {
	cs := connectSession(t, managedDatabaseMock())

	res := callTool(t, cs, "user.db_parameter_group.create", map[string]any{
		"name": "tuned", "engine": "mysql", "parameters": map[string]any{"max_connections": "200"},
	})
	var created tools.DBParameterGroupResult
	unmarshalResult(t, res, &created)
	if created.ParameterGroup["id"] != "pg-1" {
		t.Fatalf("create id = %v, want pg-1", created.ParameterGroup["id"])
	}

	res = callTool(t, cs, "user.db_parameter_group.list", map[string]any{})
	var list tools.DBParameterGroupListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.db_parameter_group.delete", map[string]any{"id": "pg-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("delete did not succeed")
	}
}
