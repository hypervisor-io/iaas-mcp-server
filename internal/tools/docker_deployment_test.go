package tools_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func dockerMock() http.Handler {
	mux := http.NewServeMux()
	// Index (used by DockerEnabled, ListDockerDeployments, and the deploy status
	// poll which scans it): engine already enabled, deployment already running so
	// the deploy waiter converges on the first poll.
	mux.HandleFunc("GET /instance/{id}/docker", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "docker_enabled": 1,
			"deployments": []any{map[string]any{"id": "dep-1", "status": "running"}},
		})
	})
	mux.HandleFunc("POST /instance/{id}/docker", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "deployment": map[string]any{"id": "dep-1", "status": "deploying"}})
	})
	mux.HandleFunc("DELETE /instance/{id}/docker/{depId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestDocker_DeployAppConverges(t *testing.T) {
	cs := connectSession(t, dockerMock())

	res := callTool(t, cs, "user.docker_deployment.deploy_app", map[string]any{
		"instance_id": "inst-1", "app_slug": "nginx",
	})
	var dep tools.DockerDeploymentResult
	unmarshalResult(t, res, &dep)
	if dep.Deployment["id"] != "dep-1" || dep.Deployment["status"] != "running" {
		t.Fatalf("deploy = %v, want dep-1/running", dep.Deployment)
	}
}

func TestDocker_ListAndDeleteConfirm(t *testing.T) {
	cs := connectSession(t, dockerMock())

	res := callTool(t, cs, "user.docker_deployment.list", map[string]any{"instance_id": "inst-1"})
	var list tools.DockerDeploymentListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.docker_deployment.delete", map[string]any{"instance_id": "inst-1", "deployment_id": "dep-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.docker_deployment.delete", map[string]any{"instance_id": "inst-1", "deployment_id": "dep-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
