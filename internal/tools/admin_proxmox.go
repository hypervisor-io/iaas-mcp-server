package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Admin Proxmox tools: node issues (webssh-proxy install / tap-bandwidth
// collection failures surfaced by the Master's Proxmox sync), PVE-native
// scheduled backup jobs, and the maintenance-evacuation/rebalancing VM
// migration flow (distinct from the general InstanceMigrateService). Part of
// the D3 curated safe allowlist: node-issue retry/resolve and backup-job
// create/update/delete are scoped, reversible operational actions an admin
// already has full authority over, so they are safe additions to the
// allowlist. admin.instance.migrate is the one high-risk operation in this
// file - it moves a running VM to a different physical host - so it is
// Destructive and confirm-gated like any other D3 high-risk admin mutation,
// even though it is not a delete.

func init() {
	toolRegistrars = append(toolRegistrars, registerAdminProxmoxTools)
}

// ── node issues ──────────────────────────────────────────────────────────

// AdminNodeIssueListInput optionally filters node issues by status (e.g.
// "open", "resolved"); omitted returns every status.
type AdminNodeIssueListInput struct {
	Status string `json:"status,omitempty" jsonschema:"optional status filter (e.g. open, resolved)"`
}

// AdminNodeIssueIDInput identifies a node issue for retry/resolve.
type AdminNodeIssueIDInput struct {
	IssueID string `json:"issue_id" jsonschema:"UUID of the node issue"`
}

func adminListNodeIssues(ctx context.Context, cl *client.Client, in AdminNodeIssueListInput) (AdminListResult, error) {
	return adminList(cl.AdminListNodeIssues(ctx, in.Status))
}

func adminRetryNodeIssue(ctx context.Context, cl *client.Client, in AdminNodeIssueIDInput) (OKResult, error) {
	if _, err := cl.AdminRetryNodeIssue(ctx, in.IssueID); err != nil {
		return OKResult{}, err
	}
	return okResult("node issue retry queued"), nil
}

func adminResolveNodeIssue(ctx context.Context, cl *client.Client, in AdminNodeIssueIDInput) (OKResult, error) {
	if _, err := cl.AdminResolveNodeIssue(ctx, in.IssueID); err != nil {
		return OKResult{}, err
	}
	return okResult("node issue resolved"), nil
}

// ── PVE-native backup jobs ──────────────────────────────────────────────────

// AdminBackupJobListInput scopes the backup-job list to a hypervisor group.
type AdminBackupJobListInput struct {
	GroupID string `json:"group_id" jsonschema:"UUID of the hypervisor group"`
}

// AdminBackupJobCreateInput creates a PVE-native scheduled backup job. Data
// carries the Master's PveBackupJobService::createRules() fields (target_type,
// target_value, storage, schedule, mode, compress, enabled, comment, and the
// keep-* retention fields), passed through as-is.
type AdminBackupJobCreateInput struct {
	GroupID string         `json:"group_id" jsonschema:"UUID of the hypervisor group"`
	Data    map[string]any `json:"data" jsonschema:"backup job definition: schedule, selection mode, and retention as a nested keep object, e.g. {\"schedule\":\"02:00\",\"mode\":\"all\",\"keep\":{\"keep_daily\":7,\"keep_weekly\":4}}"`
}

// AdminBackupJobUpdateInput updates an existing PVE-native backup job. Data
// carries the Master's PveBackupJobService::updateRules() fields (all
// optional), passed through as-is.
type AdminBackupJobUpdateInput struct {
	GroupID string         `json:"group_id" jsonschema:"UUID of the hypervisor group"`
	JobID   string         `json:"job_id" jsonschema:"ID of the backup job"`
	Data    map[string]any `json:"data" jsonschema:"backup job definition fields to update (all optional): schedule, selection mode, and retention as a nested keep object, e.g. {\"schedule\":\"02:00\",\"mode\":\"all\",\"keep\":{\"keep_daily\":7,\"keep_weekly\":4}}"`
}

// AdminBackupJobDeleteInput deletes a PVE-native backup job. Confirmation is
// embedded because delete is irreversible.
type AdminBackupJobDeleteInput struct {
	GroupID string `json:"group_id" jsonschema:"UUID of the hypervisor group"`
	JobID   string `json:"job_id" jsonschema:"ID of the backup job to delete"`
	Confirmation
}

func adminListBackupJobs(ctx context.Context, cl *client.Client, in AdminBackupJobListInput) (AdminListResult, error) {
	return adminList(cl.AdminListBackupJobs(ctx, in.GroupID))
}

func adminCreateBackupJob(ctx context.Context, cl *client.Client, in AdminBackupJobCreateInput) (AdminItemResult, error) {
	return adminItem(cl.AdminCreateBackupJob(ctx, in.GroupID, in.Data))
}

func adminUpdateBackupJob(ctx context.Context, cl *client.Client, in AdminBackupJobUpdateInput) (AdminItemResult, error) {
	return adminItem(cl.AdminUpdateBackupJob(ctx, in.GroupID, in.JobID, in.Data))
}

func adminDeleteBackupJob(ctx context.Context, cl *client.Client, in AdminBackupJobDeleteInput) (OKResult, error) {
	if _, err := cl.AdminDeleteBackupJob(ctx, in.GroupID, in.JobID); err != nil {
		return OKResult{}, err
	}
	return okResult("backup job deleted"), nil
}

// ── PVE cluster migration (maintenance evacuation / rebalancing) ───────────

// AdminMigratePrecheckInput runs the pre-migration compatibility check.
// TargetNode is optional; when empty, PVE picks/reports on all candidate
// nodes.
type AdminMigratePrecheckInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	TargetNode string `json:"target_node,omitempty" jsonschema:"optional target PVE node; omitted checks all candidate nodes"`
}

