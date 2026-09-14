package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// microvmMock mocks the MVU2-2 MicroVM endpoints. CREATE echoes the received
// request body back under the returned microvm's "echo" key so the tests can
// pin exactly which optional fields the tool sent; INDEX echoes the query
// parameters into each item; the id "illegal" makes every state verb answer
// 409 (illegal transition).
func microvmMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"microvms": map[string]any{
				"current_page": 1,
				"last_page":    1,
				"data": []any{
					map[string]any{
						"id": "vm-1", "name": "web", "state": "running",
						"queried_state": q.Get("state"), "queried_image_id": q.Get("image_id"),
						"queried_connector_id": q.Get("connector_id"), "queried_search": q.Get("search"),
					},
				},
			},
		})
	})

	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"microvm": map[string]any{"id": "vm-new", "name": body["name"], "state": "creating", "echo": body},
		})
	})

	mux.HandleFunc("GET /microvm/vm/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "MicroVM not found."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"microvm":  map[string]any{"id": id, "name": "web", "state": "running"},
			"runs":     []any{map[string]any{"id": "run-1", "status": "running"}},
			"versions": []any{map[string]any{"id": "v-2", "version": 2}},
			"domains":  []any{map[string]any{"id": "dom-1", "hostname": "web.example.test"}},
			"env_keys": []any{"FOO"},
		})
	})

	for _, verb := range []string{"pause", "resume", "stop", "start", "kill"} {
		mux.HandleFunc("POST /microvm/vm/{id}/"+verb, func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("id") == "illegal" {
				writeJSON(w, http.StatusConflict, map[string]any{
					"success": false, "message": "Cannot " + verb + " a microvm in state [killed].",
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
		})
	}

	mux.HandleFunc("POST /microvm/vm/{id}/timeout", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["timeout"] != float64(60) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"timeout": []string{"The timeout field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	for _, verb := range []string{"deploy", "rollback"} {
		mux.HandleFunc("POST /microvm/vm/{id}/"+verb, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, http.StatusOK, map[string]any{
				"success": true,
				"run":     map[string]any{"id": "run-2", "status": "creating", "echo": body},
			})
		})
	}

	mux.HandleFunc("PUT /microvm/vm/{id}/env", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env, _ := body["env"].(map[string]any)
		keys := make([]any, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "keys": keys})
	})

	mux.HandleFunc("POST /microvm/vm/{id}/domain", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["hostname"] == "taken.example.test" {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "duplicate_hostname"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"domain":  map[string]any{"id": "dom-2", "hostname": body["hostname"], "cert_status": "pending"},
		})
	})

	mux.HandleFunc("DELETE /microvm/vm/{id}/domain/{domainId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	mux.HandleFunc("GET /microvm/vm/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"lines": []any{
				map[string]any{"line": "booting", "queried_limit": r.URL.Query().Get("limit")},
				map[string]any{"line": "ready"},
			},
		})
	})

	mux.HandleFunc("GET /microvm/vm/{id}/metrics", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"metrics": map[string]any{"cpu_pct": 12.5, "mem_used_mib": 256, "series": []any{}},
		})
	})

	mux.HandleFunc("DELETE /microvm/vm/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "MicroVM deleted."})
	})

	return mux
}

func TestListMicrovms_FiltersReachQuery(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.list", map[string]any{
		"state": "running", "image_id": "img-1", "connector_id": "con-1",
	})
	var list tools.MicrovmListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Microvms[0]["id"] != "vm-1" {
		t.Fatalf("list = %+v, want one row vm-1", list)
	}
	first := list.Microvms[0]
	if first["queried_state"] != "running" || first["queried_image_id"] != "img-1" || first["queried_connector_id"] != "con-1" {
		t.Errorf("filters did not reach the query string: %v", first)
	}
	if first["queried_search"] != "" {
		t.Errorf("empty search must not reach the query, got %v", first["queried_search"])
	}
}

