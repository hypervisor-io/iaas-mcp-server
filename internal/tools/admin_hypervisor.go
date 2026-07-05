package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin hypervisor tools: reads across hypervisors, groups, storages, backup
// storages, and backup plans, plus ONE safe reversible mutation - toggling a
// hypervisor's maintenance flag. Hypervisor create/update/destroy/force-destroy
// are EXCLUDED (destroy/decommission are hard D3 exclusions; create/update are
// heavy fleet config kept off the allowlist).

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminHypervisorTools)
}

// AdminSetMaintenanceInput toggles a hypervisor's maintenance flag. Reversible,
// so it is confirm-gated (a fleet node entering maintenance stops new
// deployments) and idempotency-key aware.
type AdminSetMaintenanceInput struct {
	ID      string `json:"id" jsonschema:"UUID of the hypervisor"`
	Enabled bool   `json:"enabled" jsonschema:"true to enter maintenance, false to leave it"`
	Confirmation
	Idempotent
}

func adminSetHypervisorMaintenance(ctx context.Context, cl *client.Client, in AdminSetMaintenanceInput) (AdminItemResult, error) {
	return adminItem(cl.AdminSetHypervisorMaintenance(ctx, in.ID, in.Enabled, IdempotencyKeyFromContext(ctx)))
}

func registerAdminHypervisorTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.hypervisor.list", Description: "List all hypervisors (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListHypervisors(ctx))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor.get", Description: "Get a hypervisor by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetHypervisor(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor.metrics", Description: "Get a hypervisor's metrics (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetHypervisorMetrics(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor.instance_stats", Description: "Get a hypervisor's instance stats (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetHypervisorInstanceStats(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor.ipv4_stats", Description: "Get a hypervisor's IPv4 allocation stats (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetHypervisorIPv4Stats(ctx, in.ID))
		})

	// Groups.
	Register(s, deps, Spec{Name: "admin.hypervisor_group.list", Description: "List hypervisor groups (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListHypervisorGroups(ctx))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor_group.get", Description: "Get a hypervisor group by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetHypervisorGroup(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor_group.hypervisors", Description: "List a group's hypervisors (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminListResult, error) {
			return adminList(cl.AdminGetHypervisorGroupHypervisors(ctx, in.ID))
		})

	// Storages.
	Register(s, deps, Spec{Name: "admin.hypervisor_storage.list", Description: "List hypervisor storages (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListHypervisorStorages(ctx))
		})
	Register(s, deps, Spec{Name: "admin.hypervisor_storage.get", Description: "Get a hypervisor storage by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetHypervisorStorage(ctx, in.ID))
		})

	// Backup storages.
	Register(s, deps, Spec{Name: "admin.backup_storage.list", Description: "List backup storages (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListBackupStorages(ctx))
		})
	Register(s, deps, Spec{Name: "admin.backup_storage.get", Description: "Get a backup storage by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetBackupStorage(ctx, in.ID))
		})

	// Backup plans.
	Register(s, deps, Spec{Name: "admin.backup_plan.list", Description: "List hypervisor backup plans (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListBackupPlans(ctx))
		})
	Register(s, deps, Spec{Name: "admin.backup_plan.get", Description: "Get a backup plan by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetBackupPlan(ctx, in.ID))
		})

	// Safe mutation: maintenance toggle (reversible, confirm-gated).
	Register(s, deps, Spec{
		Name:        "admin.hypervisor.set_maintenance",
		Description: "Enable or disable a hypervisor's maintenance mode (reversible). Requires \"confirm\": true.",
		Admin:       true,
		Destructive: true,
	}, adminSetHypervisorMaintenance)
}
