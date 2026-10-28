package tools_test

// These HTTP fixtures pin the user API at Master
// 255ba16d582e917e9a7c2f8614ae482a2b4e478e: IpsecPresenter,
// IpsecConnectionService, IpsecErrorResponse and VpnGatewayService.
// connectSession uses httptest.NewServer; no platform service is needed.
import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func contractIPsecConnection() map[string]any {
	return map[string]any{
		"id": ipsecConn, "vpn_gateway_id": ipsecGW, "name": "office-hq", "conn_name": "vc-0123456789ab",
		"remote_address": nil, "local_id_type": "address", "local_id": nil,
		"remote_id_type": "fqdn", "remote_id": "office.example", "psk_version": 2,
		"ike_version": 2, "proposal_preset": "modern", "ike_proposal": nil, "esp_proposal": nil,
		"local_ts": []string{"10.60.0.0/16"}, "remote_ts": []string{"192.168.50.0/24"},
		"role": "responder", "dpd_delay": 30, "dpd_action": "clear", "ike_lifetime": 14400,
		"child_lifetime": 3600, "udp_encapsulation": true, "enabled": true,
		"sa_state": "partial", "apply_state": "applied", "children_expected": 2, "children_installed": 1,
		"remote_host": "198.51.100.7", "established_at": nil, "bytes_in": 1234, "bytes_out": 5678,
		"last_error_code": "psk_mismatch", "last_error": "The shared keys do not match. Update both peers, then restart the connection.",
		"last_error_at": "2026-10-27T10:00:00+00:00", "status_checked_at": "2026-10-27T10:01:00+00:00",
		"deleted_at": nil, "created_at": "2026-10-27T09:00:00+00:00", "updated_at": "2026-10-27T10:00:00+00:00",
	}
}

func TestVPNIpsec_PinnedPresenterAndResponderDiagnostics(t *testing.T) {
	connection := contractIPsecConnection()
	gateway := map[string]any{
		"ipsec_state": "ready", "ipsec_last_error_code": "no_proposal_chosen",
		"ipsec_last_error_at": "2026-10-27T10:01:00+00:00", "ipsec_version": nil, "ipsec_error": nil,
		"ipsec_apply_state": "idle", "ipsec_apply_error": nil, "ipsec_generation": 3, "ipsec_applied_generation": 3,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vpn-gateway/{gw}/ipsec", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "connections": []any{connection}, "gateway": gateway})
	})
	mux.HandleFunc("GET /vpn-gateway/{gw}/ipsec/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "connection": connection})
	})
	cs := connectSession(t, mux)
	var list tools.VpnIpsecListResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.list", map[string]any{"gateway_id": ipsecGW}), &list)
	if len(list.Connections) != 1 {
		t.Fatalf("connections = %v", list.Connections)
	}
	assertJSONEqual(t, list.Connections[0], connection)
	assertJSONEqual(t, list.Gateway, gateway)
	var get tools.VpnIpsecConnectionResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.get", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn}), &get)
	assertJSONEqual(t, get.Connection, connection)
	if !get.Applied || get.Connection["sa_state"] != "partial" {
		t.Fatalf("applied configuration must not imply an established tunnel: %+v", get)
	}
}

func assertJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("JSON mismatch\ngot  %s\nwant %s", g, w)
	}
}