func TestGetMicrovm_ReturnsShowEnvelope(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.get", map[string]any{"id": "vm-1"})
	var got tools.MicrovmShowResult
	unmarshalResult(t, res, &got)
	if got.Microvm["id"] != "vm-1" {
		t.Errorf("microvm id = %v, want vm-1", got.Microvm["id"])
	}
	if len(got.Runs) != 1 || got.Runs[0]["id"] != "run-1" {
		t.Errorf("runs = %v, want one row run-1", got.Runs)
	}
	if len(got.Versions) != 1 || len(got.Domains) != 1 {
		t.Errorf("versions/domains = %v/%v, want one each", got.Versions, got.Domains)
	}
	if len(got.EnvKeys) != 1 || got.EnvKeys[0] != "FOO" {
		t.Errorf("env_keys = %v, want [FOO]", got.EnvKeys)
	}

	res = callTool(t, cs, "user.microvm.vm.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}
}

func TestCreateMicrovm_BodyCarriesOnlySetFields(t *testing.T) {
	cs := connectSession(t, microvmMock())

	// Unset optional fields must not appear in the request body at all; the
	// API applies its own defaults.
	res := callTool(t, cs, "user.microvm.vm.create", map[string]any{
		"hypervisor_group_id": "hg-1",
		"image_id":            "img-1",
		"name":                "web",
	})
	var out tools.MicrovmResult
	unmarshalResult(t, res, &out)
	echo, ok := out.Microvm["echo"].(map[string]any)
	if !ok {
		t.Fatalf("create result missing the echoed request body: %v", out.Microvm)
	}
	if echo["hypervisor_group_id"] != "hg-1" || echo["image_id"] != "img-1" || echo["name"] != "web" {
		t.Errorf("create body = %v, want it to carry the required fields", echo)
	}
	for _, stray := range []string{"image_version_id", "plan_id", "ingress", "network", "max_lifetime_seconds",
		"on_timeout", "idle_timeout_seconds", "always_on", "env", "lifecycle_hooks", "domain", "secure"} {
		if _, present := echo[stray]; present {
			t.Errorf("create body must NOT carry %q when unset; sent %v", stray, echo)
		}
	}

	// Set optional fields are passed through verbatim, nested objects intact.
	res = callTool(t, cs, "user.microvm.vm.create", map[string]any{
		"hypervisor_group_id":  "hg-1",
		"image_id":             "img-1",
		"name":                 "web",
		"plan_id":              "plan-1",
		"ingress":              map[string]any{"http": map[string]any{"enabled": true, "port": 8080}, "shell": map[string]any{"enabled": true}},
		"network":              []any{map[string]any{"kind": "isolated"}},
		"max_lifetime_seconds": 3600,
		"on_timeout":           "kill",
		"always_on":            true,
		"secure":               false,
		"lifecycle_hooks":      map[string]any{"run": map[string]any{"enabled": true, "timeout": 30}},
	})
	out = tools.MicrovmResult{}
	unmarshalResult(t, res, &out)
	echo, _ = out.Microvm["echo"].(map[string]any)
	if echo["plan_id"] != "plan-1" || echo["on_timeout"] != "kill" || echo["max_lifetime_seconds"] != float64(3600) {
		t.Errorf("create body = %v, want plan_id/on_timeout/max_lifetime_seconds passed through", echo)
	}
	if echo["always_on"] != true || echo["secure"] != false {
		t.Errorf("always_on/secure = %v/%v, want true/false (explicit false must survive)", echo["always_on"], echo["secure"])
	}
	ingress, _ := echo["ingress"].(map[string]any)
	httpIngress, _ := ingress["http"].(map[string]any)
	if httpIngress["port"] != float64(8080) {
		t.Errorf("ingress.http.port = %v, want 8080", ingress)
	}
	network, _ := echo["network"].([]any)
	if len(network) != 1 {
		t.Errorf("network = %v, want one entry", echo["network"])
	}
}

func TestMicrovmLifecycle_PauseAndIllegalTransition(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.pause", map[string]any{"id": "vm-1"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("pause = %+v, want ok", ok)
	}

	// A 409 illegal-transition surfaces as an error result carrying the
	// server's message.
	res = callTool(t, cs, "user.microvm.vm.pause", map[string]any{"id": "illegal"})
	if !res.IsError || !strings.Contains(resultText(t, res), "Cannot pause a microvm in state [killed].") {
		t.Errorf("illegal pause: want the 409 message, got %q", resultText(t, res))
	}
}

