package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// sandboxMock mocks the MV1-25 sandbox endpoints. CREATE echoes the received
// request body back under the returned sandbox's "echo" key so the tests can
// pin exactly which optional fields the tool sent; LIST echoes the ?state=
// query parameter into each item.
func sandboxMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"sandboxes": []any{
				map[string]any{"id": "sb-1", "state": "running", "queried_state": state},
				map[string]any{"id": "sb-2", "state": "paused", "queried_state": state},
			},
		})
	})

	mux.HandleFunc("GET /microvm/sandbox/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Sandbox not found."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"sandbox": map[string]any{"id": id, "state": "running"},
		})
	})

	mux.HandleFunc("POST /microvm/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"sandbox": map[string]any{"id": "sb-new", "state": "running", "echo": body},
		})
	})

	mux.HandleFunc("POST /microvm/sandbox/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"sandbox": map[string]any{"id": r.PathValue("id"), "state": "paused"},
		})
	})

	mux.HandleFunc("POST /microvm/sandbox/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"sandbox": map[string]any{"id": r.PathValue("id"), "state": "running"},
		})
	})

	mux.HandleFunc("POST /microvm/sandbox/{id}/timeout", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"sandbox": map[string]any{"id": r.PathValue("id"), "state": "running", "timeout": body["timeout"]},
		})
	})

	mux.HandleFunc("DELETE /microvm/sandbox/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "refused" {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"message": "This sandbox cannot be killed right now.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Sandbox killed."})
	})

	mux.HandleFunc("GET /microvm/sandbox/{id}/metrics", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"metrics": map[string]any{"cpu_percent": 12.5, "memory_mb": 256, "disk_mb": 1024},
		})
	})

	mux.HandleFunc("GET /microvm/sandbox/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"logs": []any{
				map[string]any{"line": "booting"},
				map[string]any{"line": "ready"},
			},
		})
	})

	return mux
}

func TestListSandboxes_ReturnsCountAndItems(t *testing.T) {
	cs := connectSession(t, sandboxMock())

	res := callTool(t, cs, "user.microvm.sandbox.list", map[string]any{})
	var list tools.SandboxListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("expected 2 sandboxes, got %d", list.Count)
	}
	if list.Sandboxes[0]["id"] != "sb-1" {
		t.Errorf("sandboxes[0].id = %v, want sb-1", list.Sandboxes[0]["id"])
	}

	// The state filter reaches the API as a ?state= query parameter.
	res = callTool(t, cs, "user.microvm.sandbox.list", map[string]any{"state": "running"})
	list = tools.SandboxListResult{}
	unmarshalResult(t, res, &list)
	if list.Sandboxes[0]["queried_state"] != "running" {
		t.Errorf("state filter = %v, want it to reach the API as ?state=running", list.Sandboxes[0]["queried_state"])
	}
}

