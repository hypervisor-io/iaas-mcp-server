package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// runnerMockAPI mocks the MV3-23/MV3-24 CI runner endpoints. Pools LIST and
// jobs LIST answer with Laravel paginator envelopes; gitlab create and both
// provider updates answer with the bare CiRunnerPool model (the shape the real
// controllers return); DELETE answers {success}. Write bodies are RECORDED
// server-side instead of echoed into the response: gitlab_token must never
// appear in a tool result, so the tests read what the tool SENT from the
// recorder and separately assert the result text stays token-free.
type runnerMockAPI struct {
	mu sync.Mutex

	createBody map[string]any
	updatePath string
	updateBody map[string]any
}

func (m *runnerMockAPI) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/runners/pools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1,
			"last_page":    1,
			"data": []any{
				map[string]any{"id": "pool-gh-1", "provider": "github", "enabled": true, "max_concurrent": 3},
				map[string]any{"id": "pool-gl-1", "provider": "gitlab", "gitlab_url": "https://gitlab.example.com", "max_concurrent": 2},
			},
		})
	})

	mux.HandleFunc("POST /microvm/runners/pools/gitlab", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.createBody = body
		m.mu.Unlock()
		// A realistic bare CiRunnerPool: the model never carries the token, so
		// only non-sensitive request fields are reflected back.
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                  "pool-gl-new",
			"provider":            "gitlab",
			"gitlab_url":          body["gitlab_url"],
			"hypervisor_group_id": body["hypervisor_group_id"],
			"plan_id":             body["plan_id"],
			"warm_count":          body["warm_count"],
			"max_concurrent":      body["max_concurrent"],
			"enabled":             true,
		})
	})

	// The {provider} path segment is part of the route: a PUT without it (or
	// with a wrong one) matches no pattern and 404s, which the update test
	// surfaces as a RED call.
	mux.HandleFunc("PUT /microvm/runners/pools/{provider}/{id}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.updatePath = r.URL.Path
		m.updateBody = body
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       r.PathValue("id"),
			"provider": r.PathValue("provider"),
			"enabled":  true,
		})
	})

	mux.HandleFunc("DELETE /microvm/runners/pools/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "refused" {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"message": "Cannot delete a runner pool with an active job. Wait for it to finish.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	mux.HandleFunc("GET /microvm/runners/jobs", func(w http.ResponseWriter, r *http.Request) {
		poolID := r.URL.Query().Get("pool_id")
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1,
			"last_page":    1,
			"data": []any{
				map[string]any{"id": "job-1", "status": "succeeded", "queried_pool_id": poolID},
				map[string]any{"id": "job-2", "status": "running", "queried_pool_id": poolID},
			},
		})
	})

	return mux
}

func (m *runnerMockAPI) sentCreateBody() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createBody
}

func (m *runnerMockAPI) sentUpdatePath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePath
}

func (m *runnerMockAPI) sentUpdateBody() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateBody
}

func TestListRunnerPools_ReturnsCountAndItems(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.runner.pool.list", map[string]any{})
	var list tools.RunnerPoolListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("expected 2 pools, got %d", list.Count)
	}
	if list.Pools[0]["id"] != "pool-gh-1" {
		t.Errorf("pools[0].id = %v, want pool-gh-1", list.Pools[0]["id"])
	}
	if list.Pools[1]["provider"] != "gitlab" {
		t.Errorf("pools[1].provider = %v, want gitlab", list.Pools[1]["provider"])
	}
}

