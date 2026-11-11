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
		// 3381: create env must be the object map, never the legacy text.
		if env, present := body["env"]; present {
			if _, isMap := env.(map[string]any); !isMap {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
					"message": "env must be an object",
					"errors":  map[string]any{"env": []string{"env must be an object"}},
				})
				return
			}
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

// SBX-PLAN-FITS-KIT: the Master refuses a plan whose disk is smaller than the
// image (422, reason plan_disk_too_small). The tool must surface the refusal
// with its reason, and must send plan_id only when the caller gave one (an
// omitted plan_id means "the smallest plan that fits" on the Master).
func TestMicrovm_CreateSurfacesPlanDiskTooSmallAndOmitsAnAbsentPlan(t *testing.T) {
	var sent []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body)
		if body["plan_id"] == "plan-1gib" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"success": false, "reason": "plan_disk_too_small",
				"message": "The kit image needs 4 GiB of disk but the selected plan has 1 GiB. Choose a plan with at least 4 GiB of disk.",
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{"id": "vm-1", "name": body["name"], "state": "creating"}})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1", "image_id": "img-1", "name": "worker", "plan_id": "plan-1gib",
	})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, "plan_disk_too_small") || !strings.Contains(text, "4 GiB") {
		t.Fatalf("a too-small plan must surface its reason and sizes, got error=%v %q", res.IsError, text)
	}

	res = callTool(t, cs, "user.microvm.create", map[string]any{"location_id": "loc-1", "image_id": "img-1", "name": "worker"})
	if res.IsError {
		t.Fatalf("create without a plan failed: %s", resultText(t, res))
	}
	if _, present := sent[len(sent)-1]["plan_id"]; present {
		t.Errorf("an absent plan_id must not be sent: %v", sent[len(sent)-1])
	}
}

// envFake is a stateful Master stand-in for the create-with-env flow. older true behaves like
// the real pre-3381 Master: 'env' => 'nullable|string' makes an object map a 422 with errors.env
// and creates no row.
type envFake struct {
	older       bool
	createCalls int
	rows        int
	createEnv   []any
	putCalls    int
	putEnv      map[string]any
	storedKeys  []string
}

func (f *envFake) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.createCalls++
		f.createEnv = append(f.createEnv, body["env"])
		env, isMap := body["env"].(map[string]any)
		if isMap && f.older {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The env field must be a string.",
				"errors":  map[string]any{"env": []string{"The env field must be a string."}},
			})
			return
		}
		if isMap {
			for k := range env {
				f.storedKeys = append(f.storedKeys, k)
			}
		}
		f.rows++
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{"id": "vm-1", "name": body["name"], "state": "creating", "location_id": body["location_id"]}})
	})
	mux.HandleFunc("GET /microvm/vm/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "microvm": map[string]any{"id": "vm-1"}, "env_keys": append([]string{}, f.storedKeys...)})
	})
	mux.HandleFunc("PUT /microvm/vm/{id}/env", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.putCalls++
		f.putEnv, _ = body["env"].(map[string]any)
		f.storedKeys = nil
		for k := range f.putEnv {
			f.storedKeys = append(f.storedKeys, k)
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "keys": f.storedKeys})
	})
	return mux
}

func createWithEnv(t *testing.T, f *envFake) {
	t.Helper()
	cs := connectSession(t, f.handler())
	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1",
		"image_id":    "img-1",
		"name":        "worker",
		"env":         map[string]any{"API_KEY": " keep edges ", "DEBUG": "false"},
	})
	if res.IsError {
		t.Fatalf("create with env failed: %s", resultText(t, res))
	}
}

// (b) The current Master accepts the object map: one create, no PUT.
func TestMicrovm_CreateWithCurrentMasterSendsOneCreateAndNoPut(t *testing.T) {
	f := &envFake{}
	createWithEnv(t, f)
	got, ok := f.createEnv[0].(map[string]any)
	if !ok || got["API_KEY"] != " keep edges " || got["DEBUG"] != "false" {
		t.Fatalf("env sent = %#v, want the object map verbatim", f.createEnv[0])
	}
	if f.createCalls != 1 || f.rows != 1 || f.putCalls != 0 {
		t.Errorf("creates=%d rows=%d puts=%d, want 1, 1, 0", f.createCalls, f.rows, f.putCalls)
	}
}

