package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// instanceRescueMock exercises POST /instance/{id}/rescue exactly per
// InstanceService::rescue() at Master 8eb77dcfe: a bare envelope carrying
// {success,message,task_id,rescue:{active,since,username,password}} on
// success, and a 409 {success:false,message} on a guard conflict (a
// dedicated instance id, "already-active", always 409s).
func instanceRescueMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /instance/{id}/rescue", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "already-active" {
			writeJSON(w, http.StatusConflict, map[string]any{
				"success": false,
				"message": "This instance is already in rescue mode.",
			})
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		enable, _ := body["enable"].(bool)
		if enable {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": true,
				"message": "The instance is entering rescue mode.",
				"task_id": "task-rescue-enter",
				"rescue": map[string]any{
					"active":   true,
					"since":    "2026-09-24T00:00:00.000000Z",
					"username": "root",
					"password": "aB3xQ9zK7mP2rL5t",
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "The instance is exiting rescue mode.",
			"task_id": "task-rescue-exit",
			"rescue": map[string]any{
				"active":   false,
				"since":    nil,
				"username": "root",
				"password": nil,
			},
		})
	})
	return mux
}

func TestInstanceRescue_EnterAndExit(t *testing.T) {
	cs := connectSession(t, instanceRescueMock())

	res := callTool(t, cs, "user.instance.rescue", map[string]any{"id": "inst-1", "enable": true})
	var entered tools.ObjectResult
	unmarshalResult(t, res, &entered)
	if entered.Object["task_id"] != "task-rescue-enter" {
		t.Fatalf("enter task_id = %v, want task-rescue-enter", entered.Object["task_id"])
	}
	rescue, ok := entered.Object["rescue"].(map[string]any)
	if !ok || rescue["active"] != true || rescue["password"] != "aB3xQ9zK7mP2rL5t" || rescue["username"] != "root" {
		t.Fatalf("enter rescue block = %v", entered.Object["rescue"])
	}

	res = callTool(t, cs, "user.instance.rescue", map[string]any{"id": "inst-1", "enable": false})
	var exited tools.ObjectResult
	unmarshalResult(t, res, &exited)
	if exited.Object["task_id"] != "task-rescue-exit" {
		t.Fatalf("exit task_id = %v, want task-rescue-exit", exited.Object["task_id"])
	}
	rescue, ok = exited.Object["rescue"].(map[string]any)
	if !ok || rescue["active"] != false || rescue["password"] != nil {
		t.Fatalf("exit rescue block = %v", exited.Object["rescue"])
	}
}

// TestInstanceRescue_ConflictSurfacesError proves a 409 guard failure (already
// active, suspended, task running, etc.) surfaces as an MCP tool error
// carrying the API message, not a silently-successful result.
func TestInstanceRescue_ConflictSurfacesError(t *testing.T) {
	cs := connectSession(t, instanceRescueMock())
	res := callTool(t, cs, "user.instance.rescue", map[string]any{"id": "already-active", "enable": true})
	if !res.IsError {
		t.Fatalf("expected an error result for an already-active rescue conflict")
	}
	msg := resultText(t, res)
	if !strings.Contains(msg, "already in rescue mode") {
		t.Fatalf("error message = %q, want it to mention already in rescue mode", msg)
	}
}