func TestVPNIpsec_RemoteSettingsPreserveEverySubnetRoute(t *testing.T) {
	settings := map[string]any{
		"our_public_ip": "203.0.113.10", "our_identity": map[string]any{"type": "address", "value": "203.0.113.10"},
		"your_identity": map[string]any{"type": "fqdn", "value": "office.example"},
		"ike_version":   2, "mode": "tunnel", "auth": "psk", "local_networks": []string{"10.60.0.0/16"},
		"remote_networks": []string{"192.168.50.0/24"}, "ike_lifetime": 14400, "child_lifetime": 3600,
		"pfs": true, "nat_traversal": "forced", "dpd": map[string]any{"delay": 30, "action": "clear"},
		"ike_proposals": []any{map[string]any{"encryption": "aes256gcm16", "integrity": nil, "prf": "prfsha384", "dh_group": "ecp384", "dh_group_number": "20"}},
		"esp_proposals": []any{}, "vendor_notes": []string{"Use pre-shared key authentication with the key you entered here; it is never shown again."},
		"instance_route_hint": map[string]any{"commands": []any{
			map[string]any{"subnet": "web", "cidr": "10.60.1.0/24", "gateway": "10.60.1.1", "command": "ip route add 192.168.50.0/24 via 10.60.1.1"},
			map[string]any{"subnet": "db", "cidr": "10.60.2.0/24", "gateway": "10.60.2.1", "command": "ip route add 192.168.50.0/24 via 10.60.2.1"},
		}, "note": "Instances started after this change get the route automatically (Linux). Running instances with a public IP need it added once: ip route add <remote> via <subnet gateway>. Windows: reinstall, or add it with route -p add."},
	}
	const plain = "Our public IP: 203.0.113.10\nOur identity: address 203.0.113.10\nYour identity: fqdn office.example\nIKE version: 2\nMode: tunnel, authentication: pre-shared key (the key you entered)\nNAT traversal: forced\nOur networks: 10.60.0.0/16\nYour networks: 192.168.50.0/24\nIKE lifetime: 14400 s, child lifetime: 3600 s\nIKE proposal: aes256gcm16 prfsha384 ecp384\n"
	cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/vpn-gateway/"+ipsecGW+"/ipsec/"+ipsecConn+"/remote-settings" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if r.URL.Query().Get("format") == "text" {
			w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
			_, _ = w.Write([]byte(plain))
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "remote_settings": settings})
	}))
	var out tools.VpnIpsecRemoteSettingsResult
	args := map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn}
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.remote_settings", args), &out)
	assertJSONEqual(t, out.RemoteSettings, settings)
	args["format"] = "text"
	unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.remote_settings", args), &out)
	if out.Text != plain {
		t.Fatalf("text response changed: %q", out.Text)
	}
}

func TestVPNIpsec_RealRefusalEnvelopesAndRestartOnce(t *testing.T) {
	cases := []struct {
		status                int
		reason, message, tool string
		extra                 map[string]any
	}{
		{409, "gateway_not_active", "The VPN gateway is not active.", "update", nil},
		{409, "ipsec_install_failed", "IPsec could not be set up on this gateway.", "update", nil},
		{409, "name_taken", "A connection with this name already exists on the gateway.", "update", nil},
		{409, "remote_identity_in_use", "Another connection on this gateway already uses this remote identity.", "update", nil},
		{409, "node_tls_unpinned", "This gateway's node must be updated by your provider before IPsec can be used.", "update", nil},
		{409, "node_upgrade_required", "This gateway's node must be updated by your provider before IPsec can be used.", "update", nil},
		{409, "connection_limit_reached", "This gateway already has the maximum number of IPsec connections (16).", "update", nil},
		{409, "connection_disabled", "Enable the connection before restarting it.", "restart", nil},
		{422, "invalid_psk", "The pre-shared key must be 16-128 printable ASCII characters without spaces, double quotes or backslashes.", "update", map[string]any{"errors": map[string]any{"psk": []string{"The pre-shared key must be 16-128 printable ASCII characters without spaces, double quotes or backslashes."}}}},
		{422, "responder_requires_remote_id", "A responder needs a remote identity.", "update", map[string]any{"errors": map[string]any{"remote_id": []string{"A responder needs a remote identity."}}}},
		{422, "destination_overlap", "The range 192.168.50.0/24 overlaps an existing ipsec destination of this gateway.", "update", map[string]any{"data": map[string]any{"kind": "ipsec", "cidr": "192.168.50.0/24"}}},
		{422, "validation_failed", "The remote_ts field must have at least 1 items.", "update", map[string]any{"errors": map[string]any{"remote_ts": []string{"The remote_ts field must have at least 1 items."}}}},
		{503, "node_dispatch_unconfirmed", "The gateway did not confirm the request yet; it will be retried automatically.", "restart", nil},
		{404, "not_found", "IPsec connection not found.", "get", nil},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			calls := 0
			cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body := map[string]any{"success": false, "reason": tc.reason, "message": tc.message}
				for key, value := range tc.extra {
					body[key] = value
				}
				writeJSON(w, tc.status, body)
			}))
			args := map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn}
			if tc.tool == "update" {
				args["ike_lifetime"], args["wait"] = 7200, false
			}
			res := callTool(t, cs, "user.vpn_ipsec."+tc.tool, args)
			if !res.IsError || !strings.Contains(resultText(t, res), tc.reason) || !strings.Contains(resultText(t, res), tc.message) {
				t.Fatalf("lost refusal: %q", resultText(t, res))
			}
			if calls != 1 {
				t.Fatalf("refused or uncertain operation replayed %d times", calls)
			}
		})
	}
}

