package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func microvmConnectorMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/connectors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"connectors": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{map[string]any{
					"id": "conn-1", "name": "gh-runners", "kind": "github", "enabled": true,
					"location_id": "loc-1", "location_name": "Nuremberg 1",
				}},
			},
			"plans":                 []any{map[string]any{"id": "plan-1", "name": "micro-1", "vcpu": float64(1)}},
			"hypervisor_groups":     []any{map[string]any{"id": "loc-1", "name": "nbg1", "display_name": "Nuremberg 1"}},
			"images":                []any{map[string]any{"id": "img-1", "name": "debian-13", "status": "ready"}},
			"git_sources":           []any{map[string]any{"id": "src-1", "name": "my-org", "provider": "github_app"}},
			"github_app_configured": true,
		})
	})

	mux.HandleFunc("GET /microvm/connectors/jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"jobs": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{map[string]any{
					"id": "job-1", "connector_id": "conn-1", "status": "completed",
				}},
			},
		})
	})

	mux.HandleFunc("POST /microvm/connectors/github", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["git_source_id"] == "bad" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The selected git source was not found.",
				"errors":  map[string]any{"git_source_id": []string{"The selected git source was not found."}},
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"connector": map[string]any{
				"id": "conn-2", "name": body["name"], "kind": "github", "enabled": false,
			},
		})
	})

	mux.HandleFunc("POST /microvm/connectors/gitlab", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"connector": map[string]any{
				"id": "conn-3", "name": body["name"], "kind": "gitlab", "enabled": false,
			},
		})
	})

	mux.HandleFunc("PUT /microvm/connector/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["enabled"] == true {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "needs_plan",
				"errors":  map[string]any{"enabled": []string{"needs_plan"}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"connector": map[string]any{
				"id": r.PathValue("id"), "name": body["name"], "enabled": false,
			},
		})
	})

	mux.HandleFunc("DELETE /microvm/connector/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Connector not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	return mux
}

func TestMicrovmConnector_ListSurfacesCatalog(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.list", map[string]any{})
	var list tools.MicrovmConnectorListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Fatalf("count = %d, want 1", list.Count)
	}
	if list.Connectors[0]["name"] != "gh-runners" {
		t.Errorf("connectors[0].name = %v, want gh-runners", list.Connectors[0]["name"])
	}
	if len(list.Plans) != 1 || list.Plans[0]["name"] != "micro-1" {
		t.Errorf("plans = %v, want one plan named micro-1", list.Plans)
	}
	if len(list.GitSources) != 1 {
		t.Errorf("git_sources = %v, want one entry", list.GitSources)
	}
	if !list.GithubAppConfigured {
		t.Errorf("github_app_configured = false, want true")
	}
}

func TestMicrovmConnector_Jobs(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.jobs", map[string]any{})
	var jobs tools.MicrovmConnectorJobListResult
	unmarshalResult(t, res, &jobs)
	if jobs.Count != 1 || jobs.Jobs[0]["status"] != "completed" {
		t.Errorf("jobs = %v, want one completed job", jobs.Jobs)
	}
}

func TestMicrovmConnector_CreateGithubValidationError(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.create_github", map[string]any{
		"git_source_id": "bad", "name": "gh-runners",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "git_source_id") {
		t.Fatalf("create_github with bad source: want a git_source_id error, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.connector.create_github", map[string]any{
		"git_source_id": "src-1", "name": "gh-runners",
	})
	var created tools.MicrovmConnectorResult
	unmarshalResult(t, res, &created)
	if created.Connector["id"] != "conn-2" {
		t.Errorf("connector.id = %v, want conn-2", created.Connector["id"])
	}
}

func TestMicrovmConnector_CreateGitlab(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.create_gitlab", map[string]any{
		"name": "gl-runners", "gitlab_url": "https://gitlab.com", "gitlab_token": "glrt-abc123",
	})
	var created tools.MicrovmConnectorResult
	unmarshalResult(t, res, &created)
	if created.Connector["kind"] != "gitlab" {
		t.Errorf("connector.kind = %v, want gitlab", created.Connector["kind"])
	}
}

// TestMicrovmConnector_UpdateEnabledNeedsPlan pins that Enabled is a tri-state
// *bool: omitting it never sends "enabled" (update succeeds), while an
// explicit true is sent through and can be refused by the API.
func TestMicrovmConnector_UpdateEnabledNeedsPlan(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.update", map[string]any{
		"id": "conn-1", "name": "gh-runners-2",
	})
	var updated tools.MicrovmConnectorResult
	unmarshalResult(t, res, &updated)
	if updated.Connector["name"] != "gh-runners-2" {
		t.Errorf("connector.name = %v, want gh-runners-2", updated.Connector["name"])
	}

	res = callTool(t, cs, "user.microvm.connector.update", map[string]any{
		"id": "conn-1", "enabled": true,
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "needs_plan") {
		t.Fatalf("enabling without plan+location: want needs_plan error, got %q", resultText(t, res))
	}
}

func TestMicrovmConnector_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.delete", map[string]any{"id": "conn-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.connector.delete", map[string]any{"id": "conn-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}

	res = callTool(t, cs, "user.microvm.connector.delete", map[string]any{"id": "missing", "confirm": true})
	if !res.IsError {
		t.Errorf("delete of a missing connector should fail")
	}
}
