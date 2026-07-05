package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func autoscalingMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	deleted := false

	mux.HandleFunc("POST /scaling-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "group": map[string]any{"id": "asg-1", "name": "web", "status": "active"}})
	})
	mux.HandleFunc("GET /scaling-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":        true,
			"scaling_groups": map[string]any{"current_page": 1, "last_page": 1, "data": []any{map[string]any{"id": "asg-1", "name": "web"}}},
		})
	})
	mux.HandleFunc("GET /scaling-group/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gone := deleted
		mu.Unlock()
		if gone {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Scaling group not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "scaling_group": map[string]any{"id": r.PathValue("id"), "name": "web"}})
	})
	mux.HandleFunc("DELETE /scaling-group/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = true
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "queued"})
	})
	mux.HandleFunc("POST /scaling-group/{id}/policy", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "policy": map[string]any{"id": "pol-1", "metric": "cpu"}})
	})
	return mux
}

func TestAutoscaling_GroupAndPolicy(t *testing.T) {
	cs := connectSession(t, autoscalingMock())

	res := callTool(t, cs, "user.autoscaling_group.create", map[string]any{
		"name": "web", "hypervisor_group_id": "hg-1", "plan_id": "plan-1", "image_id": "img-1",
	})
	var group tools.AutoscalingGroupResult
	unmarshalResult(t, res, &group)
	if group.Group["id"] != "asg-1" {
		t.Fatalf("group create id = %v, want asg-1", group.Group["id"])
	}

	res = callTool(t, cs, "user.autoscaling_group.list", map[string]any{})
	var list tools.AutoscalingGroupListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("group list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.autoscaling_policy.create", map[string]any{
		"group_id": "asg-1", "metric": "cpu", "scale_up_threshold": 80.0, "scale_down_threshold": 20.0,
	})
	var policy tools.AutoscalingPolicyResult
	unmarshalResult(t, res, &policy)
	if policy.Policy["id"] != "pol-1" {
		t.Errorf("policy create id = %v, want pol-1", policy.Policy["id"])
	}
}

func TestAutoscaling_GroupDeleteConfirmConverges(t *testing.T) {
	cs := connectSession(t, autoscalingMock())
	res := callTool(t, cs, "user.autoscaling_group.delete", map[string]any{"id": "asg-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.autoscaling_group.delete", map[string]any{"id": "asg-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not converge")
	}
}