func TestVPNIpsec_RecursivePSKSuppression(t *testing.T) {
	cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"success": true, "connections": []any{map[string]any{"id": ipsecConn, "psk_version": 2, "nested": []any{map[string]any{"PSK": ipsecKey, "safe": "kept"}}}}, "gateway": map[string]any{"psk": ipsecKey}})
	}))
	res := callTool(t, cs, "user.vpn_ipsec.list", map[string]any{"gateway_id": ipsecGW})
	if res.IsError || strings.Contains(resultText(t, res), ipsecKey) {
		t.Fatalf("secret escaped: %q", resultText(t, res))
	}
	var list tools.VpnIpsecListResult
	unmarshalResult(t, res, &list)
	if list.Connections[0]["psk_version"] != float64(2) || len(list.Gateway) != 0 {
		t.Fatalf("public metadata or gateway scrub drift: %+v", list)
	}
}

func TestVPNIpsec_WriteErrorNeverEchoesSubmittedKey(t *testing.T) {
	for _, tool := range []string{"create", "update"} {
		t.Run(tool, func(t *testing.T) {
			cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					writeJSON(w, 200, map[string]any{"success": true, "connections": []any{}})
					return
				}
				writeJSON(w, 422, map[string]any{"success": false, "reason": "invalid_psk", "message": "Invalid key " + ipsecKey, "errors": map[string]any{"psk": []string{"Rejected " + ipsecKey}}})
			}))
			args := createArgs()
			if tool == "update" {
				args = map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn, "psk": ipsecKey}
			}
			res := callTool(t, cs, "user.vpn_ipsec."+tool, args)
			if !res.IsError || strings.Contains(resultText(t, res), ipsecKey) || !strings.Contains(resultText(t, res), "invalid_psk") {
				t.Fatalf("unsafe error: %q", resultText(t, res))
			}
		})
	}
}

func TestVPNGateway_DeleteRefusalRetainsResourceAndRetryIsDelete(t *testing.T) {
	deletes := 0
	cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "DELETE":
			deletes++
			if deletes == 1 {
				writeJSON(w, 409, map[string]any{"success": false, "reason": "vpn_gateway_destroy_failed", "message": "The VPN gateway could not be deleted and has been kept; try deleting it again."})
				return
			}
			writeJSON(w, 200, map[string]any{"success": true})
		case "GET":
			writeJSON(w, 200, map[string]any{"success": true, "gateway": map[string]any{"id": ipsecGW, "status": "active", "metadata": map[string]any{"destroy_failed": true, "destroy_failed_at": "2026-10-27T10:00:00+00:00"}}})
		default:
			t.Errorf("unexpected request %s", r.Method)
		}
	}))
	args := map[string]any{"id": ipsecGW, "confirm": true}
	res := callTool(t, cs, "user.vpn_gateway.delete", args)
	if !res.IsError || !strings.Contains(resultText(t, res), "vpn_gateway_destroy_failed") || deletes != 1 {
		t.Fatalf("delete refusal: %q, calls %d", resultText(t, res), deletes)
	}
	var gateway tools.VpnGatewayResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_gateway.get", map[string]any{"id": ipsecGW}), &gateway)
	if gateway.Gateway["status"] != "active" {
		t.Fatalf("gateway lost on refusal: %+v", gateway)
	}
	var deleted tools.DeleteResult
	unmarshalResult(t, callTool(t, cs, "user.vpn_gateway.delete", args), &deleted)
	if !deleted.Deleted || deletes != 2 {
		t.Fatalf("explicit DELETE retry: %+v, %d", deleted, deletes)
	}
}

