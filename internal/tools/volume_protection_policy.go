package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { toolRegistrars = append(toolRegistrars, registerVolumeProtectionPolicyTools) }

type CreateVolumeProtectionPolicyInput struct {
	Name            string  `json:"name" jsonschema:"policy name, max255"`
	Frequency       string  `json:"frequency" jsonschema:"daily or weekly"`
	ScheduleTime    string  `json:"schedule_time" jsonschema:"HH:MM wall-clock time in the effective timezone"`
	ScheduleDay     *int    `json:"schedule_day,omitempty" jsonschema:"weekly day 0 Sunday through 6 Saturday"`
	Timezone        *string `json:"timezone,omitempty" jsonschema:"named IANA zone; omit on create for account owner timezone"`
	BackupEnabled   *bool   `json:"backup_enabled,omitempty" jsonschema:"default true; backups or snapshots must be enabled"`
	SnapshotEnabled *bool   `json:"snapshot_enabled,omitempty" jsonschema:"default false"`
	RetentionMode   *string `json:"retention_mode,omitempty" jsonschema:"count default or gfs; gfs requires a positive keep rule"`
	RetentionCount  *int    `json:"retention_count,omitempty" jsonschema:"1..365 required for count retention"`
	KeepLast        *int    `json:"keep_last,omitempty" jsonschema:"GFS newest artifacts 0..365"`
	KeepWithinDays  *int    `json:"keep_within_days,omitempty" jsonschema:"GFS all artifacts in the last 0..3650 days"`
	KeepDaily       *int    `json:"keep_daily,omitempty" jsonschema:"GFS daily buckets 0..365"`
	KeepWeekly      *int    `json:"keep_weekly,omitempty" jsonschema:"GFS ISO week buckets 0..520"`
	KeepMonthly     *int    `json:"keep_monthly,omitempty" jsonschema:"GFS month buckets 0..120"`
	KeepYearly      *int    `json:"keep_yearly,omitempty" jsonschema:"GFS year buckets 0..30"`
}
type UpdateVolumeProtectionPolicyInput struct {
	ID              string  `json:"id" jsonschema:"policy UUID"`
	Name            *string `json:"name,omitempty" jsonschema:"policy name, max255; omit to preserve"`
	Frequency       *string `json:"frequency,omitempty" jsonschema:"daily or weekly; omit to preserve"`
	ScheduleTime    *string `json:"schedule_time,omitempty" jsonschema:"HH:MM wall-clock time in the effective timezone; omit to preserve"`
	ScheduleDay     *int    `json:"schedule_day,omitempty" jsonschema:"weekly day 0 Sunday through 6 Saturday; omit to preserve"`
	Timezone        *string `json:"timezone,omitempty" jsonschema:"named IANA zone; omit on create for account owner timezone; omit to preserve"`
	BackupEnabled   *bool   `json:"backup_enabled,omitempty" jsonschema:"default true; backups or snapshots must be enabled; omit to preserve"`
	SnapshotEnabled *bool   `json:"snapshot_enabled,omitempty" jsonschema:"default false; omit to preserve"`
	RetentionMode   *string `json:"retention_mode,omitempty" jsonschema:"count default or gfs; gfs requires a positive keep rule; omit to preserve"`
	RetentionCount  *int    `json:"retention_count,omitempty" jsonschema:"1..365 required for count retention; omit to preserve"`
	KeepLast        *int    `json:"keep_last,omitempty" jsonschema:"GFS newest artifacts 0..365; omit to preserve"`
	KeepWithinDays  *int    `json:"keep_within_days,omitempty" jsonschema:"GFS all artifacts in the last 0..3650 days; omit to preserve"`
	KeepDaily       *int    `json:"keep_daily,omitempty" jsonschema:"GFS daily buckets 0..365; omit to preserve"`
	KeepWeekly      *int    `json:"keep_weekly,omitempty" jsonschema:"GFS ISO week buckets 0..520; omit to preserve"`
	KeepMonthly     *int    `json:"keep_monthly,omitempty" jsonschema:"GFS month buckets 0..120; omit to preserve"`
	KeepYearly      *int    `json:"keep_yearly,omitempty" jsonschema:"GFS year buckets 0..30; omit to preserve"`
	present         map[string]bool
}

// Pointer fields advertise null in the MCP schema. Track presence separately
// so an explicit null survives decoding while an omitted field remains absent.
func (in *UpdateVolumeProtectionPolicyInput) UnmarshalJSON(data []byte) error {
	type plain UpdateVolumeProtectionPolicyInput
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*in = UpdateVolumeProtectionPolicyInput(decoded)
	in.present = make(map[string]bool, len(fields))
	for key := range fields {
		in.present[key] = true
	}
	return nil
}

