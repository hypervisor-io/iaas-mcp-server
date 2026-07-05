package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/hypervisor-io/terraform-provider-iaas/waiter"
)

// Volume tools, mirroring the iaas_volume and iaas_volume_snapshot resources.
// Volume create is async (poll status to "available"). Attach/detach/resize are
// synchronous. Snapshots have no SHOW route or id-returning create: create
// enqueues, then the id is resolved by (unique) name, then readiness is polled;
// delete converges by polling to 404.

func init() {
	toolRegistrars = append(toolRegistrars, registerVolumeTools)
}

// ── volume inputs / outputs ─────────────────────────────────────────────────

type CreateVolumeInput struct {
	Name              string `json:"name" jsonschema:"volume name"`
	VolumePlanID      string `json:"volume_plan_id" jsonschema:"UUID of the volume plan (size/type)"`
	HypervisorGroupID string `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (location)"`
	ProjectID         string `json:"project_id,omitempty" jsonschema:"optional project UUID"`
}

type GetVolumeInput struct {
	ID string `json:"id" jsonschema:"UUID of the volume"`
}

type ListVolumesInput struct{}

type AttachVolumeInput struct {
	ID         string `json:"id" jsonschema:"UUID of the volume"`
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance to attach to"`
}

type DetachVolumeInput struct {
	ID string `json:"id" jsonschema:"UUID of the volume to detach"`
}

type ResizeVolumeInput struct {
	ID           string `json:"id" jsonschema:"UUID of the volume"`
	VolumePlanID string `json:"volume_plan_id" jsonschema:"UUID of the new volume plan"`
}

type DeleteVolumeInput struct {
	ID string `json:"id" jsonschema:"UUID of the volume to delete"`
	Confirmation
}

type VolumeResult struct {
	Volume map[string]any `json:"volume"`
}

type VolumeListResult struct {
	Volumes []map[string]any `json:"volumes"`
	Count   int              `json:"count"`
}

// ── snapshot inputs / outputs ───────────────────────────────────────────────

type CreateVolumeSnapshotInput struct {
	VolumeID string `json:"volume_id" jsonschema:"UUID of the source volume"`
	Name     string `json:"name" jsonschema:"snapshot name (must be unique per volume)"`
}

type GetVolumeSnapshotInput struct {
	VolumeID   string `json:"volume_id" jsonschema:"UUID of the volume"`
	SnapshotID string `json:"snapshot_id" jsonschema:"UUID of the snapshot"`
}

type DeleteVolumeSnapshotInput struct {
	VolumeID   string `json:"volume_id" jsonschema:"UUID of the volume"`
	SnapshotID string `json:"snapshot_id" jsonschema:"UUID of the snapshot to delete"`
	Confirmation
}

type SnapshotResult struct {
	Snapshot map[string]any `json:"snapshot"`
}

// ── volume handlers ─────────────────────────────────────────────────────────

func createVolume(ctx context.Context, cl *client.Client, in CreateVolumeInput) (VolumeResult, error) {
	body := map[string]any{
		"name":                in.Name,
		"volume_plan_id":      in.VolumePlanID,
		"hypervisor_group_id": in.HypervisorGroupID,
	}
	if in.ProjectID != "" {
		body["project_id"] = in.ProjectID
	}
	created, err := cl.CreateVolume(ctx, body)
	if err != nil {
		return VolumeResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return VolumeResult{}, fmt.Errorf("create response did not include a volume id")
	}
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetVolume(ctx, id) },
		"status", []string{"available"}, []string{"failed", "error"}, defaultCreateTimeout)
	if err != nil {
		return VolumeResult{}, fmt.Errorf("volume %s did not become available: %w", id, err)
	}
	obj, err := cl.GetVolume(ctx, id)
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