func TestVPNIpsec_AllWriteFieldsAndNullableClears(t *testing.T) {
	fields := map[string]any{
		"name": "office-hq", "psk": ipsecKey, "remote_address": "198.51.100.7",
		"local_id_type": "fqdn", "local_id": "gateway.example", "remote_id_type": "fqdn", "remote_id": "office.example",
		"ike_version": 2, "proposal_preset": "custom", "ike_proposal": "aes256gcm16-prfsha384-ecp384", "esp_proposal": "aes256gcm16-ecp384",
		"local_ts": []string{"10.60.1.0/24"}, "remote_ts": []string{"192.168.50.0/24"}, "role": "responder",
		"dpd_delay": 0, "dpd_action": "clear", "ike_lifetime": 14400, "child_lifetime": 3600, "udp_encapsulation": false, "enabled": false,
	}
	for _, tool := range []string{"create", "update"} {
		t.Run(tool, func(t *testing.T) {
			writes := 0
			cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					writeJSON(w, 200, map[string]any{"success": true, "connections": []any{}})
					return
				}
				writes++
				method, path, status := "POST", "/vpn-gateway/"+ipsecGW+"/ipsec", 201
				if tool == "update" {
					method, path, status = "PATCH", path+"/"+ipsecConn, 200
				}
				if r.Method != method || r.URL.Path != path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if writes == 1 {
					assertJSONEqual(t, body, fields)
				} else {
					assertJSONEqual(t, body, map[string]any{"remote_address": nil, "local_id": nil, "remote_id": nil, "ike_proposal": nil, "esp_proposal": nil})
				}
				writeJSON(w, status, map[string]any{"success": true, "message": "IPsec connection updated.", "connection": contractIPsecConnection(), "warnings": []string{"security_group_blocks_esp"}})
			}))
			args := map[string]any{"gateway_id": ipsecGW, "wait": false}
			for key, value := range fields {
				args[key] = value
			}
			if tool == "update" {
				args["connection_id"] = ipsecConn
			}
			var out tools.VpnIpsecConnectionResult
			unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec."+tool, args), &out)
			assertJSONEqual(t, out.Connection, contractIPsecConnection())
			if len(out.Warnings) != 1 || out.Warnings[0] != "security_group_blocks_esp" || writes != 1 {
				t.Fatalf("write result %+v, writes %d", out, writes)
			}
			if tool == "update" {
				unmarshalResult(t, callTool(t, cs, "user.vpn_ipsec.update", map[string]any{"gateway_id": ipsecGW, "connection_id": ipsecConn, "wait": false, "clear": []string{"remote_address", "local_id", "remote_id", "ike_proposal", "esp_proposal"}}), &out)
				if writes != 2 {
					t.Fatalf("nullable clear requests = %d", writes)
				}
			}
		})
	}
}

func TestVPNIpsec_InstallingReasonWithout409DoesNotReplayCreate(t *testing.T) {
	posts := 0
	cs := connectSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			writeJSON(w, 200, map[string]any{"success": true, "connections": []any{}})
			return
		}
		posts++
		if posts == 1 {
			writeJSON(w, 500, map[string]any{"success": false, "reason": "ipsec_installing", "message": "The request could not be completed."})
			return
		}
		writeJSON(w, 201, map[string]any{"success": true, "connection": contractIPsecConnection(), "warnings": []any{}})
	}))
	args := createArgs()
	args["wait"] = false
	res := callTool(t, cs, "user.vpn_ipsec.create", args)
	if !res.IsError || !strings.Contains(resultText(t, res), "no definite answer") || posts != 1 {
		t.Fatalf("an uncertain create was replayed: calls=%d, response=%q", posts, resultText(t, res))
	}
}
