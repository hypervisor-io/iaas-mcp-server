package tools_test

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// egressMock is the sandbox policy API as the platform answers it: 202 with
// state pending for an accepted write, the typed refusals, and a recorder of
// every request body.
type egressMock struct {
	mu     sync.Mutex
	calls  []string
	bodies []map[string]any
	raw    []string
}

func (m *egressMock) record(r *http.Request) map[string]any {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, r.Method+" "+r.URL.Path)
	m.bodies = append(m.bodies, body)
	m.raw = append(m.raw, string(raw))
	return body
}

func (m *egressMock) handler() http.Handler {
	mux := http.NewServeMux()
	stored := map[string]any{
		"default_action": "deny", "inbound_replies": "service_ports",
		"rules": []any{map[string]any{"id": "r_4f2a9c1e", "action": "allow", "target": "api.github.com", "protocol": "any", "ports": []any{443}}},
	}
	read := func(policy any, generation, applied int, state string) map[string]any {
		return map[string]any{"success": true, "egress": map[string]any{
			"policy": policy, "generation": generation, "applied_generation": applied, "state": state,
			"applied_at": nil, "last_error_code": nil,
			"capability":    map[string]any{"available": true, "names": true},
			"service_ports": []any{map[string]any{"proto": "tcp", "port": 22}},
		}}
	}
	notFound := map[string]any{"success": false, "reason": "sandbox_not_found", "message": "Sandbox not found."}

	mux.HandleFunc("GET /microvm/vm/{id}/egress", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		switch r.PathValue("id") {
		case "missing":
			writeJSON(w, http.StatusNotFound, notFound)
		case "plain":
			writeJSON(w, http.StatusOK, read(nil, 0, 0, "none"))
		case "refused":
			body := read(stored, 2, 1, "failed")
			body["egress"].(map[string]any)["last_error_code"] = "egress_target_not_permitted"
			writeJSON(w, http.StatusOK, body)
		case "ended":
			// A killed sandbox: the node had confirmed generation 3 before it ended.
			writeJSON(w, http.StatusOK, read(stored, 3, 3, "inactive"))
		default:
			writeJSON(w, http.StatusOK, read(stored, 3, 3, "applied"))
		}
	})
	mux.HandleFunc("PUT /microvm/vm/{id}/egress", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		switch r.PathValue("id") {
		case "missing":
			writeJSON(w, http.StatusNotFound, notFound)
		case "killed":
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "egress_policy_terminal", "message": "The outbound network policy of a stopped-for-good sandbox cannot be changed."})
		case "old-node":
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "egress_policy_unsupported_node", "message": "This sandbox runs on a node that cannot enforce an outbound network policy."})
		default:
			if rules, _ := body["rules"].([]any); len(rules) > 0 {
				if first, _ := rules[0].(map[string]any); first["target"] == "169.254.169.254" {
					writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
						"success": false, "reason": "egress_target_not_permitted",
						"message":    "rules.0.target: this address is blocked by the platform (metadata)",
						"errors":     map[string]any{"rules.0.target": []string{"this address is blocked by the platform (metadata)"}},
						"violations": []any{map[string]any{"code": "egress_target_not_permitted", "field": "rules.0.target", "message": "this address is blocked by the platform (metadata)"}},
					})
					return
				}
			}
			writeJSON(w, http.StatusAccepted, read(body, 4, 3, "pending"))
		}
	})
	mux.HandleFunc("DELETE /microvm/vm/{id}/egress", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if r.PathValue("id") == "plain" {
			writeJSON(w, http.StatusOK, read(nil, 0, 0, "none"))
			return
		}
		writeJSON(w, http.StatusAccepted, read(nil, 5, 3, "pending"))
	})
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{
			"id": "vm-new", "name": body["name"], "state": "creating",
			"egress": map[string]any{"policy_set": body["egress_policy"] != nil, "default_action": "deny", "rule_count": 1, "generation": 1, "state": "applied"},
		}})
	})
	return mux
}

func (m *egressMock) last() (string, map[string]any, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := len(m.calls) - 1
	return m.calls[i], m.bodies[i], m.raw[i]
}

