package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin network reads: subnets (+ their IPs, available IPs, statistics), the
// global IP pool, and VPCs. READ-ONLY. Subnet/IP create/update/delete and any
// bulk IP operations are EXCLUDED (bulk IP/subnet delete is a hard D3
// exclusion; single create/update are network-sensitive config kept off the
// allowlist).

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminNetworkTools)
}

func registerAdminNetworkTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.subnet.list", Description: "List subnets (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListSubnets(ctx))
		})
	Register(s, deps, Spec{Name: "admin.subnet.get", Description: "Get a subnet by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetSubnet(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.subnet.ips", Description: "List a subnet's IP addresses (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminListResult, error) {
			return adminList(cl.AdminGetSubnetIPs(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.subnet.available_ips", Description: "List a subnet's available IP addresses (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminListResult, error) {
			return adminList(cl.AdminGetSubnetAvailableIPs(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.subnet.statistics", Description: "Get a subnet's usage statistics (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetSubnetStatistics(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.ip.list", Description: "List IP addresses in the global pool (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListIPs(ctx))
		})
	Register(s, deps, Spec{Name: "admin.vpc.list", Description: "List all VPCs across tenants (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListVpcs(ctx))
		})
	Register(s, deps, Spec{Name: "admin.vpc.get", Description: "Get any VPC by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetVpc(ctx, in.ID))
		})
}
