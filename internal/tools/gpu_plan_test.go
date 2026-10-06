package tools_test

import (
	"net/http"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// gpuPlanMock answers the plan endpoints in the exact shape Master sends them:
// a GPU plan carries the `gpu` object ({count, vendor, vram_min_gb, mode,
// profile, label}, plus `available` only where a location is in scope), a plan
// without a GPU carries gpu:null.
func gpuPlanMock() http.Handler {
	gpuLocation := map[string]any{"count": 1, "vendor": "nvidia", "vram_min_gb": 24, "mode": "passthrough", "profile": nil,
		"label": "1 × NVIDIA RTX A5000 · 24 GB", "available": true}
	gpuNoLocation := map[string]any{"count": 1, "vendor": "nvidia", "vram_min_gb": 24, "mode": "passthrough", "profile": nil,
		"label": "1 × NVIDIA GPU · 24 GB min"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cloud-service/location/{id}/plan-group/{pg}/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []any{
			map[string]any{"id": "plan-gpu", "name": "gpu-1", "gpu_count": 1, "gpu": gpuLocation},
			map[string]any{"id": "plan-plain", "name": "small-1", "gpu_count": 0, "gpu": nil},
		})
	})
	mux.HandleFunc("GET /kubernetes/search/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{
			// Kubernetes does not support GPU plans: Master never lists one here.
			map[string]any{"id": "ip-p", "name": "std-2", "gpu": nil},
		}})
	})
	mux.HandleFunc("GET /v1/instance/plans", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"current_page": 1, "last_page": 1, "data": []any{
			map[string]any{"id": "plan-gpu", "name": "gpu-1", "gpu": gpuNoLocation},
			map[string]any{"id": "plan-plain", "name": "small-1", "gpu": nil},
		}})
	})
	mux.HandleFunc("GET /v1/instance/plan/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "name": "gpu-1", "gpu": gpuNoLocation})
	})
	return mux
}

func gpuOf(t *testing.T, item map[string]any) map[string]any {
	t.Helper()
	v, present := item["gpu"]
	if !present {
		t.Fatalf("item %v has no gpu key (the field must pass through, null included)", item["id"])
	}
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("gpu = %#v, want object or null", v)
	}
	return m
}

func TestGPUPlan_PassesThroughOnPlanTools(t *testing.T) {
	cs := connectSession(t, gpuPlanMock())

	var plans tools.CatalogListResult
	unmarshalResult(t, callTool(t, cs, "user.catalog.plans", map[string]any{"location_id": "loc-1", "plan_group_id": "pg-1"}), &plans)
	g := gpuOf(t, plans.Items[0])
	if g["label"] != "1 × NVIDIA RTX A5000 · 24 GB" || g["available"] != true || g["vram_min_gb"] != float64(24) || g["vendor"] != "nvidia" {
		t.Errorf("user.catalog.plans gpu = %v", g)
	}
	if gpuOf(t, plans.Items[1]) != nil {
		t.Errorf("non-GPU plan must keep gpu null, got %v", plans.Items[1]["gpu"])
	}

	var k8s tools.CatalogListResult
	unmarshalResult(t, callTool(t, cs, "user.catalog.k8s_worker_plans", map[string]any{}), &k8s)
	if len(k8s.Items) != 1 || gpuOf(t, k8s.Items[0]) != nil {
		t.Errorf("k8s worker plans carry gpu null (no GPU plan is listed), got %v", k8s.Items)
	}

	var list tools.AdminListResult
	unmarshalResult(t, callTool(t, cs, "admin.instance_plan.list", map[string]any{}), &list)
	if gpuOf(t, list.Items[0])["label"] != "1 × NVIDIA GPU · 24 GB min" || gpuOf(t, list.Items[1]) != nil {
		t.Errorf("admin.instance_plan.list gpu = %v / %v", list.Items[0]["gpu"], list.Items[1]["gpu"])
	}

	var item tools.AdminItemResult
	unmarshalResult(t, callTool(t, cs, "admin.instance_plan.get", map[string]any{"id": "plan-gpu"}), &item)
	if gpuOf(t, item.Item)["mode"] != "passthrough" {
		t.Errorf("admin.instance_plan.get gpu = %v", item.Item["gpu"])
	}
}