func TestMicrovmEgress_GetReturnsThePolicyAndWhetherTheNodeEnforcesIt(t *testing.T) {
	mock := &egressMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.egress.get", map[string]any{"id": "vm-1"})
	if res.IsError {
		t.Fatalf("get failed: %s", resultText(t, res))
	}
	var got tools.MicrovmEgressResult
	unmarshalResult(t, res, &got)
	policy, _ := got.Egress["policy"].(map[string]any)
	if policy["default_action"] != "deny" || got.Egress["state"] != "applied" || !got.Confirmed {
		t.Fatalf("get = %+v", got)
	}
	if call, _, raw := mock.last(); call != "GET /microvm/vm/vm-1/egress" || raw != "" {
		t.Fatalf("request = %s body %q", call, raw)
	}

	// No policy: policy is null and nothing is owed.
	res = callTool(t, cs, "user.microvm.egress.get", map[string]any{"id": "plain"})
	got = tools.MicrovmEgressResult{}
	unmarshalResult(t, res, &got)
	if got.Egress["policy"] != nil || got.Egress["state"] != "none" || !got.Confirmed {
		t.Fatalf("a sandbox without a policy = %+v", got)
	}

	// The node refused the stored policy: not confirmed, and the code is visible.
	res = callTool(t, cs, "user.microvm.egress.get", map[string]any{"id": "refused"})
	got = tools.MicrovmEgressResult{}
	unmarshalResult(t, res, &got)
	if got.Confirmed || got.Egress["state"] != "failed" || got.Egress["last_error_code"] != "egress_target_not_permitted" {
		t.Fatalf("a refused policy = %+v", got)
	}

	res = callTool(t, cs, "user.microvm.egress.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "Sandbox not found") {
		t.Fatalf("unknown sandbox: want the API's not found, got %q", resultText(t, res))
	}
}

func TestMicrovmEgress_SetSendsThePolicyDocumentAsTheBody(t *testing.T) {
	mock := &egressMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.egress.set", map[string]any{
		"id":             "vm-1",
		"default_action": "deny",
		"rules": []map[string]any{
			{"action": "allow", "target": "api.github.com", "ports": []string{"443"}},
			{"action": "allow", "target": "8.8.8.8", "protocol": "tcp", "ports": []string{"443", "8000-8100"}, "comment": "resolver over https", "id": "dns"},
		},
	})
	if res.IsError {
		t.Fatalf("set failed: %s", resultText(t, res))
	}
	call, body, _ := mock.last()
	if call != "PUT /microvm/vm/vm-1/egress" {
		t.Fatalf("request = %s", call)
	}
	want := map[string]any{
		"default_action": "deny",
		"rules": []any{
			map[string]any{"action": "allow", "target": "api.github.com", "ports": []any{float64(443)}},
			map[string]any{"action": "allow", "target": "8.8.8.8", "protocol": "tcp", "ports": []any{float64(443), "8000-8100"}, "comment": "resolver over https", "id": "dns"},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("PUT body = %v\nwant the policy document itself (no id, no wrapper key, unset fields omitted) %v", body, want)
	}

	// A 202 is success; confirmed is false until the node enforces the new generation.
	var got tools.MicrovmEgressResult
	unmarshalResult(t, res, &got)
	if got.Confirmed || got.Egress["state"] != "pending" || got.Egress["generation"] != float64(4) {
		t.Fatalf("set result = %+v", got)
	}

	// A policy that is the default action alone still sends an explicit empty rule list.
	res = callTool(t, cs, "user.microvm.egress.set", map[string]any{"id": "vm-1", "default_action": "allow", "inbound_replies": "any"})
	if res.IsError {
		t.Fatalf("set without rules failed: %s", resultText(t, res))
	}
	if _, body, raw := mock.last(); !reflect.DeepEqual(body, map[string]any{"default_action": "allow", "inbound_replies": "any", "rules": []any{}}) {
		t.Fatalf("PUT body = %s", raw)
	}
}

