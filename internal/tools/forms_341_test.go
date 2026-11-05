package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// 341-CONSUMERS: request bodies for the 3.3.7.2 user API changes (340-FORMS).

// forms341Recorder is a mock that records the last decoded JSON body per route.
type forms341Recorder struct {
	mu   sync.Mutex
	last map[string]map[string]any
}

func (b *forms341Recorder) record(key string, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last == nil {
		b.last = map[string]map[string]any{}
	}
	b.last[key] = body
}

func (b *forms341Recorder) get(key string) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last[key]
}

func TestManagedDatabaseCreate_SendsParameterGroupID(t *testing.T) {
	rec := &forms341Recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /databases", func(w http.ResponseWriter, r *http.Request) {
		rec.record("create", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "managed_database": map[string]any{"id": "db-1", "status": "deploying"}})
	})
	mux.HandleFunc("GET /database/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "managed_database": map[string]any{"id": "db-1", "status": "active"}})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.managed_database.create", map[string]any{
		"name": "prod", "engine": "postgresql", "engine_version": "16", "db_plan_id": "dp-1",
		"vpc_id": "vpc-1", "vpc_subnet_id": "sub-1", "parameter_group_id": "pg-1",
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	if got := rec.get("create")["parameter_group_id"]; got != "pg-1" {
		t.Errorf("body parameter_group_id = %v, want pg-1", got)
	}

	// Omitted: the key must not be sent at all.
	res = callTool(t, cs, "user.managed_database.create", map[string]any{
		"name": "prod", "engine": "postgresql", "engine_version": "16", "db_plan_id": "dp-1",
		"vpc_id": "vpc-1", "vpc_subnet_id": "sub-1",
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	if _, present := rec.get("create")["parameter_group_id"]; present {
		t.Errorf("parameter_group_id must be omitted when not given: %v", rec.get("create"))
	}
}

func TestAutoscalingGroupCreate_SendsUserScripts(t *testing.T) {
	rec := &forms341Recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /scaling-groups", func(w http.ResponseWriter, r *http.Request) {
		rec.record("create", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "group": map[string]any{"id": "asg-1"}})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.autoscaling_group.create", map[string]any{
		"name": "web", "location_id": "hg-1", "plan_id": "plan-1", "image_id": "img-1",
		"user_scripts": []string{"s-1", "s-2"},
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	scripts, _ := rec.get("create")["user_scripts"].([]any)
	if len(scripts) != 2 || scripts[0] != "s-1" || scripts[1] != "s-2" {
		t.Errorf("body user_scripts = %v, want [s-1 s-2]", rec.get("create")["user_scripts"])
	}
}

func TestAlertRuleCreate_AliasAndCatalogMetricSentAsGiven(t *testing.T) {
	rec := &forms341Recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /alert-rules", func(w http.ResponseWriter, r *http.Request) {
		rec.record("create", r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "alert_rule": map[string]any{"id": "ar-1"}})
	})
	cs := connectSession(t, mux)

	for _, metric := range []string{"cpu", "cpu_pct"} {
		res := callTool(t, cs, "user.alert_rule.create", map[string]any{
			"name": "n", "resource_type": "instance", "metric": metric, "operator": "gt", "threshold": 80.0,
		})
		if res.IsError {
			t.Fatalf("create(%s) failed: %s", metric, resultText(t, res))
		}
		if got := rec.get("create")["metric"]; got != metric {
			t.Errorf("metric = %v, want %s unchanged", got, metric)
		}
	}
}

func TestSecurityGroupAddRule_WarningIsSuccessAnd422IsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /security-group/{id}/rules", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "refused" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"success": false, "message": "Duplicate rule", "errors": map[string]any{"cidr": []string{"duplicate"}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "message": "Rule added successfully", "rule": map[string]any{"id": "rule-7"},
			"warning": "The rule is saved and must not be added again.", "warning_reason": "config_push_failed",
		})
	})
	mux.HandleFunc("POST /load-balancer/{lb}/security-group/{id}/rules", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "rule": map[string]any{"id": "rule-8"},
			"warning": "saved", "warning_reason": "config_push_failed",
		})
	})
	cs := connectSession(t, mux)

	args := map[string]any{"security_group_id": "sg-1", "direction": "ingress", "protocol": "tcp", "ip_version": "ipv4"}
	res := callTool(t, cs, "user.security_group.add_rule", args)
	if res.IsError {
		t.Fatalf("200 with warning must be a success: %s", resultText(t, res))
	}
	var rule tools.RuleResult
	unmarshalResult(t, res, &rule)
	if rule.Rule["id"] != "rule-7" {
		t.Errorf("rule id = %v, want rule-7", rule.Rule["id"])
	}

	lbArgs := map[string]any{"load_balancer_id": "lb-1", "security_group_id": "sg-1", "direction": "ingress", "protocol": "tcp", "ip_version": "ipv4"}
	res = callTool(t, cs, "user.load_balancer.add_security_group_rule", lbArgs)
	if res.IsError {
		t.Fatalf("LB 200 with warning must be a success: %s", resultText(t, res))
	}

	args["security_group_id"] = "refused"
	res = callTool(t, cs, "user.security_group.add_rule", args)
	if !res.IsError || !strings.Contains(resultText(t, res), "Duplicate rule") {
		t.Errorf("422 must be an error carrying the message, got IsError=%v %q", res.IsError, resultText(t, res))
	}
}
