package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func microvmMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["location_id"] == nil || body["location_id"] == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"location_id": []string{"The location id field is required."}},
			})
			return
		}
		// hypervisor_group_id must never be sent by this MCP build - only
		// the canonical location_id.
		if _, ok := body["hypervisor_group_id"]; ok {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "unexpected hypervisor_group_id in body",
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"microvm": map[string]any{
				"id":          "vm-1",
				"name":        body["name"],
				"state":       "creating",
				"location_id": body["location_id"],
				"ssh_key_ids": body["ssh_key_ids"],
				"network":     body["network"],
			},
		})
	})

	mux.HandleFunc("GET /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"microvms": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{map[string]any{"id": "vm-1", "name": "worker", "state": "running"}},
			},
		})
	})

	mux.HandleFunc("GET /microvm/vm/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "MicroVM not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"microvm":  map[string]any{"id": id, "name": "worker", "state": "running", "location_id": "loc-1"},
			"runs":     []any{map[string]any{"id": "run-1", "status": "running"}},
			"versions": []any{map[string]any{"id": "ver-1", "version": float64(1)}},
			"domains":  []any{},
			"env_keys": []any{"API_KEY"},
		})
	})

	for _, verb := range []string{"pause", "resume", "stop", "start", "kill"} {
		verb := verb
		mux.HandleFunc("POST /microvm/vm/{id}/"+verb, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
		})
	}

	mux.HandleFunc("POST /microvm/vm/{id}/timeout", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["timeout"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "The timeout field is required."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	mux.HandleFunc("POST /microvm/vm/{id}/deploy", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "run": map[string]any{"id": "run-2", "status": "deploying"}})
	})
	mux.HandleFunc("POST /microvm/vm/{id}/rollback", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "run": map[string]any{"id": "run-3", "status": "deploying"}})
	})

	mux.HandleFunc("PUT /microvm/vm/{id}/env", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env, _ := body["env"].(map[string]any)
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "keys": keys})
	})

	mux.HandleFunc("POST /microvm/vm/{id}/domain", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true, "domain": map[string]any{"id": "dom-1", "hostname": body["hostname"]},
		})
	})
	mux.HandleFunc("DELETE /microvm/vm/{id}/domain/{domainId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	mux.HandleFunc("GET /microvm/vm/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "lines": []any{map[string]any{"line": "booted"}}})
	})
	mux.HandleFunc("GET /microvm/vm/{id}/metrics", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "metrics": map[string]any{"cpu_pct": 1.5}})
	})

	mux.HandleFunc("DELETE /microvm/vm/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "MicroVM deleted."})
	})

	return mux
}

func TestMicrovm_CreateUsesCanonicalLocationIdAndNetworkShape(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1",
		"image_id":    "img-1",
		"name":        "worker",
		"ssh_key_ids": []string{"key-1"},
		"network": []map[string]any{
			{"kind": "public"},
			{"kind": "public", "static_ip_id": "sip-1"},
			{"kind": "vpc", "vpc_subnet_id": "vpcs-1"},
		},
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	var created tools.MicrovmResult
	unmarshalResult(t, res, &created)
	if created.Microvm["id"] != "vm-1" {
		t.Fatalf("create id = %v, want vm-1", created.Microvm["id"])
	}
	if created.Microvm["location_id"] != "loc-1" {
		t.Errorf("location_id = %v, want loc-1", created.Microvm["location_id"])
	}
	network, ok := created.Microvm["network"].([]any)
	if !ok || len(network) != 3 {
		t.Fatalf("network echoed = %v, want 3 entries", created.Microvm["network"])
	}
	first, _ := network[0].(map[string]any)
	if _, hasSubnet := first["subnet_id"]; hasSubnet {
		t.Errorf("kind:public entry sent a subnet_id when none was given: %v", first)
	}
	if first["kind"] != "public" {
		t.Errorf("first network entry kind = %v, want public", first["kind"])
	}
}

func TestMicrovm_CreateMissingLocationIsValidationError(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"image_id": "img-1",
		"name":     "worker",
	})
	// location_id is a required field on the tool's JSON schema, so the MCP
	// SDK rejects the call before it ever reaches the mock API.
	if !res.IsError || !strings.Contains(resultText(t, res), "location_id") {
		t.Fatalf("create without location_id: want a schema error naming location_id, got %q", resultText(t, res))
	}
}