func TestMicrovmEgress_SetNeedsADefaultActionAndSurfacesTypedRefusals(t *testing.T) {
	mock := &egressMock{}
	cs := connectSession(t, mock.handler())

	// default_action is required by the tool's schema: refused before any request.
	res := callTool(t, cs, "user.microvm.egress.set", map[string]any{"id": "vm-1", "rules": []map[string]any{{"action": "allow", "target": "8.8.8.8"}}})
	if !res.IsError || !strings.Contains(resultText(t, res), "default_action") {
		t.Fatalf("set without default_action: want a schema error naming it, got %q", resultText(t, res))
	}
	if len(mock.calls) != 0 {
		t.Fatalf("a schema refusal must not reach the API; calls = %v", mock.calls)
	}

	res = callTool(t, cs, "user.microvm.egress.set", map[string]any{"id": "vm-1", "default_action": "deny", "rules": []map[string]any{{"action": "allow", "target": "169.254.169.254"}}})
	text := resultText(t, res)
	if !res.IsError || !strings.Contains(text, "egress_target_not_permitted") || !strings.Contains(text, "rules.0.target") || !strings.Contains(text, "blocked by the platform") {
		t.Fatalf("a refused policy must carry the reason, the field and the message; got %q", text)
	}

	for id, reason := range map[string]string{"killed": "egress_policy_terminal", "old-node": "egress_policy_unsupported_node", "missing": "sandbox_not_found"} {
		res = callTool(t, cs, "user.microvm.egress.set", map[string]any{"id": id, "default_action": "deny"})
		if !res.IsError || !strings.Contains(resultText(t, res), reason) {
			t.Errorf("%s: want reason %s, got %q", id, reason, resultText(t, res))
		}
	}
}

func TestMicrovmEgress_ClearRequiresConfirmAndReportsTheRemoval(t *testing.T) {
	mock := &egressMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.egress.clear", map[string]any{"id": "vm-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("clear without confirm should refuse; got %q", resultText(t, res))
	}
	if len(mock.calls) != 0 {
		t.Fatalf("an unconfirmed clear must not reach the API; calls = %v", mock.calls)
	}

	res = callTool(t, cs, "user.microvm.egress.clear", map[string]any{"id": "vm-1", "confirm": true})
	if res.IsError {
		t.Fatalf("confirmed clear failed: %s", resultText(t, res))
	}
	if call, _, raw := mock.last(); call != "DELETE /microvm/vm/vm-1/egress" || raw != "" {
		t.Fatalf("request = %s body %q", call, raw)
	}
	var got tools.MicrovmEgressResult
	unmarshalResult(t, res, &got)
	if got.Egress["policy"] != nil || got.Confirmed || got.Egress["state"] != "pending" {
		t.Fatalf("clear result = %+v", got)
	}

	// Nothing to remove: a 200 with state none, already confirmed.
	res = callTool(t, cs, "user.microvm.egress.clear", map[string]any{"id": "plain", "confirm": true})
	got = tools.MicrovmEgressResult{}
	unmarshalResult(t, res, &got)
	if !got.Confirmed || got.Egress["state"] != "none" {
		t.Fatalf("clear without a policy = %+v", got)
	}
}

func TestMicrovmEgress_CreateCarriesThePolicyOnlyWhenGiven(t *testing.T) {
	mock := &egressMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1", "image_id": "img-1", "name": "agent",
		"egress_policy": map[string]any{
			"default_action": "deny",
			"rules":          []map[string]any{{"action": "allow", "target": "api.github.com", "ports": []string{"443"}}},
		},
	})
	if res.IsError {
		t.Fatalf("create with a policy failed: %s", resultText(t, res))
	}
	_, body, _ := mock.last()
	want := map[string]any{"default_action": "deny", "rules": []any{map[string]any{"action": "allow", "target": "api.github.com", "ports": []any{float64(443)}}}}
	if !reflect.DeepEqual(body["egress_policy"], want) {
		t.Fatalf("create body egress_policy = %v; want %v", body["egress_policy"], want)
	}

	res = callTool(t, cs, "user.microvm.create", map[string]any{"location_id": "loc-1", "image_id": "img-1", "name": "plain"})
	if res.IsError {
		t.Fatalf("create without a policy failed: %s", resultText(t, res))
	}
	if _, body, _ := mock.last(); body["egress_policy"] != nil {
		t.Fatalf("a create without a policy must not send egress_policy; body = %v", body)
	}
	if _, ok := mock.bodies[len(mock.bodies)-1]["egress_policy"]; ok {
		t.Fatal("egress_policy key must be absent, not null")
	}
}

