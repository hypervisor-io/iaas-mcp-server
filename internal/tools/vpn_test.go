package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func vpnMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	polls := 0

	mux.HandleFunc("POST /vpc/{vpcId}/vpn-gateway", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "gateway": map[string]any{"id": "gw-1", "status": "deploying"}})
	})
	mux.HandleFunc("GET /vpn-gateway/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polls++
		status := "active"
		if polls < 2 {
			status = "deploying"
		}
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"gateway": map[string]any{"id": r.PathValue("id"), "status": status, "public_key": "pk"},
		})
	})
	mux.HandleFunc("DELETE /vpn-gateway/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("POST /vpn-gateway/{id}/peer", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "peer": map[string]any{"id": "peer-1", "type": "road_warrior"}})
	})
	mux.HandleFunc("GET /vpn-gateway/{id}/peer/{peerId}/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[Interface]\nPrivateKey = [YOUR_PRIVATE_KEY]\n"))
	})
	mux.HandleFunc("POST /vpn-gateway/{id}/peering", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"peers":   []any{map[string]any{"id": "pr-1", "type": "vpc_peering"}},
		})
	})
	return mux
}

func TestVPN_GatewayConvergesPeerAndConfig(t *testing.T) {
	cs := connectSession(t, vpnMock())

	res := callTool(t, cs, "user.vpn_gateway.create", map[string]any{
		"vpc_id": "vpc-1", "vpngw_plan_id": "vp-1", "vpc_subnet_id": "sub-1",
	})
	var gw tools.VpnGatewayResult
	unmarshalResult(t, res, &gw)
	if gw.Gateway["id"] != "gw-1" || gw.Gateway["status"] != "active" {
		t.Fatalf("create gateway = %v, want gw-1/active", gw.Gateway)
	}

	res = callTool(t, cs, "user.vpn_gateway.add_peer", map[string]any{"gateway_id": "gw-1", "type": "road_warrior"})
	var peer tools.VpnPeerResult
	unmarshalResult(t, res, &peer)
	if peer.Peer["id"] != "peer-1" {
		t.Errorf("add_peer id = %v, want peer-1", peer.Peer["id"])
	}

	res = callTool(t, cs, "user.vpn_gateway.peer_config", map[string]any{"gateway_id": "gw-1", "peer_id": "peer-1"})
	var cfg tools.VpnPeerConfigResult
	unmarshalResult(t, res, &cfg)
	if !strings.Contains(cfg.Config, "[Interface]") {
		t.Errorf("peer_config = %q, want a WireGuard config", cfg.Config)
	}

	res = callTool(t, cs, "user.vpn_peering.create", map[string]any{"gateway_id": "gw-1", "remote_gateway_id": "gw-2"})
	var peering tools.VpnPeeringResult
	unmarshalResult(t, res, &peering)
	if peering.Peering["id"] != "pr-1" {
		t.Errorf("peering create id = %v, want pr-1", peering.Peering["id"])
	}
}

func TestVPN_GatewayDeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, vpnMock())
	res := callTool(t, cs, "user.vpn_gateway.delete", map[string]any{"id": "gw-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.vpn_gateway.delete", map[string]any{"id": "gw-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
