package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin object-storage reads: S3 servers, buckets, and access keys (fleet
// scope). READ-ONLY. S3 server/plan/bucket/key create/update/delete are
// EXCLUDED (infrastructure + credential mutations kept off the allowlist).

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminStorageTools)
}

func registerAdminStorageTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "admin.s3_server.list", Description: "List S3 servers (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListS3Servers(ctx))
		})
	Register(s, deps, Spec{Name: "admin.s3_server.get", Description: "Get an S3 server by UUID (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, in AdminIDInput) (AdminItemResult, error) {
			return adminItem(cl.AdminGetS3Server(ctx, in.ID))
		})
	Register(s, deps, Spec{Name: "admin.s3_bucket.list", Description: "List all S3 buckets across tenants (admin).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListS3Buckets(ctx))
		})
	Register(s, deps, Spec{Name: "admin.s3_access_key.list", Description: "List all S3 access keys across tenants (admin, secrets not included).", Admin: true},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (AdminListResult, error) {
			return adminList(cl.AdminListS3AccessKeys(ctx))
		})
}
