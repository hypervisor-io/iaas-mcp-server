package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// mockAPI is an in-memory stand-in for the platform user API. It implements
// just the endpoints the golden tools call, with deterministic canned
// responses and a little state so the async waiters (deploy task -> completed,
// delete SHOW -> 404) actually converge across polls. No network beyond the
// local httptest server.
type mockAPI struct {
	mu sync.Mutex

	// taskPolls counts how many times each deploy task has been polled, so the
	// task reports "running" on the first poll and "completed" afterwards -
	// exercising the waiter's poll loop rather than converging on the very
	// first read.
	taskPolls map[string]int

	// deleting maps an instance id to a countdown; while > 0 a SHOW returns 200
	// (still deleting), and when it reaches 0 the SHOW 404s (converged).
	deleting map[string]int
}

func newMockAPI() *mockAPI {
	return &mockAPI{
		taskPolls: map[string]int{},
		deleting:  map[string]int{},
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (m *mockAPI) handler() http.Handler {
	mux := http.NewServeMux()

	// ── instances ────────────────────────────────────────────────────────────

	// CREATE (phase 1). location_id "invalid" forces a 422 for the error-mapping
	// test; otherwise records the row and returns it with an id.
	mux.HandleFunc("POST /cloud-service/instances", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["location_id"] == "invalid" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"plan_id": []string{"The selected plan id is invalid."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "created",
			"instance": map[string]any{
				"id":          "inst-1",
				"hostname":    body["hostname"],
				"plan_id":     body["plan_id"],
				"location_id": body["location_id"],
				"status":      "building",
				"deployed":    0,
			},
		})
	})

	// DEPLOY (phase 2): async, returns a top-level task_id.
	mux.HandleFunc("POST /instance/{id}/deploy", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "deploy queued",
			"task_id": "task-1",
		})
	})

	// TASK poll: "running" on first poll, "completed" thereafter.
	mux.HandleFunc("GET /instance/{id}/task/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("taskId")
		m.mu.Lock()
		m.taskPolls[taskID]++
		n := m.taskPolls[taskID]
		m.mu.Unlock()
		status := "completed"
		if n < 2 {
			status = "running"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"task": map[string]any{"id": taskID, "status": status, "progress": 100},
		})
	})

	// LIST: Laravel paginator envelope.
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1,
			"last_page":    1,
			"data": []any{
				map[string]any{"id": "inst-1", "hostname": "web", "status": "deployed"},
				map[string]any{"id": "inst-2", "hostname": "db", "status": "deployed"},
			},
		})
	})

	// SHOW (bare model). id "missing" 404s for the not-found test. An instance in
	// the deleting countdown 404s once the countdown expires.
	mux.HandleFunc("GET /instance/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Instance not found"})
			return
		}
		m.mu.Lock()
		if n, ok := m.deleting[id]; ok {
			m.deleting[id] = n - 1
			gone := n-1 <= 0
			m.mu.Unlock()
			if gone {
				writeJSON(w, http.StatusNotFound, map[string]any{"message": "Instance not found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "deleting", "deployed": 1})
			return
		}
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "hostname": "web", "status": "deployed", "deployed": 1,
		})
	})

	// DELETE: enqueue; SHOW converges to 404 after two more polls.
	mux.HandleFunc("DELETE /cloud-service/instances/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		m.mu.Lock()
		m.deleting[id] = 2
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "delete queued"})
	})

	// ── vpcs ─────────────────────────────────────────────────────────────────

	mux.HandleFunc("POST /vpcs", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"vpc": map[string]any{
				"id":         "vpc-1",
				"name":       body["name"],
				"cidr":       body["cidr"],
				"vni_number": 5001,
			},
		})
	})

	mux.HandleFunc("GET /vpcs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1,
			"last_page":    1,
			"data": []any{
				map[string]any{"id": "vpc-1", "name": "prod", "vni_number": 5001},
			},
		})
	})

	// SHOW (create-readback): full object with subnets.
	mux.HandleFunc("GET /vpc/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"vpc": map[string]any{
				"id": id, "name": "prod", "cidr": "10.0.0.0/24", "vni_number": 5001,
				"subnets": []any{map[string]any{"id": "sub-1", "cidr": "10.0.0.0/26"}},
			},
		})
	})

	// ── vpc attachment ───────────────────────────────────────────────────────

	mux.HandleFunc("POST /instance/{id}/vpc/enable", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "vpc enabled"})
	})

	mux.HandleFunc("GET /instance/{id}/vpc/ips", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1,
			"last_page":    1,
			"data": []any{
				map[string]any{"id": "vip-1", "ip": "10.0.0.2", "is_primary": true, "status": "used"},
			},
		})
	})

	return mux
}

