package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Volume backup and snapshot-restore tools. These enqueue work and return a
// "queue" tracker object (not a task_id), so the tool returns that handle.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityVolumeTools)
}

type VolumeBackupCreateInput struct {
	VolumeID string `json:"volume_id" jsonschema:"UUID of the volume"`
	Name     string `json:"name,omitempty" jsonschema:"optional backup name"`
}

type VolumeBackupDeleteInput struct {
	VolumeID string `json:"volume_id" jsonschema:"UUID of the volume"`
	BackupID string `json:"backup_id" jsonschema:"UUID of the backup to delete"`
	Confirmation
}

// VolumeRestoreInput restores a volume backup or snapshot. mode is in_place or
// new_volume; for new_volume, volume_plan_id and hypervisor_group_id are required.
type VolumeRestoreInput struct {
	VolumeID          string `json:"volume_id" jsonschema:"UUID of the source volume"`
	SourceID          string `json:"source_id" jsonschema:"UUID of the backup or snapshot to restore"`
	Mode              string `json:"mode" jsonschema:"in_place or new_volume"`
	VolumePlanID      string `json:"volume_plan_id,omitempty" jsonschema:"new volume plan UUID (required for new_volume)"`
	HypervisorGroupID string `json:"hypervisor_group_id,omitempty" jsonschema:"hypervisor group UUID (required for new_volume)"`
	Name              string `json:"name,omitempty" jsonschema:"name for the new volume"`
	ProjectID         string `json:"project_id,omitempty" jsonschema:"optional project UUID"`
}

func restoreBody(in VolumeRestoreInput) map[string]any {
	body := map[string]any{"mode": in.Mode}
	if in.VolumePlanID != "" {
		body["volume_plan_id"] = in.VolumePlanID
	}
	if in.HypervisorGroupID != "" {
		body["hypervisor_group_id"] = in.HypervisorGroupID
	}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.ProjectID != "" {
		body["project_id"] = in.ProjectID
	}
	return body
}

func createVolumeBackup(ctx context.Context, cl *client.Client, in VolumeBackupCreateInput) (ObjectResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	return objectResult(cl.CreateVolumeBackup(ctx, in.VolumeID, body))
}

func deleteVolumeBackup(ctx context.Context, cl *client.Client, in VolumeBackupDeleteInput) (ObjectResult, error) {
	return objectResult(cl.DeleteVolumeBackup(ctx, in.VolumeID, in.BackupID))
}

func restoreVolumeBackup(ctx context.Context, cl *client.Client, in VolumeRestoreInput) (ObjectResult, error) {
	return objectResult(cl.RestoreVolumeBackup(ctx, in.VolumeID, in.SourceID, restoreBody(in)))
}

func restoreVolumeSnapshot(ctx context.Context, cl *client.Client, in VolumeRestoreInput) (ObjectResult, error) {
	return objectResult(cl.RestoreVolumeSnapshot(ctx, in.VolumeID, in.SourceID, restoreBody(in)))
}

func registerParityVolumeTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.volume.backup_create", Description: "Create a backup of a volume (returns an async queue handle)."}, createVolumeBackup)
	Register(s, deps, Spec{
		Name:        "user.volume.backup_delete",
		Description: "Delete a volume backup. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVolumeBackup)
	Register(s, deps, Spec{Name: "user.volume.backup_restore", Description: "Restore a volume backup in place or into a new volume."}, restoreVolumeBackup)
	Register(s, deps, Spec{Name: "user.volume.snapshot_restore", Description: "Restore a volume snapshot in place or into a new volume."}, restoreVolumeSnapshot)
}