// AdminMigrateInput moves a running VM to another node in the same PVE
// cluster. Confirmation is embedded and Spec.Destructive is set: this is the
// highest-risk operation in the curated admin allowlist (it moves a live,
// possibly customer-facing VM between physical hosts), so it requires
// explicit human confirmation like any other D3 high-risk admin mutation.
type AdminMigrateInput struct {
	InstanceID         string   `json:"instance_id" jsonschema:"UUID of the instance to migrate"`
	TargetNode         string   `json:"target_node" jsonschema:"destination PVE node"`
	Online             *bool    `json:"online,omitempty" jsonschema:"optional: live-migrate without stopping the VM"`
	Bwlimit            *float64 `json:"bwlimit,omitempty" jsonschema:"optional bandwidth limit in KiB/s"`
	TargetStorage      string   `json:"targetstorage,omitempty" jsonschema:"optional destination storage (defaults to the same storage id on the target node)"`
	MigrationNetwork   string   `json:"migration_network,omitempty" jsonschema:"optional CIDR to route migration traffic over"`
	WithLocalDisks     *bool    `json:"with_local_disks,omitempty" jsonschema:"optional: migrate local disks along with the VM"`
	WithConntrackState *bool    `json:"with_conntrack_state,omitempty" jsonschema:"optional: migrate the firewall conntrack state (online migration, PVE >= 9 only)"`
	Confirmation
}

func adminMigratePrecheck(ctx context.Context, cl *client.Client, in AdminMigratePrecheckInput) (AdminItemResult, error) {
	return adminItem(cl.AdminMigratePrecheck(ctx, in.InstanceID, in.TargetNode))
}

func adminMigrateInstance(ctx context.Context, cl *client.Client, in AdminMigrateInput) (OKResult, error) {
	opts := map[string]any{
		"target_node": in.TargetNode,
	}
	if in.Online != nil {
		opts["online"] = *in.Online
	}
	if in.Bwlimit != nil {
		opts["bwlimit"] = *in.Bwlimit
	}
	if in.TargetStorage != "" {
		opts["targetstorage"] = in.TargetStorage
	}
	if in.MigrationNetwork != "" {
		opts["migration_network"] = in.MigrationNetwork
	}
	if in.WithLocalDisks != nil {
		opts["with_local_disks"] = *in.WithLocalDisks
	}
	if in.WithConntrackState != nil {
		opts["with_conntrack_state"] = *in.WithConntrackState
	}
	if _, err := cl.AdminMigrateInstance(ctx, in.InstanceID, opts); err != nil {
		return OKResult{}, err
	}
	return okResult("migration started"), nil
}

// ── registration ────────────────────────────────────────────────────────────

func registerAdminProxmoxTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "admin.proxmox.node_issue.list",
		Description: "List Proxmox node issues (webssh-proxy install / tap-bandwidth collection failures), optionally filtered by status (admin).",
		Admin:       true,
	}, adminListNodeIssues)
	Register(s, deps, Spec{
		Name:        "admin.proxmox.node_issue.retry",
		Description: "Re-dispatch the fix job for a Proxmox node issue (admin).",
		Admin:       true,
	}, adminRetryNodeIssue)
	Register(s, deps, Spec{
		Name:        "admin.proxmox.node_issue.resolve",
		Description: "Mark a Proxmox node issue solved without retrying it (admin).",
		Admin:       true,
	}, adminResolveNodeIssue)

	Register(s, deps, Spec{
		Name:        "admin.backup_job.list",
		Description: "List PVE-native scheduled backup jobs for a hypervisor group (admin).",
		Admin:       true,
	}, adminListBackupJobs)
	Register(s, deps, Spec{
		Name:        "admin.backup_job.create",
		Description: "Create a PVE-native scheduled backup job for a hypervisor group (admin).",
		Admin:       true,
	}, adminCreateBackupJob)
	Register(s, deps, Spec{
		Name:        "admin.backup_job.update",
		Description: "Update a PVE-native scheduled backup job (admin).",
		Admin:       true,
	}, adminUpdateBackupJob)
	Register(s, deps, Spec{
		Name:        "admin.backup_job.delete",
		Description: "Delete a PVE-native scheduled backup job. DESTRUCTIVE: requires \"confirm\": true.",
		Admin:       true,
		Destructive: true,
	}, adminDeleteBackupJob)

	Register(s, deps, Spec{
		Name:        "admin.instance.migrate_precheck",
		Description: "Run the pre-migration compatibility check for moving a VM to another node in the same PVE cluster (admin).",
		Admin:       true,
	}, adminMigratePrecheck)
	Register(s, deps, Spec{
		Name:        "admin.instance.migrate",
		Description: "Migrate a running VM to another node in the same PVE cluster (maintenance evacuation / rebalancing). This moves a live, possibly customer-facing VM between physical hosts. DESTRUCTIVE: requires \"confirm\": true.",
		Admin:       true,
		Destructive: true,
	}, adminMigrateInstance)
}
