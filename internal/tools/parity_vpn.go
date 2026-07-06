package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// VPN gateway list and retry tools.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityVPNTools)
}

type ListVpnGatewaysInput struct{}

type VpnGatewayRetryInput struct {
	ID string `json:"id" jsonschema:"UUID of the VPN gateway to retry"`
}

func listVpnGateways(ctx context.Context, cl *client.Client, _ ListVpnGatewaysInput) (ItemsResult, error) {
	return itemsResult(cl.ListVpnGateways(ctx))
}

// retryVpnGateway force-redeploys a gateway stuck in "error" then waits until it
// is active again.
func retryVpnGateway(ctx context.Context, cl *client.Client, in VpnGatewayRetryInput) (VpnGatewayResult, error) {
	created, err := cl.RetryVpnGateway(ctx, in.ID)
	if err != nil {
		return VpnGatewayResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		id = in.ID
	}
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetVpnGateway(ctx, id) },
		"status", []string{"active"}, []string{"error"}, defaultCreateTimeout)
	if err != nil {
		return VpnGatewayResult{}, fmt.Errorf("vpn gateway %s did not become active after retry: %w", id, err)
	}
	obj, err := cl.GetVpnGateway(ctx, id)
	if err != nil {
		return VpnGatewayResult{}, err
	}
	return VpnGatewayResult{Gateway: obj}, nil
}

func registerParityVPNTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.vpn_gateway.list", Description: "List all VPN gateways owned by the caller."}, listVpnGateways)
	Register(s, deps, Spec{Name: "user.vpn_gateway.retry", Description: "Retry a failed VPN gateway deployment and wait until active."}, retryVpnGateway)
}
