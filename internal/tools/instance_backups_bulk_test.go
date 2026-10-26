package tools_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func backupIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("b-%03d", i)
	}
	return ids
}

// The Master rejects more than 100 backup ids per POST .../backups/destroy (422), so the
// tool must send longer lists in chunks of at most 100.
func TestDestroyInstanceBackups_ChunksAtTheMasterCap(t *testing.T) {
	var sizes []int
	mux := http.NewServeMux()
	mux.HandleFunc("POST /instance/{id}/backups/destroy", func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			Backups []string `json:"backups"`
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		sizes = append(sizes, len(got.Backups))
		if len(got.Backups) > 100 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "The backups field must not have more than 100 items."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.instance.destroy_backups", map[string]any{"id": "i1", "backups": backupIDs(250), "confirm": true})
	var out tools.OKResult
	unmarshalResult(t, res, &out)

	want := []int{100, 100, 50}
	if fmt.Sprint(sizes) != fmt.Sprint(want) {
		t.Fatalf("request sizes = %v, want %v", sizes, want)
	}
}

func TestDestroyInstanceBackups_ExactlyTheCapIsOneRequest(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /instance/{id}/backups/destroy", func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})
	cs := connectSession(t, mux)

	callTool(t, cs, "user.instance.destroy_backups", map[string]any{"id": "i1", "backups": backupIDs(100), "confirm": true})
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDestroyInstanceBackups_ToolDescriptionStatesTheCap(t *testing.T) {
	cs := connectSession(t, http.NewServeMux())
	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "user.instance.destroy_backups" {
			if !strings.Contains(tool.Description, "100") {
				t.Fatalf("description must state the 100 id cap: %q", tool.Description)
			}
			return
		}
	}
	t.Fatal("user.instance.destroy_backups not registered")
}

// A tool this server registers for an endpoint must not be recorded as excluded for MCP in the manifest
// (the per-instance bulk delete was marked excluded while this tool consumed it).
func TestManifest_InstanceBulkBackupDeleteIsCoveredByItsTool(t *testing.T) {
	m := loadManifest(t)
	for _, e := range m.Endpoints {
		if e.ID == "user POST /api/instance/{user_instanceId}/backups/destroy" {
			if e.MCP.Status != "covered" || e.MCP.Tool != "user.instance.destroy_backups" {
				t.Fatalf("manifest mcp = %+v, want covered by user.instance.destroy_backups", e.MCP)
			}
			return
		}
	}
	t.Fatal("manifest has no entry for the per-instance bulk backup delete")
}
