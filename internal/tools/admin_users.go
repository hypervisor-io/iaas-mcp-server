package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin user reads: list/get users and list active sessions. READ-ONLY. User
// creation, update, deletion, password reset, 2FA disable, impersonation, and
// promotion are all EXCLUDED (see admin-phase4-excluded.md) - user deletion and
// impersonation-token minting are hard D3 exclusions, and the others are
// account-sensitive mutations kept off the allowlist.

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminUserTools)
}

func registerAdminUserTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.user.list", Description: "List all users (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListUsers(ctx))
		})
	Register(s, deps, Spec{Name: "admin.user.get", Description: "Get a user by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetUser(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.session.list", Description: "List active admin/user sessions (admin, read-only).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListSessions(ctx))
		})
}
