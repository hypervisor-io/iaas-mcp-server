package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// microvmConnectorMock mocks the MVU2-3 connector endpoints. CREATE/UPDATE
// echo the received request body back under the returned connector's "echo"
// key so the tests can pin exactly which fields the tool sent. The connector
// payload never carries "config" (encrypted at rest, never serialized).
func microvmConnectorMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/connectors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"connectors": map[string]any{
				"current_page": 1,
				"last_page":    1,
				"data": []any{
					map[string]any{"id": "con-1", "kind": "gitlab_runner", "name": "ci", "state": "ready"},
				},
			},
			"git_sources":           []any{map[string]any{"id": "gs-1", "name": "org app", "provider": "github_app", "installed": true}},
			"github_app_configured": true,
		})
	})

	mux.HandleFunc("GET /microvm/connectors/jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"jobs": map[string]any{
				"current_page": 1,
				"last_page":    1,
				"data": []any{
					map[string]any{"id": "job-1", "status": "completed", "minutes": 3},
				},
			},
		})
	})

	mux.HandleFunc("POST /microvm/connectors/github", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["git_source_id"] == "not-installed" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "not_installed",
				"errors":  map[string]any{"git_source_id": []string{"not_installed"}},
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"success":   true,
			"connector": map[string]any{"id": "con-new", "kind": "github_app", "state": "needs_plan", "echo": body},
		})
	})

	mux.HandleFunc("POST /microvm/connectors/gitlab", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success":   true,
			"connector": map[string]any{"id": "con-gl", "kind": "gitlab_runner", "state": "ready", "echo": body},
		})
	})

	mux.HandleFunc("PUT /microvm/connector/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["enabled"] == true && body["plan_id"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "needs_plan",
				"errors":  map[string]any{"enabled": []string{"needs_plan"}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":   true,
			"connector": map[string]any{"id": r.PathValue("id"), "state": "ready", "echo": body},
		})
	})

	mux.HandleFunc("DELETE /microvm/connector/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	return mux
}

func TestListMicrovmConnectors_ReturnsCreateContext(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.list", map[string]any{})
	var list tools.MicrovmConnectorListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Connectors[0]["id"] != "con-1" {
		t.Fatalf("connectors = %+v, want one row con-1", list)
	}
	if len(list.GitSources) != 1 || list.GitSources[0]["id"] != "gs-1" {
		t.Errorf("git_sources = %v, want one row gs-1", list.GitSources)
	}
	if !list.GithubAppConfigured {
		t.Errorf("github_app_configured = false, want true")
	}
}

func TestListMicrovmConnectorJobs_ReturnsRows(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.jobs", map[string]any{})
	var jobs tools.MicrovmConnectorJobListResult
	unmarshalResult(t, res, &jobs)
	if jobs.Count != 1 || jobs.Jobs[0]["id"] != "job-1" {
		t.Fatalf("jobs = %+v, want one row job-1", jobs)
	}
}

func TestCreateGithubConnector_BodyAndNotInstalled(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.create_github", map[string]any{
		"git_source_id":       "gs-1",
		"name":                "ci",
		"labels":              []string{"linux", "docker"},
		"hypervisor_group_id": "hg-1",
		"plan_id":             "plan-1",
		"max_concurrent":      4,
	})
	var out tools.MicrovmConnectorResult
	unmarshalResult(t, res, &out)
	if out.Connector["kind"] != "github_app" {
		t.Errorf("connector = %v, want kind github_app", out.Connector)
	}
	echo, _ := out.Connector["echo"].(map[string]any)
	if echo["git_source_id"] != "gs-1" || echo["name"] != "ci" || echo["max_concurrent"] != float64(4) {
		t.Errorf("body = %v, want git_source_id/name/max_concurrent passed through", echo)
	}
	labels, _ := echo["labels"].([]any)
	if len(labels) != 2 {
		t.Errorf("labels = %v, want 2", echo["labels"])
	}
	if _, present := echo["warm_count"]; present {
		t.Errorf("body must NOT carry warm_count when unset; sent %v", echo)
	}

	res = callTool(t, cs, "user.microvm.connector.create_github", map[string]any{
		"git_source_id": "not-installed", "name": "ci",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Errorf("not-installed source: want the 422, got %q", resultText(t, res))
	}
}

func TestCreateGitlabConnector_TokenNeverReturned(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.create_gitlab", map[string]any{
		"name":                "ci",
		"gitlab_url":          "https://gitlab.example.test",
		"gitlab_token":        "glrt-example-placeholder",
		"gitlab_tag_list":     []string{"microvm"},
		"gitlab_run_untagged": true,
		"hypervisor_group_id": "hg-1",
		"plan_id":             "plan-1",
	})
	var out tools.MicrovmConnectorResult
	unmarshalResult(t, res, &out)
	echo, _ := out.Connector["echo"].(map[string]any)
	if echo["gitlab_url"] != "https://gitlab.example.test" || echo["gitlab_run_untagged"] != true {
		t.Errorf("body = %v, want gitlab_url/gitlab_run_untagged passed through", echo)
	}
	if echo["gitlab_token"] != "glrt-example-placeholder" {
		t.Errorf("token must reach the API verbatim; body = %v", echo)
	}
	// The connector payload itself never carries the token or config.
	if _, present := out.Connector["config"]; present {
		t.Errorf("connector payload must never carry config: %v", out.Connector)
	}
	if _, present := out.Connector["gitlab_token"]; present {
		t.Errorf("connector payload must never carry gitlab_token: %v", out.Connector)
	}
}

func TestUpdateMicrovmConnector_SendsOnlySetFields(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	// Enabling without plan_id -> the mock answers 422 needs_plan, which the
	// framework maps to "validation failed".
	res := callTool(t, cs, "user.microvm.connector.update", map[string]any{"id": "con-1", "enabled": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Fatalf("enable without plan: want the 422, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.connector.update", map[string]any{
		"id": "con-1", "enabled": true, "plan_id": "plan-1", "warm_count": 0,
	})
	var out tools.MicrovmConnectorResult
	unmarshalResult(t, res, &out)
	echo, _ := out.Connector["echo"].(map[string]any)
	if echo["enabled"] != true || echo["plan_id"] != "plan-1" {
		t.Errorf("body = %v, want enabled/plan_id", echo)
	}
	if wc, present := echo["warm_count"]; !present || wc != float64(0) {
		t.Errorf("warm_count 0 must survive (pointer field); body = %v", echo)
	}
	for _, stray := range []string{"name", "labels", "hypervisor_group_id", "image_id", "max_concurrent"} {
		if _, present := echo[stray]; present {
			t.Errorf("update body must NOT carry %q when unset; sent %v", stray, echo)
		}
	}
}

func TestDeleteMicrovmConnector_RequiresConfirmation(t *testing.T) {
	cs := connectSession(t, microvmConnectorMock())

	res := callTool(t, cs, "user.microvm.connector.delete", map[string]any{"id": "con-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.connector.delete", map[string]any{"id": "con-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "con-1" {
		t.Errorf("delete result = %+v, want {con-1 true}", del)
	}
}
