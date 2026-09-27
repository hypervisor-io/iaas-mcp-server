package tools_test

import (
	"strings"
	"testing"
)

// This file proves the validate.go helpers are actually WIRED into the tool
// handlers that need them (vpc.create's cidr, vpn_gateway.create's
// tunnel_subnet, vpn_gateway.add_peer/update_peer's control-character
// fields), not just unit-tested in isolation. Each case runs against
// permissiveMock(), which answers {"success":true} to ANY request - so an
// IsError result can only come from validation that ran BEFORE the HTTP
// call, exactly like the confirm gate tests in confirm_gate_test.go.

func TestValidateWiring_VPCCreateRejectsBadCidr(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpc.create", map[string]any{
		"name":        "prod",
		"cidr":        "10.99.0.0/24 -j MASQUERADE",
		"location_id": "loc-1",
	})
	if !res.IsError {
		t.Fatal("user.vpc.create with an injected cidr suffix should be refused, got a success result")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "cidr") {
		t.Errorf("refusal = %q, want it to name cidr", msg)
	}
}

func TestValidateWiring_VPCCreateAcceptsGoodCidr(t *testing.T) {
	mock := &locationIDMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.vpc.create", map[string]any{
		"name":        "prod",
		"cidr":        "10.0.0.0/24",
		"location_id": "loc-1",
	})
	if res.IsError {
		t.Fatalf("user.vpc.create with a valid cidr should succeed, got error: %s", resultText(t, res))
	}
}

func TestValidateWiring_VpnGatewayCreateRejectsBadTunnelSubnet(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpn_gateway.create", map[string]any{
		"vpc_id":        "vpc-1",
		"vpngw_plan_id": "plan-1",
		"vpc_subnet_id": "sub-1",
		"tunnel_subnet": "10.99.0.0",
	})
	if !res.IsError {
		t.Fatal("user.vpn_gateway.create with a bare tunnel_subnet address (no prefix) should be refused")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "tunnel_subnet") {
		t.Errorf("refusal = %q, want it to name tunnel_subnet", msg)
	}
}

func TestValidateWiring_VpnGatewayCreateOmittedTunnelSubnetIsFine(t *testing.T) {
	cs := connectSession(t, vpnMock())

	res := callTool(t, cs, "user.vpn_gateway.create", map[string]any{
		"vpc_id":        "vpc-1",
		"vpngw_plan_id": "plan-1",
		"vpc_subnet_id": "sub-1",
	})
	if res.IsError {
		t.Fatalf("omitting tunnel_subnet should still succeed, got error: %s", resultText(t, res))
	}
}

func TestValidateWiring_AddVpnPeerRejectsControlCharacterInName(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpn_gateway.add_peer", map[string]any{
		"gateway_id": "gw-1",
		"type":       "road_warrior",
		"name":       "peer\nname",
	})
	if !res.IsError {
		t.Fatal("add_peer with a newline in name should be refused")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "name") {
		t.Errorf("refusal = %q, want it to name the field", msg)
	}
}

func TestValidateWiring_AddVpnPeerRejectsControlCharacterInAllowedIP(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpn_gateway.add_peer", map[string]any{
		"gateway_id":  "gw-1",
		"type":        "site_to_site",
		"allowed_ips": []string{"10.0.0.0/24;\rrm -rf /"},
	})
	if !res.IsError {
		t.Fatal("add_peer with a control character inside an allowed_ips element should be refused")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "allowed_ips") {
		t.Errorf("refusal = %q, want it to name allowed_ips", msg)
	}
}

func TestValidateWiring_AddVpnPeerRejectsControlCharacterInEveryStringField(t *testing.T) {
	fields := map[string]string{
		"name":          "name",
		"endpoint":      "endpoint",
		"preshared_key": "preshared_key",
		"public_key":    "public_key",
		"dns":           "dns",
	}

	for jsonKey, wantField := range fields {
		jsonKey, wantField := jsonKey, wantField

		t.Run(jsonKey, func(t *testing.T) {
			cs := connectSession(t, permissiveMock())

			res := callTool(t, cs, "user.vpn_gateway.add_peer", map[string]any{
				"gateway_id": "gw-1",
				"type":       "site_to_site",
				jsonKey:      "bad\x00value",
			})
			if !res.IsError {
				t.Fatalf("add_peer with a control character in %s should be refused", jsonKey)
			}
			if msg := resultText(t, res); !strings.Contains(msg, wantField) {
				t.Errorf("refusal = %q, want it to name %s", msg, wantField)
			}
		})
	}
}

func TestValidateWiring_AddVpnPeerAcceptsCleanInput(t *testing.T) {
	cs := connectSession(t, vpnMock())

	res := callTool(t, cs, "user.vpn_gateway.add_peer", map[string]any{
		"gateway_id":  "gw-1",
		"type":        "road_warrior",
		"name":        "office-peer",
		"allowed_ips": []string{"10.0.0.0/24"},
	})
	if res.IsError {
		t.Fatalf("clean add_peer input should succeed, got error: %s", resultText(t, res))
	}
}

func TestValidateWiring_UpdateVpnPeerRejectsControlCharacterInEndpoint(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpn_gateway.update_peer", map[string]any{
		"gateway_id": "gw-1",
		"peer_id":    "peer-1",
		"endpoint":   "203.0.113.1:51820\r\nX-Injected: 1",
	})
	if !res.IsError {
		t.Fatal("update_peer with a control character in endpoint should be refused")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "endpoint") {
		t.Errorf("refusal = %q, want it to name endpoint", msg)
	}
}

func TestValidateWiring_UpdateVpnPeerRejectsControlCharacterInAllowedIP(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpn_gateway.update_peer", map[string]any{
		"gateway_id":  "gw-1",
		"peer_id":     "peer-1",
		"allowed_ips": []string{"10.0.0.0/24\x7f"},
	})
	if !res.IsError {
		t.Fatal("update_peer with a control character inside an allowed_ips element should be refused")
	}
	if msg := resultText(t, res); !strings.Contains(msg, "allowed_ips") {
		t.Errorf("refusal = %q, want it to name allowed_ips", msg)
	}
}

func TestValidateWiring_UpdateVpnPeerAcceptsCleanInput(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	res := callTool(t, cs, "user.vpn_gateway.update_peer", map[string]any{
		"gateway_id": "gw-1",
		"peer_id":    "peer-1",
		"name":       "renamed-peer",
	})
	if res.IsError {
		t.Fatalf("clean update_peer input should succeed, got error: %s", resultText(t, res))
	}
}
