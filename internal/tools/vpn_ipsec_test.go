package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

const (
	ipsecGW   = "gw-1"
	ipsecConn = "conn-1"
	ipsecKey  = "correct-horse-battery-staple"
)

// ipsecMock is a stateful fake of the IPsec user routes: a write bumps the gateway
// generation and the "node" confirms it after a few reads.
type ipsecMock struct {
	mu sync.Mutex

	exists, deleted bool
	conn            map[string]any
	generation      int
	applied         int
	pollsLeft       int
	failApply       bool

	installingLeft int
	tooManyLeft    int
	create500      bool
	leakPSK        bool // the fake server wrongly returns a psk field
	gwState        string

	posts   []map[string]any
	patches []map[string]any
}

func (m *ipsecMock) view() map[string]any {
	out := map[string]any{}
	for k, v := range m.conn {
		out[k] = v
	}
	out["id"] = ipsecConn
	state := "applied"
	if m.failApply {
		state = "failed"
	} else if m.applied < m.generation {
		state = "pending"
	}
	out["apply_state"] = state
	out["sa_state"] = "unknown"
	if m.leakPSK {
		out["psk"] = ipsecKey
	}
	return out
}

func (m *ipsecMock) tick() {
	if m.applied < m.generation && !m.failApply {
		m.pollsLeft--
		if m.pollsLeft <= 0 {
			m.applied = m.generation
		}
	}
}

func (m *ipsecMock) write() { m.generation++; m.pollsLeft = 2 }

func (m *ipsecMock) gateway() map[string]any {
	st := "idle"
	if m.failApply {
		st = "failed"
	} else if m.applied < m.generation {
		st = "pending"
	}
	var apiErr any
	if m.failApply {
		apiErr = "load_failed"
	}
	return map[string]any{
		"ipsec_state": map[bool]string{true: m.gwState, false: "ready"}[m.gwState != ""], "ipsec_apply_state": st, "ipsec_apply_error": apiErr,
		"ipsec_generation": float64(m.generation), "ipsec_applied_generation": float64(m.applied),
	}
}

func (m *ipsecMock) handler() http.Handler {
	mux := http.NewServeMux()
	nf := func(w http.ResponseWriter) {
		writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "not_found", "message": "IPsec connection not found."})
	}
	mux.HandleFunc("GET /vpn-gateway/{gw}/ipsec", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.tick()
		conns := []any{}
		if m.exists && !m.deleted {
			conns = append(conns, m.view())
		}
		writeJSON(w, 200, map[string]any{"success": true, "connections": conns, "gateway": m.gateway()})
	})
	mux.HandleFunc("POST /vpn-gateway/{gw}/ipsec", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.posts = append(m.posts, body)
		if m.tooManyLeft > 0 {
			m.tooManyLeft--
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"success": false, "message": "Too many requests"})
			return
		}
		if m.installingLeft > 0 {
			m.installingLeft--
			writeJSON(w, 409, map[string]any{"success": false, "reason": "ipsec_installing", "message": "IPsec is still being set up on this gateway; try again in a minute."})
			return
		}
		m.conn = map[string]any{"name": body["name"], "remote_ts": body["remote_ts"], "remote_address": body["remote_address"], "enabled": true}
		m.exists, m.deleted = true, false
		m.write()
		if m.create500 {
			writeJSON(w, 500, map[string]any{"success": false, "message": "boom"})
			return
		}
		writeJSON(w, 201, map[string]any{"success": true, "connection": m.view(), "warnings": []any{"security_group_blocks_esp"}})
	})
	mux.HandleFunc("GET /vpn-gateway/{gw}/ipsec/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !m.exists || m.deleted {
			nf(w)
			return
		}
		m.tick()
		writeJSON(w, 200, map[string]any{"success": true, "connection": m.view()})
	})
	mux.HandleFunc("PATCH /vpn-gateway/{gw}/ipsec/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.patches = append(m.patches, body)
		for k, v := range body {
			if k != "psk" {
				m.conn[k] = v
			}
		}
		m.write()
		writeJSON(w, 200, map[string]any{"success": true, "connection": m.view(), "warnings": []any{}})
	})
	mux.HandleFunc("DELETE /vpn-gateway/{gw}/ipsec/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.deleted = true
		m.write()
		writeJSON(w, 200, map[string]any{"success": true, "connection": m.view()})
	})
	mux.HandleFunc("POST /vpn-gateway/{gw}/ipsec/{id}/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "unconfirmed" {
			writeJSON(w, 503, map[string]any{"success": false, "reason": "node_dispatch_unconfirmed", "message": "The change is stored; the gateway did not confirm it yet."})
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		writeJSON(w, 200, map[string]any{"success": true, "connection": m.view()})
	})
	mux.HandleFunc("GET /vpn-gateway/{gw}/ipsec/{id}/remote-settings", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") == "text" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("Our public IP: 203.0.113.10\n"))
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "remote_settings": map[string]any{"our_public_ip": "203.0.113.10", "mode": "tunnel"}})
	})
	return mux
}

