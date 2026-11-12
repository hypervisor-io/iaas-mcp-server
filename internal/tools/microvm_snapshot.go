package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/hypervisor-io/terraform-provider-iaas/waiter"
)

// Sandbox disk snapshots. A snapshot saves a sandbox's disk (not its memory) in the location the sandbox runs in
// and is billed per GiB-hour of the space it uses until deleted. A new sandbox can start from it: pass its id as
// snapshot_id to user.microvm.create.
//
//	user.microvm.snapshot_create  POST   /microvm/vm/{id}/snapshots   (waits for ready, up to wait_seconds)
//	user.microvm.snapshot_list    GET    /microvm/snapshots
//	user.microvm.snapshot_get     GET    /microvm/snapshot/{id}
//	user.microvm.snapshot_delete  DELETE /microvm/snapshot/{id}       (confirm-gated)

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmSnapshotTools)
}

const (
	snapshotDefaultWait = 300 * time.Second
	snapshotMaxWait     = 1800 * time.Second
)

type CreateMicrovmSnapshotInput struct {
	SandboxID   string `json:"sandbox_id" jsonschema:"UUID of the sandbox to snapshot (running, paused or stopped)"`
	Name        string `json:"name" jsonschema:"unique within the account: 1-64 characters of lowercase letters, digits, dot, underscore and hyphen"`
	Description string `json:"description,omitempty" jsonschema:"free text, at most 255 characters"`
	Consistency string `json:"consistency,omitempty" jsonschema:"auto (default), quiesced (fails when the file systems cannot be quiesced) or crash. The result reports the consistency achieved"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"how long to wait for the snapshot to become ready, default 300, at most 1800. Past that the result is returned with ready false and the snapshot keeps being taken: read it with user.microvm.snapshot_get"`
}

type ListMicrovmSnapshotsInput struct {
	SandboxID string `json:"sandbox_id,omitempty" jsonschema:"only snapshots taken from this sandbox"`
	Status    string `json:"status,omitempty" jsonschema:"creating, ready, failed, deleting or unavailable"`
}

type GetMicrovmSnapshotInput struct {
	ID string `json:"id" jsonschema:"UUID of the snapshot"`
}

type DeleteMicrovmSnapshotInput struct {
	ID string `json:"id" jsonschema:"UUID of the snapshot to delete"`
	Confirmation
}

// MicrovmSnapshotResult is one snapshot. Snapshot carries id, name, description, kind, status (creating, ready,
// failed, deleting, unavailable), source_sandbox_id (null once that sandbox is deleted), source_state,
// consistency (quiesced, crash or clean: what was achieved), size_bytes, used_bytes, disk_gib (the plan disk of the
// source sandbox; whether a plan can start a sandbox from the snapshot follows the stored root filesystem size, any
// plan for a small one), vcpu, mem_mib, location_id, image_id, capture_method
// (reflink, paused_copy, offline_copy), paused_ms (how long the sandbox was paused), volumes_excluded (how many
// data volumes of the sandbox are NOT in the snapshot, which holds the root disk only), billable_gib, price_gib_hour,
// failure_reason, created_at, ready_at.
type MicrovmSnapshotResult struct {
	Snapshot map[string]any `json:"snapshot"`
	// Ready is true when status is ready: a sandbox can be started from it.
	Ready bool `json:"ready"`
}

type MicrovmSnapshotListResult struct {
	Snapshots []map[string]any `json:"snapshots"`
	Count     int              `json:"count"`
}

func snapshotResult(s map[string]any) MicrovmSnapshotResult {
	status, _ := s["status"].(string)
	return MicrovmSnapshotResult{Snapshot: s, Ready: status == "ready"}
}

func createMicrovmSnapshot(ctx context.Context, cl *client.Client, in CreateMicrovmSnapshotInput) (MicrovmSnapshotResult, error) {
	body := map[string]any{"name": in.Name, "kind": "disk"}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Consistency != "" {
		body["consistency"] = in.Consistency
	}
	// An empty key makes the client send a fresh Idempotency-Key; the client retries only the transport.
	snap, err := cl.CreateMicrovmSnapshot(ctx, in.SandboxID, body, "")
	if err != nil {
		return MicrovmSnapshotResult{}, err
	}
	id, _ := snap["id"].(string)
	if id == "" {
		return MicrovmSnapshotResult{}, fmt.Errorf("the platform accepted the snapshot but returned no id; list snapshots before retrying")
	}
	wait := snapshotDefaultWait
	if in.WaitSeconds > 0 {
		wait = time.Duration(in.WaitSeconds) * time.Second
	}
	if wait > snapshotMaxWait {
		wait = snapshotMaxWait
	}
	var last = snap
	err = waiter.WaitFor(ctx, waiter.Options{
		Interval: pollInterval(),
		Timeout:  wait,
		Refresh: func() (string, bool, error) {
			cur, err := cl.GetMicrovmSnapshot(ctx, id)
			if err != nil {
				return "unknown", false, err
			}
			last = cur
			status, _ := cur["status"].(string)
			switch status {
			case "ready":
				return status, true, nil
			case "failed":
				reason, _ := cur["failure_reason"].(string)
				if reason == "" {
					reason = "unknown"
				}
				return status, false, fmt.Errorf("the snapshot failed (reason: %s)%s", reason, snapshotFailureDetail(cur))
			case "deleting", "unavailable":
				return status, false, fmt.Errorf("the snapshot is %s", status)
			}
			return status, false, nil
		},
	})
	if err != nil && ctx.Err() == nil && isWaitTimeout(err) {
		// Still being taken: hand back what we know, not an error.
		return snapshotResult(last), nil
	}
	if err != nil {
		return MicrovmSnapshotResult{}, err
	}

	return snapshotResult(last), nil
}

