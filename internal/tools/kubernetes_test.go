package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func kubernetesMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	polls := 0
	deleted := false

	mux.HandleFunc("POST /kubernetes/clusters", func(w http.ResponseWriter, r *http.Request) {
		// Assert the idempotency key threaded through.
		if r.Header.Get("Idempotency-Key") == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "missing idempotency key"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "cluster": map[string]any{"id": "k8s-1", "state": "created"}})
	})
	mux.HandleFunc("GET /kubernetes/cluster/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gone := deleted
		polls++
		state := "running"
		if polls < 2 {
			state = "created"
		}
		mu.Unlock()
		if gone {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Cluster not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "cluster": map[string]any{"id": r.PathValue("id"), "state": state}})
	})
	mux.HandleFunc("DELETE /kubernetes/cluster/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		deleted = true
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "destroying"})
	})
	mux.HandleFunc("POST /kubernetes/cluster/{id}/pools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "pool": map[string]any{"id": "pool-1", "name": "workers"}})
	})
	mux.HandleFunc("GET /kubernetes/cluster/{id}/pools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "pools": []any{map[string]any{"id": "pool-1", "name": "workers"}}})
	})
	mux.HandleFunc("POST /kubernetes/cluster/{id}/security-group/{scope}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "rule": map[string]any{"id": "sgr-1", "scope": r.PathValue("scope")}})
	})
	mux.HandleFunc("GET /kubernetes/cluster/{id}/kubeconfig", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("apiVersion: v1\nkind: Config\n"))
	})
	return mux
}

func TestKubernetes_ClusterConvergesAndChildren(t *testing.T) {
	cs := connectSession(t, kubernetesMock())

	res := callTool(t, cs, "user.kubernetes_cluster.create", map[string]any{
		"name": "prod", "slug": "prod", "hypervisor_group_id": "hg-1", "vpc_id": "vpc-1",
		"cp_vpc_subnet_id": "sub-1", "worker_vpc_subnet_id": "sub-2", "kubernetes_version_id": "kv-1",
		"control_node_count": 1, "endpoint_mode": "private", "cp_instance_plan_id": "cp-1",
		"cp_lb_plan_id": "lb-1", "worker_instance_plan_id": "wk-1",
	})
	var cluster tools.KubernetesClusterResult
	unmarshalResult(t, res, &cluster)
	if cluster.Cluster["id"] != "k8s-1" || cluster.Cluster["state"] != "running" {
		t.Fatalf("create = %v, want k8s-1/running", cluster.Cluster)
	}

	res = callTool(t, cs, "user.kubernetes_node_pool.create", map[string]any{
		"cluster_id": "k8s-1", "name": "workers", "instance_plan_id": "wk-1",
		"min_size": 1, "max_size": 3, "target_count": 2, "weight": 100, "autoscaling_enabled": true,
	})
	var pool tools.K8sNodePoolResult
	unmarshalResult(t, res, &pool)
	if pool.Pool["id"] != "pool-1" {
		t.Errorf("node_pool id = %v, want pool-1", pool.Pool["id"])
	}

	res = callTool(t, cs, "user.kubernetes_node_pool.list", map[string]any{"cluster_id": "k8s-1"})
	var pools tools.K8sNodePoolListResult
	unmarshalResult(t, res, &pools)
	if pools.Count != 1 {
		t.Errorf("node_pool list count = %d, want 1", pools.Count)
	}

	res = callTool(t, cs, "user.kubernetes_security_group_rule.create", map[string]any{
		"cluster_id": "k8s-1", "scope": "worker", "direction": "ingress", "protocol": "tcp", "ip_version": "ipv4",
	})
	var rule tools.K8sSgRuleResult
	unmarshalResult(t, res, &rule)
	if rule.Rule["id"] != "sgr-1" {
		t.Errorf("sg_rule id = %v, want sgr-1", rule.Rule["id"])
	}

	res = callTool(t, cs, "user.kubernetes_cluster.kubeconfig", map[string]any{"cluster_id": "k8s-1"})
	var kc tools.YAMLResult
	unmarshalResult(t, res, &kc)
	if !strings.Contains(kc.YAML, "kind: Config") {
		t.Errorf("kubeconfig = %q, want a kubeconfig", kc.YAML)
	}
}

func TestKubernetes_DeleteConfirmConverges(t *testing.T) {
	cs := connectSession(t, kubernetesMock())
	res := callTool(t, cs, "user.kubernetes_cluster.delete", map[string]any{"id": "k8s-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.kubernetes_cluster.delete", map[string]any{"id": "k8s-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not converge")
	}
}
