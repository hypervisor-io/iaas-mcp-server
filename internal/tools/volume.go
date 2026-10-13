package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/hypervisor-io/terraform-provider-iaas/volumeop"
	"github.com/hypervisor-io/terraform-provider-iaas/waiter"
)

// Volume tools, mirroring the iaas_volume and iaas_volume_snapshot resources.
// Volume create is async (poll status to "available").
//
// Attach, detach, resize and delete are CONFIRMED asynchronous operations on
// the platform: the API records the operation (the volume's active_operation
// names it) and the attachment, plan and size (or the volume itself) change
// only when the node confirms. These tools WAIT, with a bounded timeout, for
// that confirmed outcome through the provider's volumeop package, so the MCP
// server and the OpenTofu provider share one definition of done/failed/refused:
//   - confirmed: the tool returns the final volume object;
//   - node failure: a tool error saying the operation failed on the node and
//     the volume kept its previous state;
//   - refusal: a tool error carrying the API's reason code;
//   - timeout: a tool error saying the operation is still pending (it is NOT
//     cancelled); user.volume.get is the status tool, its active_operation
//     field names a still-pending operation and is null once it ended.
// A short per-volume cooldown between operations is waited out inside the
// same timeout.
//
// Snapshots have no SHOW route or id-returning create: create enqueues, then
// the id is resolved by (unique) name, then readiness is polled; delete
// converges by polling to 404.

// volumeOpTimeout bounds one confirmed volume mutation tool call, including
// any wait for another in-flight operation and the per-volume cooldown. A var
// only so the package test can shrink it.
var volumeOpTimeout = 30 * time.Minute

func volumeOpOptions() volumeop.Options {
	return volumeop.Options{Interval: pollInterval(), Timeout: volumeOpTimeout}
}

// volumeAdmitTimeout bounds the admission wait of a snapshot or backup
// create (busy, the per-volume cooldown of at most 300 seconds, the account
// in-flight limit). A var only so the package test can shrink it.
var volumeAdmitTimeout = 10 * time.Minute

func volumeAdmitOptions() volumeop.Options {
	return volumeop.Options{Interval: pollInterval(), Timeout: volumeAdmitTimeout}
}

// volumeAdmitNote is appended to the snapshot and backup create tools.
const volumeAdmitNote = " While another operation runs on the volume, during the platform's short " +
	"per-volume cooldown (the API names the seconds left) or while the account is at its " +
	"in-flight limit, the tool waits (bounded, 10 minutes) and asks again; any other refusal " +
	"is returned with its reason code. A request that got no definite answer is never repeated."

func init() {
	toolRegistrars = append(toolRegistrars, registerVolumeTools)
}

// ── volume inputs / outputs ─────────────────────────────────────────────────