func TestKillMicrovm_RequiresConfirmation(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.kill", map[string]any{"id": "vm-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("kill without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.vm.kill", map[string]any{"id": "vm-1", "confirm": true})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("kill with confirm = %+v, want ok", ok)
	}
}

func TestMicrovmTimeoutDeployRollbackEnv(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.set_timeout", map[string]any{"id": "vm-1", "timeout": 60})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("set_timeout = %+v, want ok", ok)
	}

	res = callTool(t, cs, "user.microvm.vm.set_timeout", map[string]any{"id": "vm-1", "timeout": 0})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Errorf("timeout 0: want the mock's 422, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.vm.deploy", map[string]any{"id": "vm-1", "image_version_id": "v-3"})
	var run tools.MicrovmRunResult
	unmarshalResult(t, res, &run)
	if run.Run["id"] != "run-2" {
		t.Errorf("deploy run = %v, want run-2", run.Run)
	}
	echo, _ := run.Run["echo"].(map[string]any)
	if echo["image_version_id"] != "v-3" {
		t.Errorf("deploy body = %v, want image_version_id v-3", echo)
	}

	res = callTool(t, cs, "user.microvm.vm.rollback", map[string]any{"id": "vm-1", "image_version_id": "v-1"})
	run = tools.MicrovmRunResult{}
	unmarshalResult(t, res, &run)
	if run.Run["id"] != "run-2" {
		t.Errorf("rollback run = %v, want run-2", run.Run)
	}

	res = callTool(t, cs, "user.microvm.vm.set_env", map[string]any{"id": "vm-1", "env": map[string]any{"FOO": "bar"}})
	var envOut tools.MicrovmEnvResult
	unmarshalResult(t, res, &envOut)
	if len(envOut.Keys) != 1 || envOut.Keys[0] != "FOO" {
		t.Errorf("set_env keys = %v, want [FOO] (values never returned)", envOut.Keys)
	}
}

func TestMicrovmDomains_AddAndRemoveGate(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.domain_add", map[string]any{"id": "vm-1", "hostname": "web.example.test"})
	var dom tools.MicrovmDomainResult
	unmarshalResult(t, res, &dom)
	if dom.Domain["id"] != "dom-2" {
		t.Errorf("domain = %v, want dom-2", dom.Domain)
	}

	res = callTool(t, cs, "user.microvm.vm.domain_add", map[string]any{"id": "vm-1", "hostname": "taken.example.test"})
	if !res.IsError {
		t.Errorf("duplicate hostname must be an error result, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.vm.domain_remove", map[string]any{"id": "vm-1", "domain_id": "dom-2"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("domain_remove without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.vm.domain_remove", map[string]any{"id": "vm-1", "domain_id": "dom-2", "confirm": true})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("domain_remove with confirm = %+v, want ok", ok)
	}
}

func TestMicrovmLogsAndMetrics(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.logs", map[string]any{"id": "vm-1", "limit": 50})
	var logs tools.MicrovmLogsResult
	unmarshalResult(t, res, &logs)
	if len(logs.Lines) != 2 {
		t.Fatalf("logs = %v, want 2 lines", logs.Lines)
	}
	if logs.Lines[0]["queried_limit"] != "50" {
		t.Errorf("limit did not reach the query string: %v", logs.Lines[0])
	}

	res = callTool(t, cs, "user.microvm.vm.metrics", map[string]any{"id": "vm-1"})
	var metrics map[string]any
	unmarshalResult(t, res, &metrics)
	if metrics["cpu_pct"] != 12.5 {
		t.Errorf("metrics cpu_pct = %v, want 12.5", metrics["cpu_pct"])
	}
}

func TestDeleteMicrovm_RequiresConfirmation(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.vm.delete", map[string]any{"id": "vm-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.vm.delete", map[string]any{"id": "vm-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "vm-1" {
		t.Errorf("delete result = %+v, want {vm-1 true}", del)
	}
}
