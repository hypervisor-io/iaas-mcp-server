package tools_test

// Tri-sync CI gate, MCP leg (spec 17 REQ-TRISYNC-03 check 3). It reads the
// vendored copy of the platform's api-manifest.json (kept in sync from the
// Master repo via `make sync-manifest`) and asserts that every endpoint the
// manifest marks mcp.status=="covered" names an mcp.tool this server actually
// registers. A covered tool that is not registered is real tri-sync drift and
// fails the build. When TRISYNC_RELEASE=1 it also fails on any
// mcp.status=="pending" (the release gate).

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

const manifestPath = "testdata/api-manifest.json"

type manifestEndpoint struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	MCP     struct {
		Status string `json:"status"`
		Tool   string `json:"tool"`
		Reason string `json:"reason"`
	} `json:"mcp"`
}

type manifest struct {
	Endpoints []manifestEndpoint `json:"endpoints"`
}

func loadManifest(t *testing.T) manifest {
	t.Helper()
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest %s: %v", manifestPath, err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}
	if len(m.Endpoints) == 0 {
		t.Fatalf("manifest %s has no endpoints", manifestPath)
	}
	return m
}

// checkCoverage returns the sorted covered-but-unregistered tools (empty ==
// pass) and the pending endpoint ids. Shared by the positive and negative tests.
func checkCoverage(registered map[string]bool, m manifest) (missing, pending []string) {
	missingSet := map[string]bool{}
	seen := map[string]bool{}
	for _, e := range m.Endpoints {
		if e.ID == "" || seen[e.ID] {
			missingSet["empty or duplicate endpoint id: "+e.ID] = true
		}
		seen[e.ID] = true
		switch e.MCP.Status {
		case "covered":
			if e.MCP.Tool == "" || !registered[e.MCP.Tool] {
				missingSet[e.MCP.Tool+"  (e.g. "+e.ID+")"] = true
			}
		case "pending":
			pending = append(pending, e.ID)
		case "excluded":
			if strings.TrimSpace(e.MCP.Reason) == "" {
				missingSet["excluded endpoint has no reason: "+e.ID] = true
			}
		default:
			missingSet["unknown MCP status "+e.MCP.Status+": "+e.ID] = true
		}
	}
	for k := range missingSet {
		missing = append(missing, k)
	}
	sort.Strings(missing)
	sort.Strings(pending)
	return missing, pending
}

func registeredToolSet(t *testing.T) map[string]bool {
	t.Helper()
	names := tools.RegisteredToolNames()
	if len(names) == 0 {
		t.Fatal("RegisteredToolNames returned zero tools")
	}
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return set
}

func TestManifestMCPCoverage(t *testing.T) {
	registered := registeredToolSet(t)
	m := loadManifest(t)

	missing, pending := checkCoverage(registered, m)

	covered := 0
	for _, e := range m.Endpoints {
		if e.MCP.Status == "covered" {
			covered++
		}
	}
	t.Logf("server registers %d tools; manifest has %d mcp-covered endpoints across %d total",
		len(registered), covered, len(m.Endpoints))

	if len(missing) > 0 {
		t.Fatalf("manifest marks these tools 'covered' but the server does not register them (tri-sync drift):\n  %s",
			strings.Join(missing, "\n  "))
	}

	if os.Getenv("TRISYNC_RELEASE") == "1" && len(pending) > 0 {
		t.Fatalf("TRISYNC_RELEASE=1 but %d endpoints are mcp.status=pending (release blocked):\n  %s",
			len(pending), strings.Join(pending, "\n  "))
	}
}

// TestManifestMCPCoverage_NegativeDetectsPhantom proves the check fails when the
// manifest claims a covered tool the server does not register.
func TestManifestMCPCoverage_NegativeDetectsPhantom(t *testing.T) {
	registered := registeredToolSet(t)
	phantom := manifest{Endpoints: []manifestEndpoint{
		{ID: "user POST /api/synthetic", Surface: "user"},
	}}
	phantom.Endpoints[0].MCP.Status = "covered"
	phantom.Endpoints[0].MCP.Tool = "user.nope.create"

	missing, _ := checkCoverage(registered, phantom)
	if len(missing) == 0 {
		t.Fatal("negative test failed: checkCoverage did not flag the phantom user.nope.create tool")
	}
}

func TestManifestMCPCoverage_AccountsForEveryEntry(t *testing.T) {
	for _, status := range []string{"", "coverd", "excluded"} {
		t.Run(status, func(t *testing.T) {
			m := manifest{Endpoints: []manifestEndpoint{{ID: "user GET /api/synthetic", Surface: "user"}}}
			m.Endpoints[0].MCP.Status = status
			missing, _ := checkCoverage(map[string]bool{}, m)
			if len(missing) == 0 {
				t.Fatal("unaccounted endpoint or unexplained exclusion was silently ignored")
			}
		})
	}
}

func TestManifestMCPCoverage_IPsecRoutes(t *testing.T) {
	want := map[string]string{
		"GET /api/vpn-gateway/{user_vpnGatewayId}/ipsec":                                          "user.vpn_ipsec.list",
		"POST /api/vpn-gateway/{user_vpnGatewayId}/ipsec":                                         "user.vpn_ipsec.create",
		"GET /api/vpn-gateway/{user_vpnGatewayId}/ipsec/{user_ipsecConnectionId}":                 "user.vpn_ipsec.get",
		"PATCH /api/vpn-gateway/{user_vpnGatewayId}/ipsec/{user_ipsecConnectionId}":               "user.vpn_ipsec.update",
		"DELETE /api/vpn-gateway/{user_vpnGatewayId}/ipsec/{user_ipsecConnectionId}":              "user.vpn_ipsec.delete",
		"POST /api/vpn-gateway/{user_vpnGatewayId}/ipsec/{user_ipsecConnectionId}/restart":        "user.vpn_ipsec.restart",
		"GET /api/vpn-gateway/{user_vpnGatewayId}/ipsec/{user_ipsecConnectionId}/remote-settings": "user.vpn_ipsec.remote_settings",
	}
	for _, endpoint := range loadManifest(t).Endpoints {
		if tool, ok := want[strings.TrimPrefix(endpoint.ID, "user ")]; ok && endpoint.Surface == "user" {
			if endpoint.MCP.Status != "covered" || endpoint.MCP.Tool != tool {
				t.Errorf("IPsec manifest drift: %+v", endpoint)
			}
			delete(want, strings.TrimPrefix(endpoint.ID, "user "))
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing user IPsec routes: %v", want)
	}
}
