package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin operations: self-provisioning packs (read), migrations (read), system
// logs (read), and reverse-DNS requests (read + a reversible approve/reject
// safe mutation). SP pack create/update/delete, migration start/rollback/stop,
// and rDNS zone/provider mutations are EXCLUDED (see admin-phase4-excluded.md).

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminOpsTools)
}

// AdminRdnsProcessInput approves or rejects a pending reverse-DNS request. This
// is a reversible workflow decision, so it is confirm-gated and idempotency-key
// aware. action must be "approve" or "reject".
type AdminRdnsProcessInput struct {
	ID     string `json:"id" jsonschema:"UUID of the reverse-DNS request"`
	Action string `json:"action" jsonschema:"approve or reject"`
	Reason string `json:"reason,omitempty" jsonschema:"optional reason (shown to the requester)"`
	Confirmation
	Idempotent
}

func adminProcessRdnsRequest(ctx context.Context, cl *client.Client, in AdminRdnsProcessInput) (AdminItemResult, error) {
	if in.Action != "approve" && in.Action != "reject" {
		return AdminItemResult{}, fmt.Errorf("action must be \"approve\" or \"reject\", got %q", in.Action)
	}
	return adminItem(cl.AdminProcessRdnsRequest(ctx, in.ID, in.Action, in.Reason, IdempotencyKeyFromContext(ctx)))
}

func registerAdminOpsTools(s *mcp.Server, deps Deps) {
	// Self-provisioning packs (read).
	Register(s, deps, Spec{Name: "admin.self_provisioning_pack.list", Description: "List self-provisioning packs (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListSpPacks(ctx))
		})
	Register(s, deps, Spec{Name: "admin.self_provisioning_pack.get", Description: "Get a self-provisioning pack by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetSpPack(ctx, in.ID))
		})

	// Migrations (read).
	Register(s, deps, Spec{Name: "admin.migration.list", Description: "List instance migrations (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListMigrations(ctx))
		})
	Register(s, deps, Spec{Name: "admin.migration.get", Description: "Get a migration by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetMigration(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.migration.logs", Description: "Get a migration's logs (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetMigrationLogs(ctx, in.ID))
		})

	// System logs (read).
	Register(s, deps, Spec{Name: "admin.system.admin_logs", Description: "List admin audit logs (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminGetAdminLogs(ctx))
		})
	Register(s, deps, Spec{Name: "admin.system.email_log", Description: "List the outgoing email log (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminGetEmailLog(ctx))
		})
	Register(s, deps, Spec{Name: "admin.system.ip_log", Description: "List the IP audit log (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminGetIPLog(ctx))
		})

	// Reverse-DNS requests: read + safe approve/reject.
	Register(s, deps, Spec{Name: "admin.rdns_request.list", Description: "List pending reverse-DNS requests (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListRdnsRequests(ctx))
		})
	Register(s, deps, Spec{
		Name:        "admin.rdns_request.process",
		Description: "Approve or reject a reverse-DNS request (reversible workflow decision). Requires \"confirm\": true.",
		Admin:       true,
		Destructive: true,
	}, adminProcessRdnsRequest)
}
