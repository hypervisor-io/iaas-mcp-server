package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin catalog reads: plan catalogs (instance/lb/db/volume/s3), ISOs, images,
// Cloud Service locations/plan-groups, and currencies. READ-ONLY. Plan and
// currency create/update/delete are EXCLUDED (pricing/catalog mutations are
// kept off the allowlist; currency/credit changes border on billing).

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminCatalogTools)
}

func registerAdminCatalogTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.instance_plan.list", Description: "List instance plans (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListInstancePlans(ctx))
		})
	Register(s, deps, Spec{Name: "admin.instance_plan.get", Description: "Get an instance plan by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetInstancePlan(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.iso.list", Description: "List ISOs (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListISOs(ctx))
		})
	Register(s, deps, Spec{Name: "admin.image.list", Description: "List images (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListImages(ctx))
		})
	Register(s, deps, Spec{Name: "admin.lb_plan.list", Description: "List load balancer plans (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListLbPlans(ctx))
		})
	Register(s, deps, Spec{Name: "admin.db_plan.list", Description: "List database plans (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListDbPlans(ctx))
		})
	Register(s, deps, Spec{Name: "admin.volume_plan.list", Description: "List volume plans (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListVolumePlans(ctx))
		})
	Register(s, deps, Spec{Name: "admin.s3_plan.list", Description: "List S3 plans (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListS3Plans(ctx))
		})
	Register(s, deps, Spec{Name: "admin.cs_location.list", Description: "List Cloud Service locations (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListCsLocations(ctx))
		})
	Register(s, deps, Spec{Name: "admin.cs_location.get", Description: "Get a Cloud Service location by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetCsLocation(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.cs_plan_group.list", Description: "List Cloud Service plan groups (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListCsPlanGroups(ctx))
		})
	Register(s, deps, Spec{Name: "admin.currency.list", Description: "List Cloud Service currencies (admin, read-only).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListCurrencies(ctx))
		})
}