func TestCreateGitlabRunnerPool_SendsTokenAndNeverEchoesIt(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	// Required-only call: gitlab_token MUST reach the API in the request body
	// (the API verifies it with GitLab) and MUST NOT appear in the result.
	res := callTool(t, cs, "user.microvm.runner.pool.create_gitlab", map[string]any{
		"gitlab_url":          "https://gitlab.example.com",
		"gitlab_token":        "glrt-secret-123",
		"hypervisor_group_id": "hg-1",
		"plan_id":             "plan-1",
		"warm_count":          2,
	})
	var out tools.RunnerPoolResult
	unmarshalResult(t, res, &out)
	if out.Pool["id"] != "pool-gl-new" {
		t.Errorf("create result id = %v, want pool-gl-new", out.Pool["id"])
	}

	body := mock.sentCreateBody()
	if body == nil {
		t.Fatal("create never reached POST /microvm/runners/pools/gitlab")
	}
	if body["gitlab_token"] != "glrt-secret-123" {
		t.Errorf("create body gitlab_token = %v, want the token to be sent to the API", body["gitlab_token"])
	}
	if body["gitlab_url"] != "https://gitlab.example.com" || body["hypervisor_group_id"] != "hg-1" || body["plan_id"] != "plan-1" {
		t.Errorf("create body = %v, want it to carry the required fields", body)
	}
	if body["warm_count"] != float64(2) {
		t.Errorf("create body warm_count = %v, want 2", body["warm_count"])
	}
	for _, stray := range []string{"gitlab_tag_list", "gitlab_run_untagged", "max_concurrent"} {
		if _, present := body[stray]; present {
			t.Errorf("create body must NOT carry %q when unset; sent %v", stray, body)
		}
	}
	if strings.Contains(resultText(t, res), "glrt-secret-123") {
		t.Errorf("create result echoes gitlab_token: %q", resultText(t, res))
	}

	// warm_count 0 is a legal, required value and must be sent; set optionals
	// pass through verbatim.
	res = callTool(t, cs, "user.microvm.runner.pool.create_gitlab", map[string]any{
		"gitlab_url":          "https://gitlab.example.com",
		"gitlab_token":        "glrt-secret-456",
		"hypervisor_group_id": "hg-1",
		"plan_id":             "plan-1",
		"warm_count":          0,
		"gitlab_tag_list":     []any{"linux", "docker"},
		"gitlab_run_untagged": true,
		"max_concurrent":      4,
	})
	out = tools.RunnerPoolResult{}
	unmarshalResult(t, res, &out)
	body = mock.sentCreateBody()
	if body["gitlab_token"] != "glrt-secret-456" {
		t.Errorf("create body gitlab_token = %v, want the token to be sent to the API", body["gitlab_token"])
	}
	if body["warm_count"] != float64(0) {
		t.Errorf("create body warm_count = %v, want 0 (a legal value that must be sent)", body["warm_count"])
	}
	tags, _ := body["gitlab_tag_list"].([]any)
	if len(tags) != 2 || tags[0] != "linux" || tags[1] != "docker" {
		t.Errorf("create body gitlab_tag_list = %v, want [linux docker]", body["gitlab_tag_list"])
	}
	if body["gitlab_run_untagged"] != true {
		t.Errorf("create body gitlab_run_untagged = %v, want true", body["gitlab_run_untagged"])
	}
	if body["max_concurrent"] != float64(4) {
		t.Errorf("create body max_concurrent = %v, want 4", body["max_concurrent"])
	}
	if strings.Contains(resultText(t, res), "glrt-secret-456") {
		t.Errorf("create result echoes gitlab_token: %q", resultText(t, res))
	}
}

func TestUpdateRunnerPool_PathCarriesProviderSegment(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	// A gitlab update: the PUT must hit /microvm/runners/pools/gitlab/{id},
	// carry only the fields the caller set (plus the rotation token), and the
	// result must not echo the token.
	res := callTool(t, cs, "user.microvm.runner.pool.update", map[string]any{
		"provider":     "gitlab",
		"id":           "pool-gl-1",
		"gitlab_url":   "https://gitlab.example.org",
		"gitlab_token": "glrt-rotated-789",
		"warm_count":   3,
	})
	var out tools.RunnerPoolResult
	unmarshalResult(t, res, &out)
	if out.Pool["id"] != "pool-gl-1" || out.Pool["provider"] != "gitlab" {
		t.Fatalf("update result = %v, want pool-gl-1/gitlab", out.Pool)
	}
	if got := mock.sentUpdatePath(); got != "/microvm/runners/pools/gitlab/pool-gl-1" {
		t.Errorf("update path = %q, want /microvm/runners/pools/gitlab/pool-gl-1", got)
	}
	body := mock.sentUpdateBody()
	if body["gitlab_url"] != "https://gitlab.example.org" {
		t.Errorf("update body gitlab_url = %v, want https://gitlab.example.org", body["gitlab_url"])
	}
	if body["gitlab_token"] != "glrt-rotated-789" {
		t.Errorf("update body gitlab_token = %v, want the rotation token to be sent to the API", body["gitlab_token"])
	}
	if body["warm_count"] != float64(3) {
		t.Errorf("update body warm_count = %v, want 3", body["warm_count"])
	}
	for _, stray := range []string{"gitlab_tag_list", "gitlab_run_untagged", "hypervisor_group_id", "plan_id", "max_concurrent", "labels", "enabled"} {
		if _, present := body[stray]; present {
			t.Errorf("update body must NOT carry %q when unset; sent %v", stray, body)
		}
	}
	if strings.Contains(resultText(t, res), "glrt-rotated-789") {
		t.Errorf("update result echoes gitlab_token: %q", resultText(t, res))
	}

	// A github update: same tool, github path segment, github-only fields.
	// enabled:false is a legal set value and must be sent.
	res = callTool(t, cs, "user.microvm.runner.pool.update", map[string]any{
		"provider":       "github",
		"id":             "pool-gh-1",
		"labels":         []any{"linux"},
		"enabled":        false,
		"max_concurrent": 5,
	})
	out = tools.RunnerPoolResult{}
	unmarshalResult(t, res, &out)
	if out.Pool["provider"] != "github" {
		t.Fatalf("update result provider = %v, want github", out.Pool["provider"])
	}
	if got := mock.sentUpdatePath(); got != "/microvm/runners/pools/github/pool-gh-1" {
		t.Errorf("update path = %q, want /microvm/runners/pools/github/pool-gh-1", got)
	}
	body = mock.sentUpdateBody()
	labels, _ := body["labels"].([]any)
	if len(labels) != 1 || labels[0] != "linux" {
		t.Errorf("update body labels = %v, want [linux]", body["labels"])
	}
	if body["enabled"] != false {
		t.Errorf("update body enabled = %v, want false (a legal value that must be sent)", body["enabled"])
	}
	if body["max_concurrent"] != float64(5) {
		t.Errorf("update body max_concurrent = %v, want 5", body["max_concurrent"])
	}
}