func TestMicrovm_ListGetAndLifecycleActions(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.list", map[string]any{})
	var list tools.MicrovmListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Fatalf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.microvm.get", map[string]any{"id": "vm-1"})
	var got tools.MicrovmShowResult
	unmarshalResult(t, res, &got)
	if got.Microvm["id"] != "vm-1" {
		t.Errorf("get id = %v, want vm-1", got.Microvm["id"])
	}
	if len(got.EnvKeys) != 1 || got.EnvKeys[0] != "API_KEY" {
		t.Errorf("env_keys = %v, want [API_KEY]", got.EnvKeys)
	}

	for _, tool := range []string{"user.microvm.pause", "user.microvm.resume", "user.microvm.stop", "user.microvm.start"} {
		res = callTool(t, cs, tool, map[string]any{"id": "vm-1"})
		var ok tools.OKResult
		unmarshalResult(t, res, &ok)
		if !ok.OK {
			t.Errorf("%s: OK = false", tool)
		}
	}

	res = callTool(t, cs, "user.microvm.set_timeout", map[string]any{"id": "vm-1", "timeout": 3600})
	var okr tools.OKResult
	unmarshalResult(t, res, &okr)
	if !okr.OK {
		t.Errorf("set_timeout: OK = false")
	}

	res = callTool(t, cs, "user.microvm.deploy", map[string]any{"id": "vm-1", "image_version_id": "ver-2"})
	var run tools.MicrovmRunResult
	unmarshalResult(t, res, &run)
	if run.Run["id"] != "run-2" {
		t.Errorf("deploy run id = %v, want run-2", run.Run["id"])
	}

	res = callTool(t, cs, "user.microvm.rollback", map[string]any{"id": "vm-1", "image_version_id": "ver-1"})
	unmarshalResult(t, res, &run)
	if run.Run["id"] != "run-3" {
		t.Errorf("rollback run id = %v, want run-3", run.Run["id"])
	}

	res = callTool(t, cs, "user.microvm.set_env", map[string]any{"id": "vm-1", "env": map[string]any{"FOO": "bar"}})
	var envRes tools.MicrovmEnvResult
	unmarshalResult(t, res, &envRes)
	if len(envRes.Keys) != 1 || envRes.Keys[0] != "FOO" {
		t.Errorf("set_env keys = %v, want [FOO]", envRes.Keys)
	}

	res = callTool(t, cs, "user.microvm.domain_add", map[string]any{"id": "vm-1", "hostname": "app.example.com"})
	var dom tools.MicrovmDomainResult
	unmarshalResult(t, res, &dom)
	if dom.Domain["hostname"] != "app.example.com" {
		t.Errorf("domain_add hostname = %v", dom.Domain["hostname"])
	}

	res = callTool(t, cs, "user.microvm.logs", map[string]any{"id": "vm-1"})
	var logs tools.MicrovmLogsResult
	unmarshalResult(t, res, &logs)
	if len(logs.Lines) != 1 {
		t.Errorf("logs lines = %v, want 1 entry", logs.Lines)
	}

	res = callTool(t, cs, "user.microvm.metrics", map[string]any{"id": "vm-1"})
	if res.IsError {
		t.Errorf("metrics failed: %s", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
}

func TestMicrovm_KillDeleteAndDomainRemoveRequireConfirm(t *testing.T) {
	cs := connectSession(t, microvmMock())

	res := callTool(t, cs, "user.microvm.kill", map[string]any{"id": "vm-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("kill without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm.kill", map[string]any{"id": "vm-1", "confirm": true})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("confirmed kill did not succeed")
	}

	res = callTool(t, cs, "user.microvm.domain_remove", map[string]any{"id": "vm-1", "domain_id": "dom-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("domain_remove without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm.domain_remove", map[string]any{"id": "vm-1", "domain_id": "dom-1", "confirm": true})
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("confirmed domain_remove did not succeed")
	}

	res = callTool(t, cs, "user.microvm.delete", map[string]any{"id": "vm-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm.delete", map[string]any{"id": "vm-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
