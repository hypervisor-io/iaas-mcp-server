package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Proxmox-only instance tools: PVE VM snapshots, tags, live guest IPs (via the
// QEMU guest agent), and PBS backup file browsing. These mirror the client's
// user-surface Proxmox methods (client/proxmox.go) one-to-one; a non-Proxmox
// instance 422s on any of these and that error surfaces through the shared
// MapError path like any other validation failure.

func init() {
	toolRegistrars = append(toolRegistrars, registerInstanceProxmoxTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

// SnapshotListInput identifies the instance whose snapshots are listed.
type SnapshotListInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
}

// SnapshotCreateInput is user.instance.snapshot.create's arguments. VMState
// optionally includes the VM's RAM state in the snapshot (slower, allows a
// live-state rollback).
type SnapshotCreateInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	Name       string `json:"name" jsonschema:"snapshot name"`
	VMState    bool   `json:"vmstate,omitempty" jsonschema:"optional: also capture the VM's RAM state"`
}

// SnapshotActionInput identifies a snapshot by name for rollback/delete.
// Confirmation is embedded because both actions are destructive: rollback
// discards all disk state written since the snapshot was taken, and delete is
// irreversible.
type SnapshotActionInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	Name       string `json:"name" jsonschema:"snapshot name"`
	Confirmation
}

// SnapshotListResult wraps the instance's PVE snapshot list plus its count.
type SnapshotListResult struct {
	Snapshots []map[string]any `json:"snapshots"`
	Count     int              `json:"count"`
}

// SetTagsInput sets the comma-separated VM tags shown in the Proxmox UI. Tags
// may be empty to clear all tags.
type SetTagsInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	Tags       string `json:"tags" jsonschema:"comma-separated tags; empty clears all tags"`
}

// GuestIPsInput identifies the instance whose live guest IPs are read.
type GuestIPsInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
}

// GuestIPsResult wraps the QEMU guest agent's reported network interfaces plus
// their count.
type GuestIPsResult struct {
	Interfaces []map[string]any `json:"interfaces"`
	Count      int              `json:"count"`
}

// BackupFilesInput browses one directory level inside a PBS-backed backup.
// Filepath is optional; the Master defaults it to "/" when omitted.
type BackupFilesInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
	BackupID   string `json:"backup_id" jsonschema:"ID of the PBS backup to browse"`
	Filepath   string `json:"filepath,omitempty" jsonschema:"optional path within the backup to list; defaults to /"`
}

// BackupFilesResult wraps the raw file-listing envelope. It is returned bare
// (not unwrapped to a bare []map[string]any) because the client method itself
// returns the whole {"success":true,"files":[...]} envelope unmodified.
type BackupFilesResult struct {
	Listing map[string]any `json:"listing"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listInstanceSnapshots(ctx context.Context, cl *client.Client, in SnapshotListInput) (SnapshotListResult, error) {
	items, err := cl.ListInstanceSnapshots(ctx, in.InstanceID)
	if err != nil {
		return SnapshotListResult{}, err
	}
	return SnapshotListResult{Snapshots: items, Count: len(items)}, nil
}

func createInstanceSnapshot(ctx context.Context, cl *client.Client, in SnapshotCreateInput) (OKResult, error) {
	if _, err := cl.CreateInstanceSnapshot(ctx, in.InstanceID, in.Name, in.VMState); err != nil {
		return OKResult{}, err
	}
	return okResult("snapshot creation queued"), nil
}

func rollbackInstanceSnapshot(ctx context.Context, cl *client.Client, in SnapshotActionInput) (OKResult, error) {
	if _, err := cl.RollbackInstanceSnapshot(ctx, in.InstanceID, in.Name); err != nil {
		return OKResult{}, err
	}
	return okResult("snapshot rollback started"), nil
}

func deleteInstanceSnapshot(ctx context.Context, cl *client.Client, in SnapshotActionInput) (OKResult, error) {
	if _, err := cl.DeleteInstanceSnapshot(ctx, in.InstanceID, in.Name); err != nil {
		return OKResult{}, err
	}
	return okResult("snapshot deleted"), nil
}

func setInstanceTags(ctx context.Context, cl *client.Client, in SetTagsInput) (OKResult, error) {
	if _, err := cl.SetInstanceTags(ctx, in.InstanceID, in.Tags); err != nil {
		return OKResult{}, err
	}
	return okResult("tags updated"), nil
}

func instanceGuestIPs(ctx context.Context, cl *client.Client, in GuestIPsInput) (GuestIPsResult, error) {
	items, err := cl.GetInstanceGuestIPs(ctx, in.InstanceID)
	if err != nil {
		return GuestIPsResult{}, err
	}
	return GuestIPsResult{Interfaces: items, Count: len(items)}, nil
}

func instanceBackupFiles(ctx context.Context, cl *client.Client, in BackupFilesInput) (BackupFilesResult, error) {
	listing, err := cl.ListInstanceBackupFiles(ctx, in.InstanceID, in.BackupID, in.Filepath)
	if err != nil {
		return BackupFilesResult{}, err
	}
	return BackupFilesResult{Listing: listing}, nil
}

// ── registration ────────────────────────────────────────────────────────────

func registerInstanceProxmoxTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.instance.snapshot.list",
		Description: "List PVE VM snapshots for a Proxmox instance.",
	}, listInstanceSnapshots)
	Register(s, deps, Spec{
		Name:        "user.instance.snapshot.create",
		Description: "Create a PVE VM snapshot, optionally including the VM's RAM state.",
	}, createInstanceSnapshot)
	Register(s, deps, Spec{
		Name:        "user.instance.snapshot.rollback",
		Description: "Roll the instance back to a snapshot, discarding disk state written since it was taken. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, rollbackInstanceSnapshot)
	Register(s, deps, Spec{
		Name:        "user.instance.snapshot.delete",
		Description: "Delete a PVE VM snapshot. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteInstanceSnapshot)
	Register(s, deps, Spec{
		Name:        "user.instance.tags.set",
		Description: "Set the comma-separated VM tags shown in the Proxmox UI (empty clears all tags).",
	}, setInstanceTags)
	Register(s, deps, Spec{
		Name:        "user.instance.guest_ips",
		Description: "Get the instance's live guest IPs as discovered via the QEMU guest agent (Proxmox only).",
	}, instanceGuestIPs)
	Register(s, deps, Spec{
		Name:        "user.instance.backup.files",
		Description: "Browse file/directory entries inside a PBS-backed Proxmox backup.",
	}, instanceBackupFiles)
}