func getVolume(ctx context.Context, cl *client.Client, in GetVolumeInput) (VolumeResult, error) {
	obj, err := cl.GetVolume(ctx, in.ID)
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

func listVolumes(ctx context.Context, cl *client.Client, _ ListVolumesInput) (VolumeListResult, error) {
	items, err := cl.ListVolumes(ctx)
	if err != nil {
		return VolumeListResult{}, err
	}
	return VolumeListResult{Volumes: items, Count: len(items)}, nil
}

func attachVolume(ctx context.Context, cl *client.Client, in AttachVolumeInput) (VolumeResult, error) {
	obj, err := cl.AttachVolume(ctx, in.ID, map[string]any{"instance_id": in.InstanceID})
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

func detachVolume(ctx context.Context, cl *client.Client, in DetachVolumeInput) (VolumeResult, error) {
	obj, err := cl.DetachVolume(ctx, in.ID)
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

func resizeVolume(ctx context.Context, cl *client.Client, in ResizeVolumeInput) (VolumeResult, error) {
	// Resize returns the bare envelope {success,is_downgrade,volume}.
	env, err := cl.ResizeVolume(ctx, in.ID, map[string]any{"volume_plan_id": in.VolumePlanID})
	if err != nil {
		return VolumeResult{}, err
	}
	if vol, ok := env["volume"].(map[string]any); ok {
		return VolumeResult{Volume: vol}, nil
	}
	return VolumeResult{Volume: env}, nil
}

func deleteVolume(ctx context.Context, cl *client.Client, in DeleteVolumeInput) (DeleteResult, error) {
	if err := cl.DeleteVolume(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// ── snapshot handlers ───────────────────────────────────────────────────────

func createVolumeSnapshot(ctx context.Context, cl *client.Client, in CreateVolumeSnapshotInput) (SnapshotResult, error) {
	if _, err := cl.CreateVolumeSnapshot(ctx, in.VolumeID, map[string]any{"name": in.Name}); err != nil {
		return SnapshotResult{}, err
	}

	// The create response returns only the backup queue, not the snapshot id;
	// resolve the id by (unique) name, tolerating not-found while it appears.
	var snapshotID string
	err := waiter.WaitFor(ctx, waiter.Options{
		Interval: pollInterval(),
		Timeout:  defaultCreateTimeout,
		Refresh: func() (string, bool, error) {
			obj, err := cl.FindVolumeSnapshotByName(ctx, in.VolumeID, in.Name)
			if err != nil {
				if client.IsNotFound(err) {
					return "resolving", false, nil
				}
				return "", false, err
			}
			if id, _ := obj["id"].(string); id != "" {
				snapshotID = id
				return "resolved", true, nil
			}
			return "resolving", false, nil
		},
	})
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot %q did not appear: %w", in.Name, err)
	}

	// Now poll readiness.
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetVolumeSnapshot(ctx, in.VolumeID, snapshotID) },
		"status", []string{"available"}, []string{"failed"}, defaultCreateTimeout)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("snapshot %s did not become available: %w", snapshotID, err)
	}
	obj, err := cl.GetVolumeSnapshot(ctx, in.VolumeID, snapshotID)
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{Snapshot: obj}, nil
}

func getVolumeSnapshot(ctx context.Context, cl *client.Client, in GetVolumeSnapshotInput) (SnapshotResult, error) {
	obj, err := cl.GetVolumeSnapshot(ctx, in.VolumeID, in.SnapshotID)
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{Snapshot: obj}, nil
}

func deleteVolumeSnapshot(ctx context.Context, cl *client.Client, in DeleteVolumeSnapshotInput) (DeleteResult, error) {
	if err := cl.DeleteVolumeSnapshot(ctx, in.VolumeID, in.SnapshotID); err != nil {
		return DeleteResult{}, err
	}
	// Async delete: converge by polling the snapshot to 404.
	err := waitForGone(ctx,
		func() (map[string]any, error) { return cl.GetVolumeSnapshot(ctx, in.VolumeID, in.SnapshotID) },
		defaultDeleteTimeout)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("snapshot %s was not removed: %w", in.SnapshotID, err)
	}
	return DeleteResult{ID: in.SnapshotID, Deleted: true}, nil
}

func registerVolumeTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.volume.create",
		Description: "Create a block volume and wait until it is available.",
	}, createVolume)
	Register(s, deps, Spec{Name: "user.volume.list", Description: "List all volumes owned by the caller."}, listVolumes)
	Register(s, deps, Spec{Name: "user.volume.get", Description: "Get a volume by UUID."}, getVolume)
	Register(s, deps, Spec{Name: "user.volume.attach", Description: "Attach a volume to an instance."}, attachVolume)
	Register(s, deps, Spec{Name: "user.volume.detach", Description: "Detach a volume from its instance."}, detachVolume)
	Register(s, deps, Spec{Name: "user.volume.resize", Description: "Resize a volume to a new volume plan."}, resizeVolume)
	Register(s, deps, Spec{
		Name:        "user.volume.delete",
		Description: "Delete a volume. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVolume)

	Register(s, deps, Spec{
		Name:        "user.volume.snapshot_create",
		Description: "Create a snapshot of a volume (name must be unique per volume) and wait until it is available.",
	}, createVolumeSnapshot)
	Register(s, deps, Spec{
		Name:        "user.volume.snapshot_get",
		Description: "Get a volume snapshot by volume UUID and snapshot UUID.",
	}, getVolumeSnapshot)
	Register(s, deps, Spec{
		Name:        "user.volume.snapshot_delete",
		Description: "Delete a volume snapshot and wait until removed. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVolumeSnapshot)
}
