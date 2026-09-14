package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

type bodyRecorder struct {
	mu     sync.Mutex
	bodies map[string]map[string]any
}

func newBodyRecorder() *bodyRecorder {
	return &bodyRecorder{bodies: map[string]map[string]any{}}
}

func (b *bodyRecorder) record(key string, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body = map[string]any{}
	}
	b.mu.Lock()
	b.bodies[key] = body
	b.mu.Unlock()
}

func (b *bodyRecorder) get(key string) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bodies[key]
}

func loadBalancerMock(rec *bodyRecorder) http.Handler {
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
		rec.record("frontend_create", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "frontend": map[string]any{"id": "fe-1", "port": 443}})
	})
	mux.HandleFunc("PATCH /load-balancer/{id}/frontend/{fid}", func(w http.ResponseWriter, r *http.Request) {
		rec.record("frontend_update", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "frontend": map[string]any{"id": r.PathValue("fid"), "port": 443}})
	})
	mux.HandleFunc("POST /load-balancer/{id}/backends", func(w http.ResponseWriter, r *http.Request) {
		rec.record("backend_create", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "backend": map[string]any{"id": "be-1", "name": "web"}})
	})
	mux.HandleFunc("PATCH /load-balancer/{id}/backend/{bid}", func(w http.ResponseWriter, r *http.Request) {
		rec.record("backend_update", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "backend": map[string]any{"id": r.PathValue("bid"), "name": "web"}})
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
	rec := newBodyRecorder()
	cs := connectSession(t, loadBalancerMock(rec))

	res := callTool(t, cs, "user.load_balancer.create", map[string]any{"name": "web", "lb_plan_id": "lp-1", "hypervisor_group_id": "hg-1"})
	var lb tools.LoadBalancerResult
	unmarshalResult(t, res, &lb)
	if lb.LoadBalancer["id"] != "lb-1" || lb.LoadBalancer["status"] != "active" {
		t.Fatalf("create = %v, want lb-1/active", lb.LoadBalancer)
	}

	res = callTool(t, cs, "user.load_balancer.frontend_create", map[string]any{"load_balancer_id": "lb-1", "name": "https", "port": 443, "idle_timeout": 600})
	var fe tools.LBFrontendResult
	unmarshalResult(t, res, &fe)
	if fe.Frontend["id"] != "fe-1" {
		t.Errorf("frontend id = %v, want fe-1", fe.Frontend["id"])
	}
	if got := rec.get("frontend_create")["idle_timeout"]; got != float64(600) {
		t.Errorf("frontend_create idle_timeout = %v, want 600", got)
	}

	res = callTool(t, cs, "user.load_balancer.frontend_update", map[string]any{"load_balancer_id": "lb-1", "frontend_id": "fe-1", "idle_timeout": 900})
	unmarshalResult(t, res, &fe)
	if got := rec.get("frontend_update")["idle_timeout"]; got != float64(900) {
		t.Errorf("frontend_update idle_timeout = %v, want 900", got)
	}

	res = callTool(t, cs, "user.load_balancer.frontend_update", map[string]any{"load_balancer_id": "lb-1", "frontend_id": "fe-1", "name": "https2"})
	unmarshalResult(t, res, &fe)
	if _, ok := rec.get("frontend_update")["idle_timeout"]; ok {
		t.Errorf("frontend_update without idle_timeout sent the key anyway: %v", rec.get("frontend_update"))
	}

	res = callTool(t, cs, "user.load_balancer.backend_create", map[string]any{"load_balancer_id": "lb-1", "name": "web", "connect_timeout": 10, "server_timeout": 900})
	var be tools.LBBackendResult
	unmarshalResult(t, res, &be)
	if be.Backend["id"] != "be-1" {
		t.Errorf("backend id = %v, want be-1", be.Backend["id"])
	}
	if got := rec.get("backend_create")["connect_timeout"]; got != float64(10) {
		t.Errorf("backend_create connect_timeout = %v, want 10", got)
	}
	if got := rec.get("backend_create")["server_timeout"]; got != float64(900) {
		t.Errorf("backend_create server_timeout = %v, want 900", got)
	}

	res = callTool(t, cs, "user.load_balancer.backend_update", map[string]any{"load_balancer_id": "lb-1", "backend_id": "be-1", "connect_timeout": 20, "server_timeout": 1200})
	unmarshalResult(t, res, &be)
	if got := rec.get("backend_update")["connect_timeout"]; got != float64(20) {
		t.Errorf("backend_update connect_timeout = %v, want 20", got)
	}
	if got := rec.get("backend_update")["server_timeout"]; got != float64(1200) {
		t.Errorf("backend_update server_timeout = %v, want 1200", got)
	}

	res = callTool(t, cs, "user.load_balancer.backend_update", map[string]any{"load_balancer_id": "lb-1", "backend_id": "be-1", "name": "web2"})
	unmarshalResult(t, res, &be)
	if _, ok := rec.get("backend_update")["connect_timeout"]; ok {
		t.Errorf("backend_update without connect_timeout sent the key anyway: %v", rec.get("backend_update"))
	}
	if _, ok := rec.get("backend_update")["server_timeout"]; ok {
		t.Errorf("backend_update without server_timeout sent the key anyway: %v", rec.get("backend_update"))
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

func TestLoadBalancerFrontend_SSLRedirect(t *testing.T) {
	rec := newBodyRecorder()
	cs := connectSession(t, loadBalancerMock(rec))

	res := callTool(t, cs, "user.load_balancer.frontend_create", map[string]any{"load_balancer_id": "lb-1", "name": "http", "port": 80, "ssl_redirect": true})
	var fe tools.LBFrontendResult
	unmarshalResult(t, res, &fe)
	if got := rec.get("frontend_create")["ssl_redirect"]; got != true {
		t.Errorf("frontend_create ssl_redirect = %v, want true", got)
	}

	res = callTool(t, cs, "user.load_balancer.frontend_update", map[string]any{"load_balancer_id": "lb-1", "frontend_id": "fe-1", "ssl_redirect": false})
	unmarshalResult(t, res, &fe)
	if got, ok := rec.get("frontend_update")["ssl_redirect"]; !ok || got != false {
		t.Errorf("frontend_update ssl_redirect = %v (present=%v), want false", got, ok)
	}

	res = callTool(t, cs, "user.load_balancer.frontend_update", map[string]any{"load_balancer_id": "lb-1", "frontend_id": "fe-1", "name": "http2"})
	unmarshalResult(t, res, &fe)
	if _, ok := rec.get("frontend_update")["ssl_redirect"]; ok {
		t.Errorf("frontend_update without ssl_redirect sent the key anyway: %v", rec.get("frontend_update"))
	}
}

func TestLoadBalancer_DeleteConfirmConverges(t *testing.T) {
	cs := connectSession(t, loadBalancerMock(newBodyRecorder()))
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
