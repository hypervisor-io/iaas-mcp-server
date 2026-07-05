package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func projectMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /projects", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] == "" || body["name"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"name": []string{"The name field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "project": map[string]any{"id": "proj-1", "name": body["name"]}})
	})
	mux.HandleFunc("GET /projects", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "proj-1", "name": "prod"}},
		})
	})
	mux.HandleFunc("GET /project/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Project not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "project": map[string]any{"id": id, "name": "prod"}})
	})
	mux.HandleFunc("PATCH /project/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "project": map[string]any{"id": id, "name": "renamed"}})
	})
	mux.HandleFunc("DELETE /project/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("POST /project/assign-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ok"})
	})
	return mux
}

func TestProject_CRUDAndAssign(t *testing.T) {
	cs := connectSession(t, projectMock())

	res := callTool(t, cs, "user.project.create", map[string]any{"name": "prod"})
	var created tools.ProjectResult
	unmarshalResult(t, res, &created)
	if created.Project["id"] != "proj-1" {
		t.Fatalf("create id = %v, want proj-1", created.Project["id"])
	}

	res = callTool(t, cs, "user.project.list", map[string]any{})
	var list tools.ProjectListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.project.get", map[string]any{"id": "proj-1"})
	var got tools.ProjectResult
	unmarshalResult(t, res, &got)
	if got.Project["id"] != "proj-1" {
		t.Errorf("get id = %v, want proj-1", got.Project["id"])
	}

	res = callTool(t, cs, "user.project.assign_resource", map[string]any{
		"resource_type": "instance", "resource_id": "inst-1", "project_id": "proj-1",
	})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("assign ok = false")
	}

	res = callTool(t, cs, "user.project.unassign_resource", map[string]any{
		"resource_type": "instance", "resource_id": "inst-1",
	})
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("unassign ok = false")
	}
}

func TestProject_DeleteConfirmAndErrors(t *testing.T) {
	cs := connectSession(t, projectMock())
	res := callTool(t, cs, "user.project.delete", map[string]any{"id": "proj-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.project.delete", map[string]any{"id": "proj-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
	res = callTool(t, cs, "user.project.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
}
