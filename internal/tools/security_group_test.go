package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func securityGroupMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /security-groups", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] == "" || body["name"] == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The given data was invalid.",
				"errors":  map[string]any{"name": []string{"The name field is required."}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "security_group": map[string]any{"id": "sg-1", "name": body["name"]},
		})
	})
	mux.HandleFunc("GET /security-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "sg-1", "name": "web", "rules_count": 2}},
		})
	})
	mux.HandleFunc("GET /security-group/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Security group not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"security_group": map[string]any{
				"id": id, "name": "web",
				"rules": []any{map[string]any{"id": "rule-1", "direction": "ingress"}},
			},
			"attached_instances": []any{map[string]any{"id": "inst-1", "name": "web-1"}},
		})
	})
	mux.HandleFunc("DELETE /security-group/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("POST /security-group/{id}/rules", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "rule": map[string]any{"id": "rule-9", "direction": "ingress", "protocol": "tcp"},
		})
	})
	mux.HandleFunc("DELETE /security-group/{id}/rule/{ruleId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "rule removed"})
	})
	mux.HandleFunc("POST /security-group/{id}/attach-instances", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "attached"})
	})
	mux.HandleFunc("POST /security-group/{id}/detach-instances", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "detached"})
	})
	return mux
}

func TestSecurityGroup_CRUDAndActions(t *testing.T) {
	cs := connectSession(t, securityGroupMock())

	res := callTool(t, cs, "user.security_group.create", map[string]any{"name": "web"})
	var created tools.SecurityGroupResult
	unmarshalResult(t, res, &created)
	if created.SecurityGroup["id"] != "sg-1" {
		t.Fatalf("create id = %v, want sg-1", created.SecurityGroup["id"])
	}

	res = callTool(t, cs, "user.security_group.list", map[string]any{})
	var list tools.SecurityGroupListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.security_group.get", map[string]any{"id": "sg-1"})
	var got tools.SecurityGroupResult
	unmarshalResult(t, res, &got)
	if len(got.AttachedInstances) != 1 || got.AttachedInstances[0]["id"] != "inst-1" {
		t.Errorf("get attached_instances = %v, want one inst-1", got.AttachedInstances)
	}

	res = callTool(t, cs, "user.security_group.add_rule", map[string]any{
		"security_group_id": "sg-1", "direction": "ingress", "protocol": "tcp", "ip_version": "ipv4",
		"port_range_min": 80, "port_range_max": 80, "cidr": "0.0.0.0/0",
	})
	var rule tools.RuleResult
	unmarshalResult(t, res, &rule)
	if rule.Rule["id"] != "rule-9" {
		t.Errorf("add_rule id = %v, want rule-9", rule.Rule["id"])
	}

	res = callTool(t, cs, "user.security_group.attach_instances", map[string]any{
		"security_group_id": "sg-1", "instance_ids": []any{"inst-1"},
	})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("attach ok = false")
	}
}

func TestSecurityGroup_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, securityGroupMock())

	res := callTool(t, cs, "user.security_group.delete", map[string]any{"id": "sg-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse and mention confirm; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.security_group.delete", map[string]any{"id": "sg-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}

func TestSecurityGroup_ErrorMapping(t *testing.T) {
	cs := connectSession(t, securityGroupMock())

	// 422 on create (empty name).
	res := callTool(t, cs, "user.security_group.create", map[string]any{"name": ""})
	if !res.IsError || !strings.Contains(resultText(t, res), "validation failed") {
		t.Errorf("create empty name: want validation failed, got %q", resultText(t, res))
	}
	// 404 on get.
	res = callTool(t, cs, "user.security_group.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
}

// SBX-SG-ATTACH: attach / detach a security group to sandboxes, the typed
// refusals the API answers, and the create tool's real security_group_ids.
func TestSecurityGroup_AttachDetachSandboxes(t *testing.T) {
	var gotPaths []string
	var gotBodies []map[string]any
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotPaths = append(gotPaths, r.Method+" "+r.URL.Path)
		gotBodies = append(gotBodies, body)
		if ids, _ := body["sandbox_ids"].([]any); len(ids) == 1 && ids[0] == "killed" {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "sandbox_killed", "message": "A killed sandbox cannot carry security groups."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ok", "sandbox_ids": body["sandbox_ids"]})
	}
	mux.HandleFunc("POST /security-group/{id}/attach-sandboxes", handler)
	mux.HandleFunc("POST /security-group/{id}/detach-sandboxes", handler)
	mux.HandleFunc("GET /security-group/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":            true,
			"security_group":     map[string]any{"id": r.PathValue("id"), "name": "web"},
			"attached_sandboxes": []any{map[string]any{"id": "vm-1", "name": "worker", "state": "running"}},
		})
	})
	cs := connectSession(t, mux)

	var ok tools.OKResult
	res := callTool(t, cs, "user.security_group.attach_sandboxes", map[string]any{"security_group_id": "sg-1", "sandbox_ids": []any{"vm-1", "vm-2"}})
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Fatalf("attach_sandboxes ok = false: %s", resultText(t, res))
	}
	res = callTool(t, cs, "user.security_group.detach_sandboxes", map[string]any{"security_group_id": "sg-1", "sandbox_ids": []any{"vm-1"}})
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Fatalf("detach_sandboxes ok = false: %s", resultText(t, res))
	}
	if len(gotPaths) != 2 || gotPaths[0] != "POST /security-group/sg-1/attach-sandboxes" || gotPaths[1] != "POST /security-group/sg-1/detach-sandboxes" {
		t.Fatalf("paths = %v", gotPaths)
	}
	if ids, _ := gotBodies[0]["sandbox_ids"].([]any); len(ids) != 2 || ids[1] != "vm-2" {
		t.Errorf("attach body = %v", gotBodies[0])
	}

	res = callTool(t, cs, "user.security_group.attach_sandboxes", map[string]any{"security_group_id": "sg-1", "sandbox_ids": []any{"killed"}})
	if !res.IsError || !strings.Contains(resultText(t, res), "killed sandbox") {
		t.Errorf("killed sandbox: want the typed refusal text, got %q", resultText(t, res))
	}

	// The group read lists the sandboxes it is attached to.
	res = callTool(t, cs, "user.security_group.get", map[string]any{"id": "sg-1"})
	var got tools.SecurityGroupResult
	unmarshalResult(t, res, &got)
	if len(got.AttachedSandboxes) != 1 || got.AttachedSandboxes[0]["id"] != "vm-1" {
		t.Errorf("get attached_sandboxes = %v", got.AttachedSandboxes)
	}
}