type VolumeProtectionPolicyIDInput struct {
	ID string `json:"id" jsonschema:"policy UUID"`
}
type DeleteVolumeProtectionPolicyInput struct {
	ID string `json:"id" jsonschema:"policy UUID"`
	Confirmation
}
type VolumeProtectionPolicyMemberInput struct {
	PolicyID string `json:"policy_id" jsonschema:"policy UUID"`
	VolumeID string `json:"volume_id" jsonschema:"volume UUID"`
}
type VolumeProtectionPolicyDetails struct {
	Policy            map[string]any   `json:"policy"`
	AttachableVolumes []map[string]any `json:"attachable_volumes"`
}

func createVolumeProtectionPolicy(ctx context.Context, cl *client.Client, in CreateVolumeProtectionPolicyInput) (BackupPolicyResult, error) {
	body := map[string]any{"name": in.Name, "frequency": in.Frequency, "schedule_time": in.ScheduleTime}
	if in.ScheduleDay != nil {
		body["schedule_day"] = *in.ScheduleDay
	}
	if in.Timezone != nil {
		body["timezone"] = *in.Timezone
	}
	if in.BackupEnabled != nil {
		body["backup_enabled"] = *in.BackupEnabled
	}
	if in.SnapshotEnabled != nil {
		body["snapshot_enabled"] = *in.SnapshotEnabled
	}
	if in.RetentionMode != nil {
		body["retention_mode"] = *in.RetentionMode
	}
	if in.RetentionCount != nil {
		body["retention_count"] = *in.RetentionCount
	}
	if in.KeepLast != nil {
		body["keep_last"] = *in.KeepLast
	}
	if in.KeepWithinDays != nil {
		body["keep_within_days"] = *in.KeepWithinDays
	}
	if in.KeepDaily != nil {
		body["keep_daily"] = *in.KeepDaily
	}
	if in.KeepWeekly != nil {
		body["keep_weekly"] = *in.KeepWeekly
	}
	if in.KeepMonthly != nil {
		body["keep_monthly"] = *in.KeepMonthly
	}
	if in.KeepYearly != nil {
		body["keep_yearly"] = *in.KeepYearly
	}
	obj, err := cl.CreateVolumeProtectionPolicy(ctx, body)
	return BackupPolicyResult{Policy: obj}, err
}
func volumeProtectionPatchBody(in UpdateVolumeProtectionPolicyInput) (map[string]any, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	} else if in.present["name"] {
		body["name"] = nil
	}
	if in.Frequency != nil {
		body["frequency"] = *in.Frequency
	} else if in.present["frequency"] {
		body["frequency"] = nil
	}
	if in.ScheduleTime != nil {
		body["schedule_time"] = *in.ScheduleTime
	} else if in.present["schedule_time"] {
		body["schedule_time"] = nil
	}
	if in.ScheduleDay != nil {
		body["schedule_day"] = *in.ScheduleDay
	} else if in.present["schedule_day"] {
		body["schedule_day"] = nil
	}
	if in.Timezone != nil {
		body["timezone"] = *in.Timezone
	} else if in.present["timezone"] {
		body["timezone"] = nil
	}
	if in.BackupEnabled != nil {
		body["backup_enabled"] = *in.BackupEnabled
	} else if in.present["backup_enabled"] {
		body["backup_enabled"] = nil
	}
	if in.SnapshotEnabled != nil {
		body["snapshot_enabled"] = *in.SnapshotEnabled
	} else if in.present["snapshot_enabled"] {
		body["snapshot_enabled"] = nil
	}
	if in.RetentionMode != nil {
		body["retention_mode"] = *in.RetentionMode
	} else if in.present["retention_mode"] {
		body["retention_mode"] = nil
	}
	if in.RetentionCount != nil {
		body["retention_count"] = *in.RetentionCount
	} else if in.present["retention_count"] {
		body["retention_count"] = nil
	}
	if in.KeepLast != nil {
		body["keep_last"] = *in.KeepLast
	} else if in.present["keep_last"] {
		body["keep_last"] = nil
	}
	if in.KeepWithinDays != nil {
		body["keep_within_days"] = *in.KeepWithinDays
	} else if in.present["keep_within_days"] {
		body["keep_within_days"] = nil
	}
	if in.KeepDaily != nil {
		body["keep_daily"] = *in.KeepDaily
	} else if in.present["keep_daily"] {
		body["keep_daily"] = nil
	}
	if in.KeepWeekly != nil {
		body["keep_weekly"] = *in.KeepWeekly
	} else if in.present["keep_weekly"] {
		body["keep_weekly"] = nil
	}
	if in.KeepMonthly != nil {
		body["keep_monthly"] = *in.KeepMonthly
	} else if in.present["keep_monthly"] {
		body["keep_monthly"] = nil
	}
	if in.KeepYearly != nil {
		body["keep_yearly"] = *in.KeepYearly
	} else if in.present["keep_yearly"] {
		body["keep_yearly"] = nil
	}
	return body, nil
}
func updateVolumeProtectionPolicy(ctx context.Context, cl *client.Client, in UpdateVolumeProtectionPolicyInput) (BackupPolicyResult, error) {
	body, err := volumeProtectionPatchBody(in)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	obj, err := cl.UpdateVolumeProtectionPolicy(ctx, in.ID, body)
	return BackupPolicyResult{Policy: obj}, err
}
func getVolumeProtectionPolicy(ctx context.Context, cl *client.Client, in VolumeProtectionPolicyIDInput) (VolumeProtectionPolicyDetails, error) {
	obj, err := cl.GetVolumeProtectionPolicyDetails(ctx, in.ID)
	if err != nil {
		return VolumeProtectionPolicyDetails{}, err
	}
	policy, ok := obj["policy"].(map[string]any)
	if !ok {
		return VolumeProtectionPolicyDetails{}, fmt.Errorf("policy detail response omitted policy")
	}
	volumes := []map[string]any{}
	if list, ok := obj["attachable_volumes"].([]any); ok {
		for _, item := range list {
			if volume, ok := item.(map[string]any); ok {
				volumes = append(volumes, volume)
			}
		}
	}
	return VolumeProtectionPolicyDetails{Policy: policy, AttachableVolumes: volumes}, nil
}
func listVolumeProtectionPolicies(ctx context.Context, cl *client.Client, _ ListInstanceBackupPoliciesInput) (BackupPolicyListResult, error) {
	items, err := cl.ListVolumeProtectionPolicies(ctx)
	return BackupPolicyListResult{Policies: items, Count: len(items)}, err
}
func deleteVolumeProtectionPolicy(ctx context.Context, cl *client.Client, in DeleteVolumeProtectionPolicyInput) (DeleteResult, error) {
	if err := cl.DeleteVolumeProtectionPolicy(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}
func attachVolumeProtectionPolicy(ctx context.Context, cl *client.Client, in VolumeProtectionPolicyMemberInput) (OKResult, error) {
	if err := cl.AttachVolumeToProtectionPolicy(ctx, in.PolicyID, in.VolumeID); err != nil {
		return OKResult{}, err
	}
	return okResult("volume attached to protection policy"), nil
}
func detachVolumeProtectionPolicy(ctx context.Context, cl *client.Client, in VolumeProtectionPolicyMemberInput) (OKResult, error) {
	if err := cl.DetachVolumeFromProtectionPolicy(ctx, in.PolicyID, in.VolumeID); err != nil {
		return OKResult{}, err
	}
	return okResult("volume detached from protection policy; artifacts kept"), nil
}
func resetVolumeProtectionPolicyFailures(ctx context.Context, cl *client.Client, in VolumeProtectionPolicyIDInput) (BackupPolicyResult, error) {
	obj, err := cl.ResetVolumeProtectionPolicyFailures(ctx, in.ID)
	return BackupPolicyResult{Policy: obj}, err
}
func registerVolumeProtectionPolicyTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.volume_policy.list", Description: "List all volume protection policies across all pages."}, listVolumeProtectionPolicies)
	Register(s, deps, Spec{Name: "user.volume_policy.get", Description: "Get policy configuration, protected volumes, attachable volumes and the 50 most recent runs."}, getVolumeProtectionPolicy)
	Register(s, deps, Spec{Name: "user.volume_policy.create", Description: "Create a standalone volume protection policy. Schedule is wall-clock in the named IANA zone or account owner timezone, following DST. At least backups or snapshots must be enabled; GFS needs a positive keep rule. Sent once."}, createVolumeProtectionPolicy)
	Register(s, deps, Spec{Name: "user.volume_policy.update", Description: "Partially update a volume protection policy. Omitted fields keep their values. Explicit null clears timezone and keep rules; keep rules may also be zero to disable them. Null retention mode/count preserves the stored value. Switching to daily clears schedule_day. Operational status and ownership cannot be written."}, updateVolumeProtectionPolicy)
	Register(s, deps, Spec{Name: "user.volume_policy.delete", Description: "Delete a volume protection policy, detach all its volumes and keep existing artifacts. Requires confirm:true.", Destructive: true}, deleteVolumeProtectionPolicy)
	Register(s, deps, Spec{Name: "user.volume_policy.attach", Description: "Attach one volume to a protection policy. A volume can have only one policy; same-policy attach is idempotent. Proxmox per-volume policies are unsupported."}, attachVolumeProtectionPolicy)
	Register(s, deps, Spec{Name: "user.volume_policy.detach", Description: "Detach one volume from a protection policy, preserving existing artifacts."}, detachVolumeProtectionPolicy)
	Register(s, deps, Spec{Name: "user.volume_policy.reset_failures", Description: "Reset failure counters and reactivate a paused volume protection policy."}, resetVolumeProtectionPolicyFailures)
}
