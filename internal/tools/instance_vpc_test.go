package tools_test

import (
	"net/http"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func instanceVpcMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /instance/{id}/vpc/disable", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "disabled"})
	})
	mux.HandleFunc("GET /instance/{id}/vpc/ips", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "vip-1", "ip": "10.0.0.2", "is_primary": true}},
		})
	})
	mux.HandleFunc("GET /instance/{id}/vpc/available-ips", func(w http.ResponseWriter, r *http.Request) {
		// bare array shape
		writeJSON(w, http.StatusOK, []any{map[string]any{"id": "vip-9", "ip": "10.0.0.9"}})
	})
	mux.HandleFunc("POST /instance/{id}/vpc/ip/add", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "vpc_ip": map[string]any{"id": "vip-9", "ip": "10.0.0.9", "is_primary": false},
		})
	})
	mux.HandleFunc("POST /instance/{id}/vpc/ip/{vpcIpId}/primary", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "primary set"})
	})
	mux.HandleFunc("DELETE /instance/{id}/vpc/ip/{vpcIpId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "removed"})
	})
	return mux
}

func TestInstanceVpc_IPManagement(t *testing.T) {
	cs := connectSession(t, instanceVpcMock())

	res := callTool(t, cs, "user.instance_vpc.list_ips", map[string]any{"instance_id": "inst-1"})
	var ips tools.VpcIPListResult
	unmarshalResult(t, res, &ips)
	if ips.Count != 1 || ips.IPs[0]["ip"] != "10.0.0.2" {
		t.Fatalf("list_ips = %v, want one 10.0.0.2", ips.IPs)
	}

	res = callTool(t, cs, "user.instance_vpc.list_available_ips", map[string]any{"instance_id": "inst-1"})
	var avail tools.VpcIPListResult
	unmarshalResult(t, res, &avail)
	if avail.Count != 1 || avail.IPs[0]["id"] != "vip-9" {
		t.Errorf("list_available_ips = %v, want one vip-9", avail.IPs)
	}

	res = callTool(t, cs, "user.instance_vpc.add_ip", map[string]any{"instance_id": "inst-1", "ip_id": "vip-9"})
	var added tools.VpcIPResult
	unmarshalResult(t, res, &added)
	if added.VpcIP["id"] != "vip-9" {
		t.Errorf("add_ip id = %v, want vip-9", added.VpcIP["id"])
	}

	res = callTool(t, cs, "user.instance_vpc.set_primary_ip", map[string]any{"instance_id": "inst-1", "vpc_ip_id": "vip-9"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("set_primary_ip ok = false")
	}

	res = callTool(t, cs, "user.instance_vpc.disable", map[string]any{"instance_id": "inst-1"})
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("disable ok = false")
	}
}

func TestInstanceVpc_AddIPRequiresChoice(t *testing.T) {
	cs := connectSession(t, instanceVpcMock())
	// Neither ip_id nor random -> handler-level error.
	res := callTool(t, cs, "user.instance_vpc.add_ip", map[string]any{"instance_id": "inst-1"})
	if !res.IsError {
		t.Fatalf("add_ip with no ip_id/random should error")
	}
}