func TestDeleteRunnerPool_RequiresConfirmation(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	// Without confirm the framework refuses before any HTTP call; with
	// confirm:true the delete goes through (the same two-part proof
	// TestKillSandbox_RequiresConfirmation uses for KillSandboxInput).
	res := callTool(t, cs, "user.microvm.runner.pool.delete", map[string]any{"id": "pool-gl-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.runner.pool.delete", map[string]any{"id": "pool-gl-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "pool-gl-1" {
		t.Errorf("delete result = %+v, want {pool-gl-1 true}", del)
	}
}

func TestDeleteRunnerPool_ClientErrorIsSurfaced(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	// "refused" makes the mock answer success:false at HTTP 200 (a pool with an
	// active job); the tool must surface that as an error result, never as
	// Deleted:true.
	res := callTool(t, cs, "user.microvm.runner.pool.delete", map[string]any{"id": "refused", "confirm": true})
	if !res.IsError {
		t.Fatalf("delete of a refused pool must be an error result, got %q", resultText(t, res))
	}
}

func TestListRunnerJobs_PoolFilterReachesAPI(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.runner.job.list", map[string]any{})
	var list tools.RunnerJobListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("expected 2 jobs, got %d", list.Count)
	}
	if list.Jobs[0]["id"] != "job-1" {
		t.Errorf("jobs[0].id = %v, want job-1", list.Jobs[0]["id"])
	}

	// The pool filter reaches the API as a ?pool_id= query parameter.
	res = callTool(t, cs, "user.microvm.runner.job.list", map[string]any{"pool_id": "pool-gl-1"})
	list = tools.RunnerJobListResult{}
	unmarshalResult(t, res, &list)
	if list.Jobs[0]["queried_pool_id"] != "pool-gl-1" {
		t.Errorf("pool_id filter = %v, want it to reach the API as ?pool_id=pool-gl-1", list.Jobs[0]["queried_pool_id"])
	}
}

// TestRunnerPoolCreateGitlabSchema_TokenRequiredString is the merged-shape
// twin of the section's schema assertion: on the client side ListTools
// delivers the SDK-generated input schema as map[string]any, so gitlab_token
// is asserted to be a required string property there.
func TestRunnerPoolCreateGitlabSchema_TokenRequiredString(t *testing.T) {
	mock := &runnerMockAPI{}
	cs := connectSession(t, mock.handler())

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var tool *mcp.Tool
	for _, tl := range res.Tools {
		if tl.Name == "user.microvm.runner.pool.create_gitlab" {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatal("user.microvm.runner.pool.create_gitlab is not registered")
	}

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema = %T, want map[string]any", tool.InputSchema)
	}
	props, _ := schema["properties"].(map[string]any)
	tokenProp, ok := props["gitlab_token"].(map[string]any)
	if !ok {
		t.Fatal("expected gitlab_token in the tool's input schema properties")
	}
	if tokenProp["type"] != "string" {
		t.Errorf("gitlab_token type = %v, want string", tokenProp["type"])
	}
	required, _ := schema["required"].([]any)
	found := false
	for _, r := range required {
		if r == "gitlab_token" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gitlab_token must be required (required = %v)", required)
	}
}