// (a) The real older Master refuses the map with 422 errors.env (no row). The create is retried
// once without env and the variables are set through PUT, the pre-3381 sequence.
func TestMicrovm_CreateAgainstOlderMasterRetriesWithoutEnvThenPuts(t *testing.T) {
	f := &envFake{older: true}
	createWithEnv(t, f)
	if f.createCalls != 2 {
		t.Fatalf("create calls = %d, want 2 (refused, then retried)", f.createCalls)
	}
	if f.createEnv[0] == nil || f.createEnv[1] != nil {
		t.Errorf("env per create = %#v, want [map, absent]", f.createEnv)
	}
	if f.rows != 1 {
		t.Errorf("rows = %d, want exactly one sandbox (the 422 creates none)", f.rows)
	}
	if f.putCalls != 1 || f.putEnv["API_KEY"] != " keep edges " || f.putEnv["DEBUG"] != "false" {
		t.Errorf("puts=%d env=%#v, want one PUT with the full map", f.putCalls, f.putEnv)
	}
}

// A 422 about another field is surfaced unchanged: no retry, no PUT.
func TestMicrovm_CreateOtherValidationErrorIsNotRetried(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"message": "The name has already been taken.",
			"errors":  map[string]any{"name": []string{"You already have a sandbox with this name."}},
		})
	})
	cs := connectSession(t, mux)
	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1", "image_id": "img-1", "name": "worker",
		"env": map[string]any{"A": "b"},
	})
	if !res.IsError {
		t.Fatalf("want the 422 surfaced, got success")
	}
	if calls != 1 {
		t.Errorf("create calls = %d, want 1 (not retried)", calls)
	}
}

// SBX-LATENCY: the resume fields of the user API sandbox read reach the tool
// output unchanged, and the tool description documents them.
func TestMicrovm_GetCarriesResumeFieldsAndDescribesThem(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /microvm/vm/vm-resume", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"microvm": map[string]any{
				"id": "vm-resume", "state": "running",
				"last_resume_mode": "fresh_boot", "last_resume_reason": "snapshot_missing", "last_resumed_at": "2026-11-10T09:30:00.000000Z",
			},
			"runs":     []any{map[string]any{"id": "run-1", "resume_mode": "fresh_boot", "resume_reason": "snapshot_missing", "resumed_at": "2026-11-10T09:30:00.000000Z"}},
			"versions": []any{}, "domains": []any{}, "env_keys": []any{},
		})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.microvm.get", map[string]any{"id": "vm-resume"})
	var got tools.MicrovmShowResult
	unmarshalResult(t, res, &got)
	if got.Microvm["last_resume_mode"] != "fresh_boot" || got.Microvm["last_resume_reason"] != "snapshot_missing" || got.Microvm["last_resumed_at"] == nil {
		t.Errorf("resume fields lost: %v", got.Microvm)
	}
	if len(got.Runs) != 1 || got.Runs[0]["resume_mode"] != "fresh_boot" {
		t.Errorf("run resume fields lost: %v", got.Runs)
	}

	list, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "user.microvm.get" {
			for _, want := range []string{"last_resume_mode", "fresh_boot", "last_resume_reason", "last_resumed_at"} {
				if !strings.Contains(tool.Description, want) {
					t.Errorf("description does not document %s: %q", want, tool.Description)
				}
			}
			return
		}
	}
	t.Fatal("user.microvm.get not registered")
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

// SBX-SG-ATTACH: the create tool's security_group_ids is a real parameter: it
// reaches the API as the sandbox-level field, and the per-NIC field still goes
// out as given.
func TestMicrovm_CreateSendsSecurityGroupIDs(t *testing.T) {
	var sent map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{
			"id": "vm-1", "security_groups": []any{map[string]any{"id": "sg-1", "name": "web"}},
		}})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1", "image_id": "img-1", "name": "fw",
		"security_group_ids": []string{"sg-1"},
		"network":            []map[string]any{{"kind": "public", "security_group_ids": []string{"sg-1"}}},
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	ids, _ := sent["security_group_ids"].([]any)
	if len(ids) != 1 || ids[0] != "sg-1" {
		t.Errorf("top-level security_group_ids = %v", sent["security_group_ids"])
	}
	nic, _ := sent["network"].([]any)
	first, _ := nic[0].(map[string]any)
	if nicIDs, _ := first["security_group_ids"].([]any); len(nicIDs) != 1 {
		t.Errorf("per-NIC security_group_ids = %v", first["security_group_ids"])
	}

	// Not set: the field is omitted so the API applies its own default (no groups).
	sent = nil
	res = callTool(t, cs, "user.microvm.create", map[string]any{"location_id": "loc-1", "image_id": "img-1", "name": "open"})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	if _, present := sent["security_group_ids"]; present {
		t.Errorf("security_group_ids sent when not set: %v", sent["security_group_ids"])
	}
}
