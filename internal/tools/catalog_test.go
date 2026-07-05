package tools_test

import (
	"net/http"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func catalogMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cloud-service/locations", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "loc-1", "name": "London"}, map[string]any{"id": "loc-2", "name": "NYC"}},
		})
	})
	mux.HandleFunc("GET /cloud-service/location/{id}/plan-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{"id": "pg-1", "name": "General"}})
	})
	mux.HandleFunc("GET /cloud-service/location/{id}/plan-group/{pg}/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{map[string]any{"id": "plan-1", "name": "2vCPU"}})
	})
	mux.HandleFunc("GET /isos", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "iso-1", "name": "ubuntu.iso"}},
		})
	})
	// Kubernetes catalog search endpoints return Select2 {results:[...]}.
	mux.HandleFunc("GET /kubernetes/search/versions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{map[string]any{"id": "v-1", "text": "1.30"}}})
	})
	mux.HandleFunc("GET /kubernetes/search/subnets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{map[string]any{"id": "sub-1", "text": "10.0.0.0/24"}}})
	})
	return mux
}

func TestCatalog_Lookups(t *testing.T) {
	cs := connectSession(t, catalogMock())

	res := callTool(t, cs, "user.catalog.locations", map[string]any{})
	var locs tools.CatalogListResult
	unmarshalResult(t, res, &locs)
	if locs.Count != 2 {
		t.Fatalf("locations count = %d, want 2", locs.Count)
	}

	res = callTool(t, cs, "user.catalog.plan_groups", map[string]any{"location_id": "loc-1"})
	var pg tools.CatalogListResult
	unmarshalResult(t, res, &pg)
	if pg.Count != 1 || pg.Items[0]["id"] != "pg-1" {
		t.Errorf("plan_groups = %v", pg.Items)
	}

	res = callTool(t, cs, "user.catalog.plans", map[string]any{"location_id": "loc-1", "plan_group_id": "pg-1"})
	var plans tools.CatalogListResult
	unmarshalResult(t, res, &plans)
	if plans.Count != 1 || plans.Items[0]["id"] != "plan-1" {
		t.Errorf("plans = %v", plans.Items)
	}

	res = callTool(t, cs, "user.catalog.isos", map[string]any{})
	var isos tools.CatalogListResult
	unmarshalResult(t, res, &isos)
	if isos.Count != 1 {
		t.Errorf("isos count = %d, want 1", isos.Count)
	}

	res = callTool(t, cs, "user.catalog.k8s_versions", map[string]any{"query": "1.30"})
	var vers tools.CatalogListResult
	unmarshalResult(t, res, &vers)
	if vers.Count != 1 || vers.Items[0]["id"] != "v-1" {
		t.Errorf("k8s_versions = %v", vers.Items)
	}

	res = callTool(t, cs, "user.catalog.k8s_subnets", map[string]any{"vpc_id": "vpc-1", "type": "private"})
	var subs tools.CatalogListResult
	unmarshalResult(t, res, &subs)
	if subs.Count != 1 {
		t.Errorf("k8s_subnets count = %d, want 1", subs.Count)
	}
}