// snapshotFailureDetail renders failure_detail (message, pause estimate and limit in milliseconds) for an error text.
func snapshotFailureDetail(snap map[string]any) string {
	detail, ok := snap["failure_detail"].(map[string]any)
	if !ok {
		return ""
	}
	out := ""
	if m, ok := detail["message"].(string); ok && m != "" {
		out += ": " + m
	}
	est, hasEst := detail["pause_estimate_ms"].(float64)
	limit, hasLimit := detail["pause_limit_ms"].(float64)
	if hasEst && hasLimit {
		out += fmt.Sprintf(" (estimated pause %d ms, limit %d ms)", int64(est), int64(limit))
	}
	return out
}

func isWaitTimeout(err error) bool { return errors.Is(err, context.DeadlineExceeded) }

func listMicrovmSnapshots(ctx context.Context, cl *client.Client, in ListMicrovmSnapshotsInput) (MicrovmSnapshotListResult, error) {
	items, err := cl.ListMicrovmSnapshots(ctx, in.SandboxID, in.Status, "")
	if err != nil {
		return MicrovmSnapshotListResult{}, err
	}
	return MicrovmSnapshotListResult{Snapshots: items, Count: len(items)}, nil
}

func getMicrovmSnapshot(ctx context.Context, cl *client.Client, in GetMicrovmSnapshotInput) (MicrovmSnapshotResult, error) {
	s, err := cl.GetMicrovmSnapshot(ctx, in.ID)
	if err != nil {
		return MicrovmSnapshotResult{}, err
	}
	return snapshotResult(s), nil
}

func deleteMicrovmSnapshot(ctx context.Context, cl *client.Client, in DeleteMicrovmSnapshotInput) (OKResult, error) {
	if err := cl.DeleteMicrovmSnapshot(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("deleting: billing for the snapshot has stopped and it disappears once its location confirms the delete"), nil
}

func registerMicrovmSnapshotTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.snapshot_create", Description: "Save a sandbox's disk as a snapshot and wait until it is ready (up to wait_seconds; past that it returns ready false and the snapshot keeps being taken). Memory is not saved: a sandbox started from the snapshot (user.microvm.create with snapshot_id) boots cold from the saved disk. The sandbox must be running, paused or stopped. On locations without a fast-copy file system a running sandbox may pause for a few seconds while its disk is copied (the result's paused_ms says how long, capture_method says how); pausing or stopping it first avoids that, and when the pause would be too long the call is refused with snapshot_requires_pause. A snapshot is stored in the sandbox's location and billed per GiB-hour of the space it uses (price_gib_hour) until deleted; it survives the deletion of the sandbox. The location is asked first: a refusal it can name (snapshot_requires_pause with the estimated pause and the limit, quiesce_unavailable, node_insufficient_disk) comes back at once as this tool's error and nothing is created, so repeat the call after pausing the sandbox. Refused with a typed reason: name_taken, snapshot_limit, snapshot_in_progress, sandbox_state_invalid, quiesce_unavailable, insufficient_credit, rate_limited, node_insufficient_disk, node_snapshots_unsupported."}, createMicrovmSnapshot)
	Register(s, deps, Spec{Name: "user.microvm.snapshot_list", Description: "List the account's sandbox disk snapshots, newest first, optionally for one sandbox or one status. Never changes anything."}, listMicrovmSnapshots)
	Register(s, deps, Spec{Name: "user.microvm.snapshot_get", Description: "Read one sandbox disk snapshot: status, size, how it was captured, how long the sandbox was paused, and its price. Use it to follow a snapshot that user.microvm.snapshot_create returned with ready false. Never changes anything."}, getMicrovmSnapshot)
	Register(s, deps, Spec{Name: "user.microvm.snapshot_delete", Description: "Delete a sandbox disk snapshot. Billing for it stops at this request. Sandboxes already started from it keep running; new ones can no longer be started from it. Refused with snapshot_in_use while a sandbox is being created from it. DESTRUCTIVE and irreversible: requires \"confirm\": true.", Destructive: true}, deleteMicrovmSnapshot)
}
