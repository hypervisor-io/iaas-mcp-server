package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin compute reads: instances (fleet-wide) and tasks. Read-only. Admin
// instance destroy, deploy, power, and modify are intentionally NOT exposed
// (see admin-phase4-excluded.md).

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminComputeTools)
}

func registerAdminComputeTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.instance.list", Description: "List all instances across the fleet (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListInstances(ctx))
		})
	Register(s, deps, Spec{Name: "admin.instance.get", Description: "Get any instance by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetInstance(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.instance.list_by_user", Description: "List a user's instances (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminUserIDInput) (AdminListResult, error) {
			return adminList(cl.AdminListUserInstances(ctx, in.UserID))
		})
	Register(s, deps, Spec{Name: "admin.instance.backups", Description: "List an instance's backups (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminListResult, error) {
			return adminList(cl.AdminGetInstanceBackups(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.instance.ips", Description: "List an instance's IP addresses (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminListResult, error) {
			return adminList(cl.AdminGetInstanceIPs(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.instance.disks", Description: "List an instance's disks (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminListResult, error) {
			return adminList(cl.AdminGetInstanceDisks(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.instance.metrics", Description: "Get an instance's metrics (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetInstanceMetrics(ctx, in.ID))
		})

	// Tasks.
	Register(s, deps, Spec{Name: "admin.task.list", Description: "List platform tasks (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListTasks(ctx))
		})
	Register(s, deps, Spec{Name: "admin.task.get", Description: "Get a task by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetTask(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.task.logs", Description: "Get a task's logs (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetTaskLogs(ctx, in.ID))
		})
}