func createArgs() map[string]any {
	return map[string]any{
		"gateway_id": ipsecGW, "name": "office-hq", "psk": ipsecKey,
		"remote_address": "198.51.100.7", "remote_ts": []string{"192.168.50.0/24"},
	}
}

func TestVPNIpsec_CreateWaitsForApplyAndNeverEchoesTheKey(t *testing.T) {
	m := &ipsecMock{installingLeft: 2, leakPSK: true}
	cs := connectSession(t, m.handler())

	res := callTool(t, cs, "user.vpn_ipsec.create", createArgs())
	if strings.Contains(resultText(t, res), ipsecKey) {
		t.Fatalf("the result contains the pre-shared key: %s", resultText(t, res))
	}
	var out tools.VpnIpsecConnectionResult
	unmarshalResult(t, res, &out)
	if out.Connection["id"] != ipsecConn || out.Connection["apply_state"] != "applied" || !out.Applied {
		t.Fatalf("create result = %+v; want the applied connection", out)
	}
	if _, has := out.Connection["psk"]; has {
		t.Errorf("connection carries a psk field")
	}
	if len(out.Warnings) != 1 || out.Warnings[0] != "security_group_blocks_esp" {
		t.Errorf("warnings = %v", out.Warnings)
	}
	// two ipsec_installing refusals are waited out, the third POST is the recorded create
	if len(m.posts) != 3 {
		t.Errorf("POST count = %d; want 3", len(m.posts))
	}
	last := m.posts[len(m.posts)-1]
	if last["psk"] != ipsecKey || last["name"] != "office-hq" {
		t.Errorf("create body = %v", last)
	}
}

func TestVPNIpsec_CreateWaitFalseReturnsPending(t *testing.T) {
	m := &ipsecMock{}
	cs := connectSession(t, m.handler())
	args := createArgs()
	args["wait"] = false
	var out tools.VpnIpsecConnectionResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.create", args), &out)
	if out.Applied || out.Connection["apply_state"] != "pending" {
		t.Fatalf("wait=false result = %+v; want pending", out)
	}
}

func TestVPNIpsec_UncertainCreateFailsClosed(t *testing.T) {
	m := &ipsecMock{create500: true}
	cs := connectSession(t, m.handler())
	res := callTool(t, cs, "user.vpn_ipsec.create", createArgs())
	txt := resultText(t, res)
	if !res.IsError || !strings.Contains(txt, "no definite answer") || !strings.Contains(txt, ipsecConn) ||
		!strings.Contains(txt, "user.vpn_ipsec.get") || strings.Contains(txt, ipsecKey) {
		t.Fatalf("expected a fail-closed error naming the connection and the get call, got IsError=%v %q", res.IsError, txt)
	}
	if len(m.posts) != 1 {
		t.Errorf("POST count = %d; want exactly 1 (never replayed)", len(m.posts))
	}
}

func TestVPNIpsec_InstallFailureFailsFast(t *testing.T) {
	m := &ipsecMock{gwState: "failed"}
	cs := connectSession(t, m.handler())
	res := callTool(t, cs, "user.vpn_ipsec.create", createArgs())
	if !res.IsError || !strings.Contains(resultText(t, res), "installing IPsec on the gateway failed") {
		t.Fatalf("expected a fast install failure, got %q", resultText(t, res))
	}
}