type CreateVolumeInput struct {
	Name              string `json:"name" jsonschema:"volume name"`
	VolumePlanID      string `json:"volume_plan_id" jsonschema:"UUID of the volume plan (size/type)"`
	LocationID        string `json:"location_id,omitempty" jsonschema:"UUID of the location (hypervisor group)"`
	HypervisorGroupID string `json:"hypervisor_group_id,omitempty" jsonschema:"deprecated alias for location_id"`
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
	Name     string `json:"name" jsonschema:"snapshot name (unique per volume, nonempty, no leading or trailing whitespace, must not be 0)"`
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
		"name":           in.Name,
		"volume_plan_id": in.VolumePlanID,
		"location_id":    resolveLocationID(in.LocationID, in.HypervisorGroupID),
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
	obj, err := volumeop.AttachVolume(ctx, cl, in.ID, in.InstanceID, volumeOpOptions())
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

func detachVolume(ctx context.Context, cl *client.Client, in DetachVolumeInput) (VolumeResult, error) {
	obj, err := volumeop.DetachVolume(ctx, cl, in.ID, volumeOpOptions())
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

func resizeVolume(ctx context.Context, cl *client.Client, in ResizeVolumeInput) (VolumeResult, error) {
	obj, err := volumeop.ResizeVolume(ctx, cl, in.ID, in.VolumePlanID, volumeOpOptions())
	if err != nil {
		return VolumeResult{}, err
	}
	return VolumeResult{Volume: obj}, nil
}

// deleteVolume does NOT detach first: an attached volume is refused by the API
// with reason volume_attached, which the tool error carries; call
// user.volume.detach first.
func deleteVolume(ctx context.Context, cl *client.Client, in DeleteVolumeInput) (DeleteResult, error) {
	if err := volumeop.DeleteVolume(ctx, cl, in.ID, volumeOpOptions()); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// ── snapshot handlers ───────────────────────────────────────────────────────

func createVolumeSnapshot(ctx context.Context, cl *client.Client, in CreateVolumeSnapshotInput) (SnapshotResult, error) {
	// The platform does not enforce unique snapshot names per volume and the
	// new snapshot is found by its name: refuse a name already in use, so the
	// tool can never return (or lead to the deletion of) a snapshot it did
	// not create.
	before, err := volumeop.CheckSnapshotNameFree(ctx, cl, in.VolumeID, in.Name)
	if err != nil {
		return SnapshotResult{}, err
	}

	// ONE attempt per send; only the gate's transient, definite refusals are
	// waited out and sent again (volumeop.Admit).
	// Before every repeated send the pre-send set is refreshed, so a same-name
	// snapshot someone else made during an admission wait is never taken for
	// ours (volumeop.SnapshotIDsBeforeSend).
	attempt := 0
	err = volumeop.Admit(ctx, in.VolumeID, volumeop.Snapshot, volumeAdmitOptions(), func(ctx context.Context) error {
		if attempt > 0 {
			before = volumeop.SnapshotIDsBeforeSend(ctx, cl, in.VolumeID, in.Name, before)
		}
		attempt++
		_, err := cl.CreateVolumeSnapshot(ctx, in.VolumeID, map[string]any{"name": in.Name})
		return err
	})

	var snapshotID string
	if err != nil {
		if !volumeop.IsUncertain(err) {
			return SnapshotResult{}, err
		}
		// No definite answer: the snapshot row is written in the create
		// request itself, so a read-back decides. Only a snapshot of this
		// name that did not exist before the send is adopted; it is never
		// sent again. %v, not %w: MapError must not reduce this to the bare
		// HTTP status.
		id, aerr := volumeop.AdoptAfterUncertain(ctx, cl, in.VolumeID, in.Name, before)
		if aerr != nil {
			return SnapshotResult{}, fmt.Errorf("snapshot create for %q on volume %s got no definite answer (%v) and was not repeated: %v",
				in.Name, in.VolumeID, err, aerr)
		}
		snapshotID = id
	}

	// The create response returns only the backup queue, not the snapshot id;
	// resolve it as the single snapshot of that name that was not there before
	// the send, tolerating a brief lag. Several new same-name snapshots are an
	// error, never a guess.
	if snapshotID == "" {
		err = waiter.WaitFor(ctx, waiter.Options{
			Interval: pollInterval(),
			Timeout:  defaultCreateTimeout,
			Refresh: func() (string, bool, error) {
				id, found, err := volumeop.NewSnapshotID(ctx, cl, in.VolumeID, in.Name, before)
				if err != nil {
					return "", false, err
				}
				if !found {
					return "resolving", false, nil
				}
				snapshotID = id
				return "resolved", true, nil
			},
		})
		if err != nil {
			return SnapshotResult{}, fmt.Errorf("snapshot %q did not appear: %w", in.Name, err)
		}
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

// volumeOpNote is appended to every confirmed volume mutation tool.
const volumeOpNote = " A node failure (the volume keeps its previous state) or a refusal (with " +
	"its reason code) is returned as an error. The wait is bounded (30 minutes, including the " +
	"platform's short per-volume cooldown); on timeout the operation is NOT cancelled, " +
	"follow it with user.volume.get."

func registerVolumeTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.volume.create",
		Description: "Create a block volume and wait until it is available." + locationIDAliasNote,
	}, createVolume)
	Register(s, deps, Spec{Name: "user.volume.list", Description: "List all volumes owned by the caller."}, listVolumes)
	Register(s, deps, Spec{
		Name: "user.volume.get",
		Description: "Get a volume by UUID. Its active_operation field names a still-pending " +
			"attach, detach, resize or delete (null once the operation ended).",
	}, getVolume)
	Register(s, deps, Spec{
		Name: "user.volume.attach",
		Description: "Attach a volume to an instance and wait until the node confirmed the " +
			"attachment." + volumeOpNote,
	}, attachVolume)
	Register(s, deps, Spec{
		Name:        "user.volume.detach",
		Description: "Detach a volume from its instance and wait until the node confirmed it." + volumeOpNote,
	}, detachVolume)
	Register(s, deps, Spec{
		Name: "user.volume.resize",
		Description: "Resize a volume to a larger volume plan and wait until the node confirmed " +
			"the new capacity (size and plan change only then)." + volumeOpNote,
	}, resizeVolume)
	Register(s, deps, Spec{
		Name: "user.volume.delete",
		Description: "Delete a volume and wait until the node confirmed the deletion. An attached " +
			"volume is refused (reason volume_attached): detach it first." + volumeOpNote +
			" DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVolume)

	Register(s, deps, Spec{
		Name: "user.volume.snapshot_create",
		Description: "Create a snapshot of a volume and wait until it is available. The name must be unique on the " +
			"volume: a name already used there is refused (the new snapshot is found by its name). " +
			"Names must be nonempty, have no leading or trailing whitespace, and must not be the literal \"0\"." +
			volumeAdmitNote,
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
