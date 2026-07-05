package tools_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func userScriptMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /user-scripts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "script": map[string]any{"id": "us-1", "name": "setup", "type": "bash"},
		})
	})
	mux.HandleFunc("GET /user-scripts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{
				map[string]any{"id": "us-1", "name": "setup"},
				map[string]any{"id": "us-2", "name": "teardown"},
			},
		})
	})
	mux.HandleFunc("PATCH /user-script/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "script": map[string]any{"id": id, "name": "renamed"}})
	})
	mux.HandleFunc("DELETE /user-script/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestUserScript_CRUD(t *testing.T) {
	cs := connectSession(t, userScriptMock())

	res := callTool(t, cs, "user.user_script.create", map[string]any{
		"name": "setup", "type": "bash", "content": "#!/bin/bash\necho hi",
	})
	var created tools.UserScriptResult
	unmarshalResult(t, res, &created)
	if created.Script["id"] != "us-1" {
		t.Fatalf("create id = %v, want us-1", created.Script["id"])
	}

	res = callTool(t, cs, "user.user_script.list", map[string]any{})
	var list tools.UserScriptListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Errorf("list count = %d, want 2", list.Count)
	}

	// get uses list-and-match (no SHOW route).
	res = callTool(t, cs, "user.user_script.get", map[string]any{"id": "us-2"})
	var got tools.UserScriptResult
	unmarshalResult(t, res, &got)
	if got.Script["id"] != "us-2" {
		t.Errorf("get id = %v, want us-2", got.Script["id"])
	}

	res = callTool(t, cs, "user.user_script.update", map[string]any{"id": "us-1", "name": "renamed"})
	var upd tools.UserScriptResult
	unmarshalResult(t, res, &upd)
	if upd.Script["name"] != "renamed" {
		t.Errorf("update name = %v, want renamed", upd.Script["name"])
	}
}

func TestUserScript_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, userScriptMock())
	res := callTool(t, cs, "user.user_script.delete", map[string]any{"id": "us-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.user_script.delete", map[string]any{"id": "us-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
