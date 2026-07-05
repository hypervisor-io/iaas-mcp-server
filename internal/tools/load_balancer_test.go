package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func loadBalancerMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	polls := 0
	deleted := false

	mux.HandleFunc("POST /load-balancers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "load_balancer": map[string]any{"id": "lb-1", "status": "deploying"}})
	})
	mux.HandleFunc("GET /load-balancers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":        true,
			"load_balancers": map[string]any{"current_page": 1, "last_page": 1, "data": []any{map[string]any{"id": "lb-1", "name": "web"}}},
		})
	})
	mux.HandleFunc("GET /load-balancer/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gone := deleted
		polls++
		status := "active"
		if polls < 2 {
			status = "deploying"
		}
		mu.Unlock()
		if gone {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Load balancer not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "load_balancer": map[string]any{"id": r.PathValue("id"), "status": status, "public_ip": map[string]any{"ip": "203.0.113.20"}},
		})
	})
	mux.HandleFunc("DELETE /load-balancer/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = true
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "queued"})
	})
	mux.HandleFunc("POST /load-balancer/{id}/frontends", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "frontend": map[string]any{"id": "fe-1", "port": 443}})
	})
	mux.HandleFunc("POST /load-balancer/{id}/backends", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "backend": map[string]any{"id": "be-1", "name": "web"}})
	})
	mux.HandleFunc("POST /load-balancer/{id}/backend/{bid}/targets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "target": map[string]any{"id": "tg-1", "target_ip": "10.0.0.5"}})
	})
	mux.HandleFunc("POST /load-balancer/{id}/frontend/{fid}/rules", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "rule": map[string]any{"id": "rl-1"}})
	})
	return mux
}

func TestLoadBalancer_CreateConvergesAndChildren(t *testing.T) {
	cs := connectSession(t, loadBalancerMock())

	res := callTool(t, cs, "user.load_balancer.create", map[string]any{"name": "web", "lb_plan_id": "lp-1", "hypervisor_group_id": "hg-1"})
	var lb tools.LoadBalancerResult
	unmarshalResult(t, res, &lb)
	if lb.LoadBalancer["id"] != "lb-1" || lb.LoadBalancer["status"] != "active" {
		t.Fatalf("create = %v, want lb-1/active", lb.LoadBalancer)
	}

	res = callTool(t, cs, "user.load_balancer.frontend_create", map[string]any{"load_balancer_id": "lb-1", "name": "https", "port": 443})
	var fe tools.LBFrontendResult
	unmarshalResult(t, res, &fe)
	if fe.Frontend["id"] != "fe-1" {
		t.Errorf("frontend id = %v, want fe-1", fe.Frontend["id"])
	}

	res = callTool(t, cs, "user.load_balancer.backend_create", map[string]any{"load_balancer_id": "lb-1", "name": "web"})
	var be tools.LBBackendResult
	unmarshalResult(t, res, &be)
	if be.Backend["id"] != "be-1" {
		t.Errorf("backend id = %v, want be-1", be.Backend["id"])
	}

	res = callTool(t, cs, "user.load_balancer.target_create", map[string]any{"load_balancer_id": "lb-1", "backend_id": "be-1", "target_ip": "10.0.0.5", "target_port": 8080})
	var tg tools.LBTargetResult
	unmarshalResult(t, res, &tg)
	if tg.Target["id"] != "tg-1" {
		t.Errorf("target id = %v, want tg-1", tg.Target["id"])
	}

	res = callTool(t, cs, "user.load_balancer.routing_rule_create", map[string]any{"load_balancer_id": "lb-1", "frontend_id": "fe-1", "lb_backend_id": "be-1", "match_value": "/api"})
	var rl tools.LBRoutingRuleResult
	unmarshalResult(t, res, &rl)
	if rl.Rule["id"] != "rl-1" {
		t.Errorf("rule id = %v, want rl-1", rl.Rule["id"])
	}
}

func TestLoadBalancer_DeleteConfirmConverges(t *testing.T) {
	cs := connectSession(t, loadBalancerMock())
	res := callTool(t, cs, "user.load_balancer.delete", map[string]any{"id": "lb-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.load_balancer.delete", map[string]any{"id": "lb-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not converge")
	}
}