func egressLogHandler(seen *[]string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /microvm/vm/{id}/egress/log", func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.PathValue("id") {
		case "missing":
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "sandbox_not_found", "message": "Sandbox not found."})
		case "empty":
			writeJSON(w, http.StatusOK, map[string]any{"success": true,
				"events":     map[string]any{"current_page": 1, "data": []any{}, "per_page": 50, "total": 0, "last_page": 1},
				"collection": map[string]any{"pulled_at": nil, "packet_layer": nil, "retention_days": 14, "max_rows": 5000}})
		default:
			if r.URL.Query().Get("layer") == "http" {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "The selected layer is invalid.", "errors": map[string]any{"layer": []string{"The selected layer is invalid."}}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true,
				"events": map[string]any{"current_page": 2, "per_page": 2, "total": 5, "last_page": 3, "data": []any{
					map[string]any{"id": 12, "layer": "dns", "class": "dns_name_denied", "rule": "default", "destination": "telemetry.example.net", "withheld": false, "qtype": "A", "count": 14, "sampled": false},
					map[string]any{"id": 11, "layer": "packet", "class": "floor", "rule": "floor:platform_network", "destination": nil, "withheld": true, "protocol": "tcp", "port": 443, "count": 1, "sampled": true},
				}},
				"collection": map[string]any{"pulled_at": "2026-11-12T09:15:00+00:00", "packet_layer": true, "retention_days": 14, "max_rows": 5000}})
		}
	})
	return mux
}