// newSession stands up the mock API, registers the golden tools on an MCP
// server whose TokenSource returns a mock-backed client, and connects an
// in-process MCP client. It returns the client session for driving tool calls.
func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	return connectSession(t, newMockAPI().handler())
}

// connectSession is the reusable test harness every tool-family test uses: it
// stands up the given handler as the mock IaaS API, registers ALL tool
// families (RegisterAll) on a fresh MCP server whose TokenSource points at that
// mock, and connects an in-process MCP client. Family tests pass their own mux
// so each family exercises only the endpoints it needs.
func connectSession(t *testing.T, handler http.Handler) *mcp.ClientSession {
	t.Helper()

	// Fast polling so any create/delete waiters converge instantly.
	t.Setenv("IAAS_MCP_POLL_INTERVAL", "1ms")

	mock := httptest.NewServer(handler)
	t.Cleanup(mock.Close)

	deps := tools.Deps{
		TokenSource: func(_ context.Context) (*client.Client, error) {
			return client.New(mock.URL, "test-token", 2*time.Second, false), nil
		},
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "iaas-mcp-server", Version: "test"}, nil)
	tools.RegisterAll(server, deps)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	cs, err := c.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

// callTool invokes a tool and returns the result. Transport-level errors fail
// the test; tool-execution errors surface via result.IsError.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) transport error: %v", name, err)
	}
	return res
}

// resultText returns the first TextContent block, which for successful tools is
// the JSON-encoded structured output and for errors is the error message.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("result content[0] is %T, want *TextContent", res.Content[0])
	}
	return tc.Text
}

// unmarshalResult decodes a successful tool's JSON output into v.
func unmarshalResult(t *testing.T, res *mcp.CallToolResult, v any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned an error result: %s", resultText(t, res))
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), v); err != nil {
		t.Fatalf("unmarshalling result %q: %v", resultText(t, res), err)
	}
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestListTools_AllGoldenRegistered(t *testing.T) {
	cs := newSession(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{
		"user.instance.create", "user.instance.list", "user.instance.get", "user.instance.delete",
		"user.vpc.create", "user.vpc.list", "user.vpc.get", "user.vpc.attach_instance",
	} {
		if !got[want] {
			t.Errorf("tool %q not registered (have: %v)", want, got)
		}
	}
}

func TestInstanceCreate_ConvergesToDeployed(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.instance.create", map[string]any{
		"location_id": "loc-1", "plan_id": "plan-1", "image_id": "img-1", "hostname": "web",
	})
	var out tools.InstanceResult
	unmarshalResult(t, res, &out)

	if out.Instance["id"] != "inst-1" {
		t.Errorf("instance id = %v, want inst-1", out.Instance["id"])
	}
	// The converged SHOW reports deployed - proving the waiter ran to completion
	// before the result was hydrated.
	if out.Instance["status"] != "deployed" {
		t.Errorf("instance status = %v, want deployed", out.Instance["status"])
	}
}

