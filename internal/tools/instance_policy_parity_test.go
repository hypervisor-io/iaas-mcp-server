package tools_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func TestInstancePolicyPatchPreservesNullOmissionAndZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields map[string]any
	}{
		{"empty", map[string]any{}},
		{"name_only", map[string]any{"name": "renamed"}},
		{"timezone_set", map[string]any{"timezone": "Europe/Amsterdam"}},
		{"timezone_clear", map[string]any{"timezone": nil}},
		{"gfs", map[string]any{"retention_mode": "gfs", "keep_last": float64(2), "keep_within_days": float64(30), "keep_daily": float64(7), "keep_weekly": float64(4), "keep_monthly": float64(6), "keep_yearly": float64(1)}},
		{"disable_rules", map[string]any{"keep_last": nil, "keep_daily": float64(0)}},
		{"zero_schedule", map[string]any{"full_backup_day": float64(0), "max_incremental_chain": float64(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			mux := http.NewServeMux()
			mux.HandleFunc("PATCH /backup-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(body, tc.fields) {
					t.Errorf("PATCH = %#v, want %#v", body, tc.fields)
				}
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "policy": map[string]any{"id": "p1", "timezone": nil, "retention_mode": "gfs", "keep_daily": nil}})
			})
			cs := connectSession(t, mux)
			args := map[string]any{"id": "p1"}
			for k, v := range tc.fields {
				args[k] = v
			}
			result := callTool(t, cs, "user.instance_backup_policy.update", args)
			if result.IsError {
				t.Fatalf("update: %s", resultText(t, result))
			}
			if calls != 1 {
				t.Fatalf("PATCH calls=%d", calls)
			}
			var out tools.BackupPolicyResult
			unmarshalResult(t, result, &out)
			if value, exists := out.Policy["timezone"]; !exists || value != nil {
				t.Fatalf("nullable response = %#v", out.Policy)
			}
		})
	}
}

func TestInstancePolicyCreateGFSAndReadFields(t *testing.T) {
	policy := map[string]any{"id": "p1", "timezone": "Europe/Amsterdam", "retention_mode": "gfs", "keep_last": float64(2), "keep_within_days": float64(30), "keep_daily": float64(7), "keep_weekly": float64(4), "keep_monthly": float64(6), "keep_yearly": float64(1)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /backup-policies", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, exists := body["retention_count"]; exists {
			t.Error("GFS count must be omittable")
		}
		for k, v := range policy {
			if k != "id" && body[k] != v {
				t.Errorf("body %s=%v, want %v", k, body[k], v)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "policy": policy})
	})
	mux.HandleFunc("GET /backup-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"policy": policy, "available_instances": []any{}})
	})
	mux.HandleFunc("GET /backup-policies", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{policy}, "current_page": 1, "last_page": 1})
	})
	cs := connectSession(t, mux)
	args := map[string]any{"name": "gfs", "full_backup_frequency": "daily", "full_backup_time": "02:30", "max_incremental_chain": 0, "backup_device": "all"}
	for k, v := range policy {
		if k != "id" {
			args[k] = v
		}
	}
	for _, step := range []struct {
		name string
		args map[string]any
	}{{"create", args}, {"get", map[string]any{"id": "p1"}}} {
		result := callTool(t, cs, "user.instance_backup_policy."+step.name, step.args)
		if result.IsError {
			t.Fatalf("%s: %s", step.name, resultText(t, result))
		}
		var out tools.BackupPolicyResult
		unmarshalResult(t, result, &out)
		if !reflect.DeepEqual(out.Policy, policy) {
			t.Fatalf("%s fields = %#v", step.name, out.Policy)
		}
	}
	result := callTool(t, cs, "user.instance_backup_policy.list", map[string]any{})
	var list tools.BackupPolicyListResult
	unmarshalResult(t, result, &list)
	if list.Count != 1 || !reflect.DeepEqual(list.Policies[0], policy) {
		t.Fatalf("list = %#v", list)
	}
}

func TestInstancePolicyInvalidTimezoneReason(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /backup-policy/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"success": false, "reason": "invalid_timezone", "message": "Choose a named IANA policy timezone or null to use the default.", "errors": map[string]any{"timezone": []string{"The selected timezone is invalid."}}})
	})
	cs := connectSession(t, mux)
	result := callTool(t, cs, "user.instance_backup_policy.update", map[string]any{"id": "p1", "timezone": "+05:30"})
	if !result.IsError || !strings.Contains(resultText(t, result), "invalid_timezone") {
		t.Fatalf("timezone error = %s", resultText(t, result))
	}
}

func TestCloudInstanceCreateCapacityReason(t *testing.T) {
	for _, reason := range []string{"ipv6_not_available", "storage_backend_not_offered", "no_capacity"} {
		t.Run(reason, func(t *testing.T) {
			calls := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/cloud-service/instances" {
					t.Errorf("must not deploy after refusal: %s %s", r.Method, r.URL.Path)
				}
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"success": false, "message": "Try another plan or location.", "reason": reason})
			})
			cs := connectSession(t, handler)
			result := callTool(t, cs, "user.instance.create", map[string]any{"location_id": "l1", "plan_id": "p1", "image_id": "image1"})
			if !result.IsError || !strings.Contains(resultText(t, result), reason) {
				t.Fatalf("capacity error=%s", resultText(t, result))
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