func TestMicrovmEgress_LogListsRefusalsWithFiltersAndPaging(t *testing.T) {
	var seen []string
	cs := connectSession(t, egressLogHandler(&seen))

	res := callTool(t, cs, "user.microvm.egress.log", map[string]any{
		"id": "vm-1", "layer": "dns", "search": "telemetry", "since": "2026-11-12T00:00:00Z", "page": "2", "per_page": "2",
	})
	if res.IsError {
		t.Fatalf("log failed: %s", resultText(t, res))
	}
	if got := seen[len(seen)-1]; got != "/microvm/vm/vm-1/egress/log?layer=dns&page=2&per_page=2&search=telemetry&since=2026-11-12T00%3A00%3A00Z" {
		t.Fatalf("request = %s", got)
	}
	var got tools.MicrovmEgressLogResult
	unmarshalResult(t, res, &got)
	if len(got.Events) != 2 || got.Total != 5 || got.Page != 2 || got.PerPage != 2 || got.LastPage != 3 {
		t.Fatalf("log page = %+v", got)
	}
	if got.Events[0]["destination"] != "telemetry.example.net" || got.Events[0]["rule"] != "default" {
		t.Fatalf("first entry = %v", got.Events[0])
	}
	// A refusal that hit a platform network has no destination, and none is invented.
	if got.Events[1]["withheld"] != true || got.Events[1]["destination"] != nil {
		t.Fatalf("withheld entry = %v", got.Events[1])
	}
	if got.Collection["retention_days"] != float64(14) || got.Collection["packet_layer"] != true {
		t.Fatalf("collection = %v", got.Collection)
	}

	// No filters: no query string. An empty log is an empty list, not an error.
	res = callTool(t, cs, "user.microvm.egress.log", map[string]any{"id": "empty"})
	if res.IsError {
		t.Fatalf("empty log failed: %s", resultText(t, res))
	}
	if got := seen[len(seen)-1]; got != "/microvm/vm/empty/egress/log?" {
		t.Fatalf("unfiltered request = %s", got)
	}
	got = tools.MicrovmEgressLogResult{}
	unmarshalResult(t, res, &got)
	if got.Events == nil || len(got.Events) != 0 || got.Total != 0 {
		t.Fatalf("empty log = %+v", got)
	}

	res = callTool(t, cs, "user.microvm.egress.log", map[string]any{"id": "vm-1", "layer": "http"})
	if !res.IsError || !strings.Contains(resultText(t, res), "layer") {
		t.Fatalf("a bad filter: want the API's field error, got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm.egress.log", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "Sandbox not found") {
		t.Fatalf("unknown sandbox: want the API's not found, got %q", resultText(t, res))
	}
}

func TestMicrovmEgress_PresetsListAndApplyKeepTheSourceTag(t *testing.T) {
	mock := &egressMock{}
	base := mock.handler()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /microvm/egress/presets", func(w http.ResponseWriter, r *http.Request) {
		mock.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "presets": []any{
			map[string]any{"id": "no_outbound", "version": 1, "source": "preset:no_outbound@1", "name": "No outbound access", "description": "d", "rule_count": 0,
				"policy": map[string]any{"default_action": "deny", "rules": []any{}}},
			map[string]any{"id": "model_apis", "version": 2, "source": "preset:model_apis@2", "name": "AI provider APIs", "description": "d", "rule_count": 1,
				"policy": map[string]any{"default_action": "deny", "rules": []any{
					map[string]any{"action": "allow", "target": "api.anthropic.com", "protocol": "tcp", "ports": []any{443}, "source": "preset:model_apis@2"},
				}}},
		}})
	})
	mux.Handle("/", base)
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.microvm.egress.presets", map[string]any{})
	if res.IsError {
		t.Fatalf("presets failed: %s", resultText(t, res))
	}
	if call, _, raw := mock.last(); call != "GET /microvm/egress/presets" || raw != "" {
		t.Fatalf("request = %s body %q", call, raw)
	}
	var got tools.MicrovmEgressPresetsResult
	unmarshalResult(t, res, &got)
	if len(got.Presets) != 2 || got.Presets[0]["id"] != "no_outbound" || got.Presets[1]["source"] != "preset:model_apis@2" {
		t.Fatalf("presets = %+v", got.Presets)
	}
	policy := got.Presets[1]["policy"].(map[string]any)
	rule := policy["rules"].([]any)[0].(map[string]any)

	// Applying a preset is an ordinary set: the document goes out as the body, the source tag with it.
	res = callTool(t, cs, "user.microvm.egress.set", map[string]any{
		"id": "vm-1", "default_action": policy["default_action"],
		"rules": []map[string]any{{"action": rule["action"], "target": rule["target"], "protocol": rule["protocol"], "ports": []string{"443"}, "source": rule["source"]}},
	})
	if res.IsError {
		t.Fatalf("applying a preset failed: %s", resultText(t, res))
	}
	call, body, _ := mock.last()
	want := map[string]any{"default_action": "deny", "rules": []any{
		map[string]any{"action": "allow", "target": "api.anthropic.com", "protocol": "tcp", "ports": []any{float64(443)}, "source": "preset:model_apis@2"},
	}}
	if call != "PUT /microvm/vm/vm-1/egress" || !reflect.DeepEqual(body, want) {
		t.Fatalf("apply = %s %v\nwant %v", call, body, want)
	}
}

// A killed or errored sandbox reads state inactive: the stored policy is kept
// for reading and is in force nowhere, so it is never reported as confirmed,
// whatever generation the node had applied before the sandbox ended.
func TestMicrovmEgress_GetOfAnEndedSandboxIsInactiveAndNotConfirmed(t *testing.T) {
	mock := &egressMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.egress.get", map[string]any{"id": "ended"})
	if res.IsError {
		t.Fatalf("get failed: %s", resultText(t, res))
	}
	var got tools.MicrovmEgressResult
	unmarshalResult(t, res, &got)
	policy, _ := got.Egress["policy"].(map[string]any)
	if got.Confirmed || got.Egress["state"] != "inactive" || policy["default_action"] != "deny" {
		t.Fatalf("an ended sandbox's policy must be readable and must not read as in force: %+v", got)
	}
	if got.Egress["generation"] != float64(3) || got.Egress["applied_generation"] != float64(3) {
		t.Fatalf("the generations are passed through: %+v", got.Egress)
	}
}
