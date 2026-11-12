package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Persistent data volumes for sandboxes. A volume is created with user.volume.create and for "sandbox", attached when the
// sandbox is created (volumes on user.microvm.create) or while the sandbox is STOPPED, and deleted with
// user.volume.delete. It keeps its data when the sandbox stops, pauses, resumes and (unless delete_on_sandbox_delete) is
// deleted; it lives on one node and the sandbox that uses it runs there.
//
//	user.microvm.volume_list    GET    /microvm/vm/{id}/volumes
//	user.microvm.volume_attach  POST   /microvm/vm/{id}/volumes          (sandbox stopped)
//	user.microvm.volume_detach  DELETE /microvm/vm/{id}/volume/{volume}  (sandbox stopped, confirm-gated)

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmVolumeTools)
}

type ListMicrovmVolumesInput struct {
	SandboxID string `json:"sandbox_id" jsonschema:"UUID of the sandbox"`
}

type AttachMicrovmVolumeInput struct {
	SandboxID             string `json:"sandbox_id" jsonschema:"UUID of the sandbox; it must be stopped (user.microvm.stop)"`
	VolumeID              string `json:"volume_id" jsonschema:"UUID of a free sandbox volume (user.volume.create with for sandbox) on the same node as the sandbox"`
	MountPath             string `json:"mount_path,omitempty" jsonschema:"absolute path where the guest mounts it: letters, digits and . _ + - /, not inside a system directory. Default /mnt/<volume name>"`
	ReadOnly              *bool  `json:"read_only,omitempty" jsonschema:"mount read-only"`
	Noexec                *bool  `json:"noexec,omitempty" jsonschema:"mount with noexec"`
	DeleteOnSandboxDelete *bool  `json:"delete_on_sandbox_delete,omitempty" jsonschema:"delete the volume (and stop billing it) when the sandbox is deleted; default keeps it with its data"`
}

type DetachMicrovmVolumeInput struct {
	SandboxID string `json:"sandbox_id" jsonschema:"UUID of the sandbox; it must be stopped"`
	VolumeID  string `json:"volume_id" jsonschema:"UUID of the volume to detach"`
	Confirmation
}

// MicrovmVolumeResult is one sandbox volume: id, name, for, size_gib, status (available, attached, attaching,
// detaching, pending, deleting, error), location_id, sandbox_id and sandbox_name (null while free), mount_path, slot,
// read_only, noexec, delete_on_sandbox_delete, node_pinned, failure_reason, created_at.
type MicrovmVolumeResult struct {
	Volume map[string]any `json:"volume"`
}

type MicrovmVolumeListResult struct {
	Volumes []map[string]any `json:"volumes"`
	Count   int              `json:"count"`
}

func listMicrovmVolumes(ctx context.Context, cl *client.Client, in ListMicrovmVolumesInput) (MicrovmVolumeListResult, error) {
	items, err := cl.ListMicrovmVolumes(ctx, in.SandboxID)
	if err != nil {
		return MicrovmVolumeListResult{}, err
	}
	return MicrovmVolumeListResult{Volumes: items, Count: len(items)}, nil
}

func attachMicrovmVolume(ctx context.Context, cl *client.Client, in AttachMicrovmVolumeInput) (MicrovmVolumeResult, error) {
	body := map[string]any{"volume_id": in.VolumeID}
	if in.MountPath != "" {
		body["mount_path"] = in.MountPath
	}
	if in.ReadOnly != nil {
		body["read_only"] = *in.ReadOnly
	}
	if in.Noexec != nil {
		body["noexec"] = *in.Noexec
	}
	if in.DeleteOnSandboxDelete != nil {
		body["delete_on_sandbox_delete"] = *in.DeleteOnSandboxDelete
	}
	// An empty key makes the client send a fresh Idempotency-Key; the client retries only the transport.
	vol, err := cl.AttachMicrovmVolume(ctx, in.SandboxID, body, "")
	if err != nil {
		return MicrovmVolumeResult{}, err
	}
	return MicrovmVolumeResult{Volume: vol}, nil
}

func detachMicrovmVolume(ctx context.Context, cl *client.Client, in DetachMicrovmVolumeInput) (MicrovmVolumeResult, error) {
	vol, err := cl.DetachMicrovmVolume(ctx, in.SandboxID, in.VolumeID)
	if err != nil {
		return MicrovmVolumeResult{}, err
	}
	return MicrovmVolumeResult{Volume: vol}, nil
}

func registerMicrovmVolumeTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.volume_list", Description: "List the persistent data volumes a sandbox holds, in slot order (the guest sees them as its data disks in that order): mount path, size, read-only, whether the volume is deleted with the sandbox. Never changes anything."}, listMicrovmVolumes)
	Register(s, deps, Spec{Name: "user.microvm.volume_attach", Description: "Attach a persistent data volume (user.volume.create with for sandbox) to a STOPPED sandbox; the next start mounts it at mount_path. The volume must be free and on the same node as the sandbox (a volume stays on the node it was created on), and a sandbox holds at most 4. A running or paused sandbox keeps its volumes and refuses with sandbox_state_invalid: stop it first. Other typed refusals: volume_in_use, volume_node_mismatch, volume_slots_exhausted, mount_path_conflict, mount_path_invalid, volume_not_found, sandbox_not_found, node_volumes_unsupported, volume_node_unavailable. Snapshots of the sandbox do not contain its volumes."}, attachMicrovmVolume)
	Register(s, deps, Spec{Name: "user.microvm.volume_detach", Description: "Detach a volume from a STOPPED sandbox. The volume keeps its data and its node and can be attached to another sandbox on that node. Repeating the call after it succeeded succeeds again. A running or paused sandbox refuses with sandbox_state_invalid. Requires \"confirm\": true.", Destructive: true}, detachMicrovmVolume)
}
