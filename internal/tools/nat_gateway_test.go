package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func natGatewayMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	polls := 0

	mux.HandleFunc("POST /vpc/{vpcId}/nat-gateway", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "gateway": map[string]any{"id": "nat-1", "status": "provisioning"},
		})
	})
	mux.HandleFunc("GET /vpc/{vpcId}/nat-gateway/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "NAT gateway not found"})
			return
		}
		mu.Lock()
		polls++
		status := "active"
		if polls < 2 {
			status = "provisioning"
		}
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"gateway": map[string]any{"id": id, "status": status, "public_ip": map[string]any{"ip": "203.0.113.9"}},
		})
	})
	mux.HandleFunc("DELETE /vpc/{vpcId}/nat-gateway/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("POST /vpc/{vpcId}/nat-gateway/{id}/subnet", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "gateway": map[string]any{"id": r.PathValue("id"), "status": "active"}})
	})
	return mux
}

func TestNatGateway_CreateConvergesAndActions(t *testing.T) {
	cs := connectSession(t, natGatewayMock())

	res := callTool(t, cs, "user.nat_gateway.create", map[string]any{"vpc_id": "vpc-1", "name": "gw"})
	var created tools.NatGatewayResult
	unmarshalResult(t, res, &created)
	if created.Gateway["id"] != "nat-1" {
		t.Fatalf("create id = %v, want nat-1", created.Gateway["id"])
	}
	if created.Gateway["status"] != "active" {
		t.Errorf("create status = %v, want active (converged)", created.Gateway["status"])
	}

	res = callTool(t, cs, "user.nat_gateway.attach_subnet", map[string]any{"vpc_id": "vpc-1", "id": "nat-1", "subnet_id": "sub-1"})
	var attached tools.NatGatewayResult
	unmarshalResult(t, res, &attached)
	if attached.Gateway["id"] != "nat-1" {
		t.Errorf("attach_subnet id = %v, want nat-1", attached.Gateway["id"])
	}
}

func TestNatGateway_DeleteConfirmAndErrors(t *testing.T) {
	cs := connectSession(t, natGatewayMock())
	res := callTool(t, cs, "user.nat_gateway.delete", map[string]any{"vpc_id": "vpc-1", "id": "nat-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.nat_gateway.delete", map[string]any{"vpc_id": "vpc-1", "id": "nat-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
	res = callTool(t, cs, "user.nat_gateway.get", map[string]any{"vpc_id": "vpc-1", "id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
}