func TestVPNIpsec_SelectorAndStringRulesAreRefusedBeforeAnyCall(t *testing.T) {
	m := &ipsecMock{exists: true, conn: map[string]any{"name": "office-hq"}}
	cs := connectSession(t, m.handler())
	bad := map[string]func(a map[string]any){
		"empty local_ts (would expose the whole VPC)": func(a map[string]any) { a["local_ts"] = []string{} },
		"non canonical remote":                        func(a map[string]any) { a["remote_ts"] = []string{"192.168.50.7/24"} },
		"loopback remote":                             func(a map[string]any) { a["remote_ts"] = []string{"127.0.0.0/24"} },
		"multicast remote":                            func(a map[string]any) { a["remote_ts"] = []string{"224.0.0.0/8"} },
		"overlap inside the list":                     func(a map[string]any) { a["remote_ts"] = []string{"192.168.0.0/16", "192.168.50.0/24"} },
		"empty optional string":                       func(a map[string]any) { a["local_id"] = "" },
		"padded optional string":                      func(a map[string]any) { a["remote_id"] = " x" },
	}
	for name, mutate := range bad {
		a := createArgs()
		mutate(a)
		if res := callTool(t, cs, "user.vpn_ipsec.create", a); !res.IsError {
			t.Errorf("create %s: expected a validation error", name)
		}
	}
	for name, args := range map[string]map[string]any{
		"update empty remote_ts": {"remote_ts": []string{}, "ike_lifetime": 7200},
		"update empty string":    {"local_id": ""},
		"update bad clear":       {"clear": []string{"name"}},
		"update set and clear":   {"local_id": "x", "clear": []string{"local_id"}},
	} {
		args["gateway_id"], args["connection_id"] = ipsecGW, ipsecConn
		if res := callTool(t, cs, "user.vpn_ipsec.update", args); !res.IsError {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if len(m.posts) != 0 || len(m.patches) != 0 {
		t.Errorf("validation let requests through: %d posts, %d patches", len(m.posts), len(m.patches))
	}
}

func TestVPNIpsec_FailedApplyIsAnError(t *testing.T) {
	m := &ipsecMock{failApply: true}
	cs := connectSession(t, m.handler())
	res := callTool(t, cs, "user.vpn_ipsec.create", createArgs())
	if !res.IsError || !strings.Contains(resultText(t, res), "load_failed") {
		t.Fatalf("expected an error naming load_failed, got %q", resultText(t, res))
	}
}

func TestVPNIpsec_ValidationRefusesBeforeAnyCallAndNeverEchoesTheKey(t *testing.T) {
	m := &ipsecMock{}
	cs := connectSession(t, m.handler())

	badKey := createArgs()
	badKey["psk"] = "has space in-it-0123456789"
	res := callTool(t, cs, "user.vpn_ipsec.create", badKey)
	if !res.IsError || strings.Contains(resultText(t, res), "has space") || !strings.Contains(resultText(t, res), "psk") {
		t.Fatalf("bad key: IsError=%v text=%q", res.IsError, resultText(t, res))
	}

	cases := map[string]func(a map[string]any){
		"name":                func(a map[string]any) { a["name"] = "Office_HQ" },
		"remote_ts":           func(a map[string]any) { a["remote_ts"] = []string{"0.0.0.0/0"} },
		"empty remote_ts":     func(a map[string]any) { a["remote_ts"] = []string{} },
		"ike_version":         func(a map[string]any) { a["ike_version"] = 3 },
		"ikev1 needs compat":  func(a map[string]any) { a["ike_version"] = 1 },
		"role":                func(a map[string]any) { a["role"] = "both" },
		"dpd_delay":           func(a map[string]any) { a["dpd_delay"] = 301 },
		"child above ike":     func(a map[string]any) { a["child_lifetime"] = 20000 },
		"custom no proposals": func(a map[string]any) { a["proposal_preset"] = "custom" },
		"control character":   func(a map[string]any) { a["local_id"] = "a\nb" },
		"initiator no peer":   func(a map[string]any) { delete(a, "remote_address") },
	}
	for name, mutate := range cases {
		a := createArgs()
		mutate(a)
		if res := callTool(t, cs, "user.vpn_ipsec.create", a); !res.IsError {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if len(m.posts) != 0 {
		t.Errorf("validation let %d requests through", len(m.posts))
	}
}

func TestVPNIpsec_UpdateSendsOnlyTheChangedFields(t *testing.T) {
	m := &ipsecMock{exists: true, conn: map[string]any{"name": "office-hq", "local_id": "x"}}
	cs := connectSession(t, m.handler())

	var out tools.VpnIpsecConnectionResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.update", map[string]any{
		"gateway_id": ipsecGW, "connection_id": ipsecConn, "ike_lifetime": 7200, "clear": []string{"local_id"},
	}), &out)
	if !out.Applied {
		t.Fatalf("update result = %+v", out)
	}
	if len(m.patches) != 1 || len(m.patches[0]) != 2 || m.patches[0]["ike_lifetime"] != float64(7200) {
		t.Fatalf("PATCH bodies = %v", m.patches)
	}
	if v, has := m.patches[0]["local_id"]; !has || v != nil {
		t.Errorf("clear must send null for a nullable field: %v", m.patches[0])
	}

	// key rotation sends only the key
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.update", map[string]any{
		"gateway_id": ipsecGW, "connection_id": ipsecConn, "psk": "another-Secret-Key-2026!",
	}), &out)
	if len(m.patches) != 2 || len(m.patches[1]) != 1 || m.patches[1]["psk"] != "another-Secret-Key-2026!" {
		t.Fatalf("PATCH bodies = %v", m.patches)
	}

	if res := callTool(t, cs, "user.vpn_ipsec.update", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn}); !res.IsError {
		t.Error("an update with no fields must be refused")
	}
}

