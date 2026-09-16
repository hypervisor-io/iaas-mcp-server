package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// locationIDMock captures the create body user.vpc.create sends to the API and
// returns a canned object that carries location_id (the API's public output
// name since the response middleware rename).
type locationIDMock struct {
	createBodies []map[string]any
	lastLocation any
}

func (m *locationIDMock) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vpcs", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.createBodies = append(m.createBodies, body)
		m.lastLocation = body["location_id"]
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"vpc": map[string]any{
				"id":          "vpc-1",
				"name":        body["name"],
				"cidr":        body["cidr"],
				"location_id": body["location_id"],
				"vni_number":  5001,
			},
		})
	})
	mux.HandleFunc("GET /vpc/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"vpc": map[string]any{
				"id":          r.PathValue("id"),
				"name":        "prod",
				"cidr":        "10.0.0.0/24",
				"location_id": m.lastLocation,
				"vni_number":  5001,
			},
		})
	})
	return mux
}

// callVPCCreate runs user.vpc.create against the mock and returns the captured
// API request body plus the tool's structured result.
func callVPCCreate(t *testing.T, mock *locationIDMock, args map[string]any) (map[string]any, tools.VPCResult) {
	t.Helper()
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.vpc.create", args)
	var out tools.VPCResult
	unmarshalResult(t, res, &out)
	if len(mock.createBodies) != 1 {
		t.Fatalf("create called %d times, want 1", len(mock.createBodies))
	}
	return mock.createBodies[0], out
}

func TestLocationID_CanonicalKeyAcceptedAndForwarded(t *testing.T) {
	mock := &locationIDMock{}
	body, out := callVPCCreate(t, mock, map[string]any{
		"name": "prod", "cidr": "10.0.0.0/24", "location_id": "loc-1",
	})
	if body["location_id"] != "loc-1" {
		t.Errorf("API create body location_id = %v, want loc-1", body["location_id"])
	}
	if _, ok := body["hypervisor_group_id"]; ok {
		t.Errorf("API create body carries the deprecated key: %v", body)
	}
	// The output shape carries location_id and never the old key.
	if out.VPC["location_id"] != "loc-1" {
		t.Errorf("result location_id = %v, want loc-1", out.VPC["location_id"])
	}
	if _, ok := out.VPC["hypervisor_group_id"]; ok {
		t.Errorf("result carries the deprecated key: %v", out.VPC)
	}
}

func TestLocationID_OldKeyAcceptedAsAlias(t *testing.T) {
	mock := &locationIDMock{}
	body, out := callVPCCreate(t, mock, map[string]any{
		"name": "prod", "cidr": "10.0.0.0/24", "hypervisor_group_id": "hg-1",
	})
	if body["location_id"] != "hg-1" {
		t.Errorf("API create body location_id = %v, want hg-1 (alias resolved)", body["location_id"])
	}
	if _, ok := body["hypervisor_group_id"]; ok {
		t.Errorf("API create body carries the deprecated key: %v", body)
	}
	if out.VPC["location_id"] != "hg-1" {
		t.Errorf("result location_id = %v, want hg-1", out.VPC["location_id"])
	}
}

func TestLocationID_BothKeysLocationIDWins(t *testing.T) {
	mock := &locationIDMock{}
	body, _ := callVPCCreate(t, mock, map[string]any{
		"name": "prod", "cidr": "10.0.0.0/24",
		"location_id": "loc-1", "hypervisor_group_id": "hg-1",
	})
	if body["location_id"] != "loc-1" {
		t.Errorf("API create body location_id = %v, want loc-1 (canonical wins)", body["location_id"])
	}
	if _, ok := body["hypervisor_group_id"]; ok {
		t.Errorf("API create body carries the deprecated key: %v", body)
	}
}

func TestLocationID_ToolDescriptionsNoteTheAlias(t *testing.T) {
	cs := newSession(t)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	affected := map[string]bool{
		"user.autoscaling_group.create":  true,
		"user.catalog.images":            true,
		"user.catalog.k8s_vpcs":          true,
		"user.kubernetes_cluster.create": true,
		"user.load_balancer.create":      true,
		"user.managed_database.create":   true,
		"user.static_ip.allocate":        true,
		"user.vpc.create":                true,
		"user.volume.backup_restore":     true,
		"user.volume.create":             true,
		"user.volume.snapshot_restore":   true,
	}
	for _, tool := range res.Tools {
		if !affected[tool.Name] {
			continue
		}
		if !strings.Contains(tool.Description, "hypervisor_group_id") || !strings.Contains(tool.Description, "alias") {
			t.Errorf("tool %s description %q does not note the deprecated alias", tool.Name, tool.Description)
		}
	}
}