func TestGetSandbox_ReturnsSandbox(t *testing.T) {
	cs := connectSession(t, sandboxMock())

	res := callTool(t, cs, "user.microvm.sandbox.get", map[string]any{"id": "sb-1"})
	var got tools.SandboxResult
	unmarshalResult(t, res, &got)
	if got.Sandbox["id"] != "sb-1" {
		t.Errorf("get id = %v, want sb-1", got.Sandbox["id"])
	}

	res = callTool(t, cs, "user.microvm.sandbox.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}
}

func TestCreateSandbox_BodyCarriesOnlySetFields(t *testing.T) {
	cs := connectSession(t, sandboxMock())

	// Unset optional fields must not appear in the request body at all; the
	// API applies its own defaults (timeout 15, on_timeout pause).
	res := callTool(t, cs, "user.microvm.sandbox.create", map[string]any{
		"hypervisor_group_id": "hg-1",
		"template":            "ubuntu-24.04",
	})
	var out tools.SandboxResult
	unmarshalResult(t, res, &out)
	echo, ok := out.Sandbox["echo"].(map[string]any)
	if !ok {
		t.Fatalf("create result missing the echoed request body: %v", out.Sandbox)
	}
	if echo["hypervisor_group_id"] != "hg-1" || echo["template"] != "ubuntu-24.04" {
		t.Errorf("create body = %v, want it to carry the required fields", echo)
	}
	for _, stray := range []string{"timeout", "metadata", "env_vars", "secure", "on_timeout"} {
		if _, present := echo[stray]; present {
			t.Errorf("create body must NOT carry %q when unset; sent %v", stray, echo)
		}
	}

	// Set optional fields are passed through verbatim.
	res = callTool(t, cs, "user.microvm.sandbox.create", map[string]any{
		"hypervisor_group_id": "hg-1",
		"template":            "ubuntu-24.04",
		"timeout":             30,
		"on_timeout":          "kill",
		"secure":              true,
		"metadata":            map[string]any{"team": "ci"},
		"env_vars":            map[string]any{"FOO": "bar"},
	})
	out = tools.SandboxResult{}
	unmarshalResult(t, res, &out)
	echo, ok = out.Sandbox["echo"].(map[string]any)
	if !ok {
		t.Fatalf("create result missing the echoed request body: %v", out.Sandbox)
	}
	if echo["timeout"] != float64(30) {
		t.Errorf("timeout = %v, want 30", echo["timeout"])
	}
	if echo["on_timeout"] != "kill" {
		t.Errorf("on_timeout = %v, want kill", echo["on_timeout"])
	}
	if echo["secure"] != true {
		t.Errorf("secure = %v, want true", echo["secure"])
	}
	metadata, _ := echo["metadata"].(map[string]any)
	if metadata["team"] != "ci" {
		t.Errorf("metadata = %v, want team=ci", echo["metadata"])
	}
	envVars, _ := echo["env_vars"].(map[string]any)
	if envVars["FOO"] != "bar" {
		t.Errorf("env_vars = %v, want FOO=bar", echo["env_vars"])
	}
}

func TestSandboxLifecycle_PauseResumeTimeoutMetricsLogs(t *testing.T) {
	cs := connectSession(t, sandboxMock())

	res := callTool(t, cs, "user.microvm.sandbox.pause", map[string]any{"id": "sb-1"})
	var out tools.SandboxResult
	unmarshalResult(t, res, &out)
	if out.Sandbox["state"] != "paused" {
		t.Errorf("pause state = %v, want paused", out.Sandbox["state"])
	}

	res = callTool(t, cs, "user.microvm.sandbox.resume", map[string]any{"id": "sb-1"})
	out = tools.SandboxResult{}
	unmarshalResult(t, res, &out)
	if out.Sandbox["state"] != "running" {
		t.Errorf("resume state = %v, want running", out.Sandbox["state"])
	}

	res = callTool(t, cs, "user.microvm.sandbox.set_timeout", map[string]any{"id": "sb-1", "timeout": 60})
	out = tools.SandboxResult{}
	unmarshalResult(t, res, &out)
	if out.Sandbox["timeout"] != float64(60) {
		t.Errorf("set_timeout echoed timeout = %v, want 60", out.Sandbox["timeout"])
	}

	res = callTool(t, cs, "user.microvm.sandbox.metrics", map[string]any{"id": "sb-1"})
	var metrics map[string]any
	unmarshalResult(t, res, &metrics)
	if metrics["cpu_percent"] != 12.5 {
		t.Errorf("metrics cpu_percent = %v, want 12.5", metrics["cpu_percent"])
	}

	res = callTool(t, cs, "user.microvm.sandbox.logs", map[string]any{"id": "sb-1"})
	var logs tools.SandboxLogsResult
	unmarshalResult(t, res, &logs)
	if len(logs.Logs) != 2 {
		t.Errorf("logs = %v, want 2 lines", logs.Logs)
	}
}

func TestKillSandbox_RequiresConfirmation(t *testing.T) {
	cs := connectSession(t, sandboxMock())

	// Without confirm the framework refuses before any HTTP call; with
	// confirm:true the kill goes through (the same two-part proof
	// TestCertificate_DeleteConfirmGate uses for DeleteCertificateInput).
	res := callTool(t, cs, "user.microvm.sandbox.kill", map[string]any{"id": "sb-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("kill without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.sandbox.kill", map[string]any{"id": "sb-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "sb-1" {
		t.Errorf("kill result = %+v, want {sb-1 true}", del)
	}
}

func TestKillSandbox_ClientErrorIsSurfaced(t *testing.T) {
	cs := connectSession(t, sandboxMock())

	// "refused" makes the mock answer success:false at HTTP 200; the tool must
	// surface that as an error result, never as Deleted:true.
	res := callTool(t, cs, "user.microvm.sandbox.kill", map[string]any{"id": "refused", "confirm": true})
	if !res.IsError {
		t.Fatalf("kill of a refused sandbox must be an error result, got %q", resultText(t, res))
	}
}
