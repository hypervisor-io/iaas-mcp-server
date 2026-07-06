package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Additional read-only catalog lookups (db/lb/vpn plans, lb/vpc locations,
// currencies), reusing the shared CatalogListResult.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityCatalogTools)
}

func registerParityCatalogTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.catalog.db_plans", Description: "List managed database plans."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListDbPlans(ctx))
		})
	Register(s, deps, Spec{Name: "user.catalog.lb_plans", Description: "List load balancer plans."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListLbPlans(ctx))
		})
	Register(s, deps, Spec{Name: "user.catalog.lb_locations", Description: "List load balancer locations."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListLbLocations(ctx))
		})
	Register(s, deps, Spec{Name: "user.catalog.vpc_locations", Description: "List VPC locations."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListVpcLocations(ctx))
		})
	Register(s, deps, Spec{Name: "user.catalog.vpn_plans", Description: "List VPN gateway plans."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListVpnGwPlans(ctx))
		})
	Register(s, deps, Spec{Name: "user.catalog.currencies", Description: "List Cloud Service currencies."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListCurrencies(ctx))
		})
}