func TestInstanceList_ReturnsAll(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.instance.list", map[string]any{})
	var out tools.InstanceListResult
	unmarshalResult(t, res, &out)
	if out.Count != 2 || len(out.Instances) != 2 {
		t.Fatalf("count = %d / len = %d, want 2/2", out.Count, len(out.Instances))
	}
	if out.Instances[0]["id"] != "inst-1" {
		t.Errorf("instances[0].id = %v, want inst-1", out.Instances[0]["id"])
	}
}

func TestInstanceGet_ReturnsBareModel(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.instance.get", map[string]any{"id": "inst-9"})
	var out tools.InstanceResult
	unmarshalResult(t, res, &out)
	if out.Instance["id"] != "inst-9" {
		t.Errorf("instance id = %v, want inst-9", out.Instance["id"])
	}
}

func TestInstanceDelete_RefusesWithoutConfirm(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.instance.delete", map[string]any{"id": "inst-1"})
	if !res.IsError {
		t.Fatalf("delete without confirm should be an error result")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "confirm") {
		t.Errorf("refusal message = %q, want it to mention confirm", msg)
	}
}

func TestInstanceDelete_SucceedsWithConfirm(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.instance.delete", map[string]any{"id": "inst-1", "confirm": true})
	var out tools.DeleteResult
	unmarshalResult(t, res, &out)
	if !out.Deleted || out.ID != "inst-1" {
		t.Errorf("delete result = %+v, want {inst-1 true}", out)
	}
}

func TestErrorMapping_422FieldErrors(t *testing.T) {
	cs := newSession(t)
	// location_id "invalid" makes the mock create endpoint return 422 with field errors.
	res := callTool(t, cs, "user.instance.create", map[string]any{
		"location_id": "invalid", "plan_id": "plan-1", "image_id": "img-1",
	})
	if !res.IsError {
		t.Fatalf("expected an error result for 422")
	}
	msg := resultText(t, res)
	if !strings.Contains(msg, "validation failed") {
		t.Errorf("422 message = %q, want it to contain 'validation failed'", msg)
	}
	if !strings.Contains(msg, "plan_id") {
		t.Errorf("422 message = %q, want it to include the field name plan_id", msg)
	}
}

func TestErrorMapping_404NotFound(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.instance.get", map[string]any{"id": "missing"})
	if !res.IsError {
		t.Fatalf("expected an error result for 404")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "not found") {
		t.Errorf("404 message = %q, want it to contain 'not found'", msg)
	}
}

func TestVPCCreate_ReadsBackFullObject(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.vpc.create", map[string]any{
		"name": "prod", "cidr": "10.0.0.0/24", "hypervisor_group_id": "hg-1",
	})
	var out tools.VPCResult
	unmarshalResult(t, res, &out)
	if out.VPC["id"] != "vpc-1" {
		t.Errorf("vpc id = %v, want vpc-1", out.VPC["id"])
	}
	// The readback (SHOW) carries subnets that the create envelope does not.
	if _, ok := out.VPC["subnets"]; !ok {
		t.Errorf("vpc result missing subnets (readback did not run): %v", out.VPC)
	}
}

func TestVPCList_ReturnsAll(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.vpc.list", map[string]any{})
	var out tools.VPCListResult
	unmarshalResult(t, res, &out)
	if out.Count != 1 || len(out.VPCs) != 1 {
		t.Fatalf("count = %d / len = %d, want 1/1", out.Count, len(out.VPCs))
	}
}

func TestVPCAttachInstance_ReadsBackIPs(t *testing.T) {
	cs := newSession(t)
	res := callTool(t, cs, "user.vpc.attach_instance", map[string]any{
		"instance_id": "inst-1", "vpc_id": "vpc-1", "vpc_subnet_id": "sub-1",
	})
	var out tools.AttachResult
	unmarshalResult(t, res, &out)
	if !out.Attached {
		t.Errorf("attached = false, want true")
	}
	if len(out.IPs) != 1 || out.IPs[0]["ip"] != "10.0.0.2" {
		t.Errorf("ips = %v, want one row with ip 10.0.0.2", out.IPs)
	}
}
