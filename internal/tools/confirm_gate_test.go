package tools_test

import (
	"net/http"
	"strings"
	"testing"
)

// permissiveMock returns {"success":true} for every request, so any tool that
// gets past the framework's confirm gate "succeeds". The gate itself runs
// before any HTTP call, so the no-confirm cases never reach this handler.
func permissiveMock() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "ok"})
	})
}

// TestConfirmGate_ChildResourceDeletes asserts every child/sub-resource
// delete/remove tool is confirm-gated: a call without confirm is refused (with
// a message naming confirm), and the same call with "confirm": true is allowed
// through to the (permissive) backend.
func TestConfirmGate_ChildResourceDeletes(t *testing.T) {
	cs := connectSession(t, permissiveMock())

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"user.security_group.remove_rule", map[string]any{"security_group_id": "sg-1", "rule_id": "r-1"}},
		{"user.ip_set.remove_entry", map[string]any{"ip_set_id": "set-1", "entry_id": "e-1"}},
		{"user.dns_record.delete", map[string]any{"zone_id": "z-1", "record_set_id": "rs-1", "record_id": "rec-1"}},
		{"user.instance_vpc.remove_ip", map[string]any{"instance_id": "i-1", "vpc_ip_id": "vip-1"}},
		{"user.vpn_gateway.remove_peer", map[string]any{"gateway_id": "gw-1", "peer_id": "p-1"}},
		{"user.vpn_peering.delete", map[string]any{"gateway_id": "gw-1", "peering_id": "pr-1"}},
		{"user.kubernetes_security_group_rule.delete", map[string]any{"cluster_id": "k-1", "scope": "worker", "rule_id": "sgr-1"}},
		{"user.autoscaling_policy.delete", map[string]any{"group_id": "g-1", "policy_id": "pol-1"}},
		{"user.load_balancer.frontend_delete", map[string]any{"load_balancer_id": "lb-1", "child_id": "fe-1"}},
		{"user.load_balancer.backend_delete", map[string]any{"load_balancer_id": "lb-1", "child_id": "be-1"}},
		{"user.load_balancer.certificate_delete", map[string]any{"load_balancer_id": "lb-1", "child_id": "cert-1"}},
		{"user.load_balancer.target_delete", map[string]any{"load_balancer_id": "lb-1", "backend_id": "be-1", "target_id": "tg-1"}},
		{"user.load_balancer.routing_rule_delete", map[string]any{"load_balancer_id": "lb-1", "frontend_id": "fe-1", "rule_id": "rl-1"}},
		{"user.instance.snapshot.rollback", map[string]any{"instance_id": "inst-1", "name": "snap-1"}},
		{"user.instance.snapshot.delete", map[string]any{"instance_id": "inst-1", "name": "snap-1"}},
	}

	for _, tc := range cases {
		t.Run(tc.tool+"/refused without confirm", func(t *testing.T) {
			res := callTool(t, cs, tc.tool, tc.args)
			if !res.IsError {
				t.Fatalf("%s without confirm should be refused, got a success result", tc.tool)
			}
			if msg := resultText(t, res); !strings.Contains(msg, "confirm") {
				t.Errorf("%s refusal = %q, want it to mention confirm", tc.tool, msg)
			}
		})

		t.Run(tc.tool+"/allowed with confirm", func(t *testing.T) {
			args := map[string]any{"confirm": true}
			for k, v := range tc.args {
				args[k] = v
			}
			res := callTool(t, cs, tc.tool, args)
			if res.IsError {
				t.Fatalf("%s with confirm should be allowed, got error: %s", tc.tool, resultText(t, res))
			}
		})
	}
}
