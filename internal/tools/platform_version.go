package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Platform version tool (NUI-V-R20-VER1, round-20 customer report item 1):
// the master's own version was not readable through the API at all. Lets an
// agent gate an agent-version-dependent feature (e.g. rescue mode) against
// min_agent_version instead of hardcoding a version string. Read-only, so
// not confirm-gated. `GET /version` (user API surface) -- the admin
// api/v1 twin (GET /v1/system/version, which also carries a
// hypervisor-fleet summary) is intentionally NOT exposed here: see
// api-manifest.json, "admin read not in the v1 curated admin allowlist;
// candidate for a later phase".

func init() {
	toolRegistrars = append(toolRegistrars, registerPlatformVersionTools)
}

// PlatformVersionResult mirrors the API's {version, api_envelope,
// min_agent_version} object. MinAgentVersion is a *string (not string) so
// the generated output schema allows an explicit JSON null -- it is nil
// while the master enforces no agent-version floor.
type PlatformVersionResult struct {
	Version         string  `json:"version"`
	ApiEnvelope     string  `json:"api_envelope"`
	MinAgentVersion *string `json:"min_agent_version"`
}

func platformVersionResult(top map[string]any, err error) (PlatformVersionResult, error) {
	if err != nil {
		return PlatformVersionResult{}, err
	}

	version, _ := top["version"].(string)
	envelope, _ := top["api_envelope"].(string)

	result := PlatformVersionResult{Version: version, ApiEnvelope: envelope}
	if minVersion, ok := top["min_agent_version"].(string); ok {
		result.MinAgentVersion = &minVersion
	}

	return result, nil
}

func registerPlatformVersionTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.platform.version", Description: "Get the platform's own version, the API envelope version, and the minimum hypervisor agent version it currently requires (null when nothing enforces a floor)."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (PlatformVersionResult, error) {
			return platformVersionResult(cl.GetPlatformVersion(ctx))
		})
}