func TestVPNIpsec_DeleteConfirmGateAndGatewayConfirmation(t *testing.T) {
	m := &ipsecMock{exists: true, conn: map[string]any{"name": "office-hq"}}
	cs := connectSession(t, m.handler())

	res := callTool(t, cs, "user.vpn_ipsec.delete", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	if m.deleted {
		t.Fatal("the unconfirmed delete reached the API")
	}
	var out tools.VpnIpsecDeleteResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.delete", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn, "confirm": true}), &out)
	if !out.Deleted || !out.Confirmed {
		t.Fatalf("delete result = %+v; want deleted and confirmed by the gateway", out)
	}
	if m.applied != m.generation {
		t.Errorf("the delete returned before the gateway confirmed (applied %d, generation %d)", m.applied, m.generation)
	}
}

func TestVPNIpsec_ListGetRestartRemoteSettings(t *testing.T) {
	m := &ipsecMock{exists: true, conn: map[string]any{"name": "office-hq"}, leakPSK: true}
	cs := connectSession(t, m.handler())

	var list tools.VpnIpsecListResult
	res := callTool(t, cs, "user.vpn_ipsec.list", map[string]any{"gateway_id": ipsecGW})
	unmarshalResult(t, res, &list)
	if len(list.Connections) != 1 || list.Gateway["ipsec_state"] != "ready" || strings.Contains(resultText(t, res), ipsecKey) {
		t.Fatalf("list = %+v / %s", list, resultText(t, res))
	}

	var get tools.VpnIpsecConnectionResult
	res = callTool(t, cs, "user.vpn_ipsec.get", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn})
	unmarshalResult(t, res, &get)
	if get.Connection["id"] != ipsecConn || strings.Contains(resultText(t, res), ipsecKey) {
		t.Fatalf("get = %+v", get)
	}

	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.restart", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn}), &get)
	res = callTool(t, cs, "user.vpn_ipsec.restart", map[string]any{"gateway_id": ipsecGW, "connection_id": "unconfirmed"})
	if !res.IsError || !strings.Contains(resultText(t, res), "node_dispatch_unconfirmed") {
		t.Fatalf("restart 503 should surface its reason, got %q", resultText(t, res))
	}

	var rs tools.VpnIpsecRemoteSettingsResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.remote_settings", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn}), &rs)
	if rs.RemoteSettings["our_public_ip"] != "203.0.113.10" {
		t.Errorf("remote settings = %+v", rs)
	}
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.remote_settings", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn, "format": "text"}), &rs)
	if !strings.Contains(rs.Text, "Our public IP") {
		t.Errorf("text settings = %+v", rs)
	}
	if res := callTool(t, cs, "user.vpn_ipsec.remote_settings", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn, "format": "yaml"}); !res.IsError {
		t.Error("an unknown format must be refused")
	}
}

func TestVPNIpsec_ToolDescriptionsNeverCarryAKey(t *testing.T) {
	cs := connectSession(t, (&ipsecMock{}).handler())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tl := range res.Tools {
		if !strings.HasPrefix(tl.Name, "user.vpn_ipsec.") {
			continue
		}
		seen++
		if strings.Contains(tl.Description, ipsecKey) {
			t.Errorf("%s description carries a key", tl.Name)
		}
	}
	if seen != 7 {
		t.Errorf("registered %d user.vpn_ipsec tools; want 7", seen)
	}
}

func TestVPNIpsec_RateLimitIsRetriedNotUnknown(t *testing.T) {
	m := &ipsecMock{tooManyLeft: 2}
	cs := connectSession(t, m.handler())
	var out tools.VpnIpsecConnectionResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.create", createArgs()), &out)
	if !out.Applied || len(m.posts) != 3 {
		t.Fatalf("result %+v after %d POSTs; want applied after two 429s and one recorded create", out, len(m.posts))
	}
}
