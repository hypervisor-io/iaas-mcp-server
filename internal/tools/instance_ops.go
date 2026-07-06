package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Instance sub-resource tools (backups, disks, IPs, network, ISO, forge,
// deploy), closing coverage gaps. Deploy/self-deploy are async (poll the deploy
// task to completed, then return the instance). Backup/disk/iso/forge writes
// enqueue server-side work; most return {success,message} synchronously.

func init() {
	toolRegistrars = append(toolRegistrars, registerInstanceOpsTools)
}

// ── inputs ───────────────────────────────────────────────────────────────────

type InstanceIDInput struct {
	ID string `json:"id" jsonschema:"UUID of the instance"`
}

type InstanceDeployInput struct {
	ID       string   `json:"id" jsonschema:"UUID of the instance"`
	ImageID  string   `json:"image_id" jsonschema:"UUID of the OS image to deploy"`
	Hostname string   `json:"hostname,omitempty" jsonschema:"optional hostname"`
	Timezone string   `json:"timezone,omitempty" jsonschema:"optional IANA timezone"`
	SSHKeys  []string `json:"ssh_keys,omitempty" jsonschema:"optional SSH key UUIDs"`
}

type InstanceSelfDeployInput struct {
	ID       string `json:"id" jsonschema:"UUID of the self-provisioned instance"`
	ImageID  string `json:"image_id" jsonschema:"UUID of the OS image"`
	Hostname string `json:"hostname" jsonschema:"hostname for the rebuilt instance"`
}

type CreateInstanceBackupInput struct {
	ID           string `json:"id" jsonschema:"UUID of the instance"`
	Name         string `json:"name,omitempty" jsonschema:"optional backup name"`
	Description  string `json:"description,omitempty" jsonschema:"optional description"`
	Encrypted    *bool  `json:"encrypted,omitempty" jsonschema:"encrypt the backup"`
	BackupType   string `json:"backup_type,omitempty" jsonschema:"full or incremental"`
	BackupDevice string `json:"backup_device,omitempty" jsonschema:"optional backup device"`
}

type DeleteInstanceBackupInput struct {
	ID       string `json:"id" jsonschema:"UUID of the instance"`
	BackupID string `json:"backup_id" jsonschema:"UUID of the backup to delete"`
	Confirmation
}

type RestoreInstanceBackupInput struct {
	ID           string `json:"id" jsonschema:"UUID of the instance"`
	BackupID     string `json:"backup_id" jsonschema:"UUID of the backup to restore"`
	TargetDiskID string `json:"target_disk_id,omitempty" jsonschema:"optional disk UUID to restore into"`
}

type BulkDestroyInstanceBackupsInput struct {
	ID      string   `json:"id" jsonschema:"UUID of the instance"`
	Backups []string `json:"backups" jsonschema:"UUIDs of the backups to destroy"`
	Confirmation
}

type InstanceDiskActionInput struct {
	ID        string `json:"id" jsonschema:"UUID of the instance"`
	StorageID string `json:"storage_id" jsonschema:"UUID of the instance disk"`
	Action    string `json:"action" jsonschema:"attach, detach, or delete"`
}

type SetInstanceIPRdnsInput struct {
	ID   string `json:"id" jsonschema:"UUID of the instance"`
	IPID string `json:"ip_id" jsonschema:"UUID of the instance IP"`
	Rdns string `json:"rdns" jsonschema:"reverse DNS hostname"`
}

type InstanceISOActionInput struct {
	ID     string `json:"id" jsonschema:"UUID of the instance"`
	Device string `json:"device" jsonschema:"primary or secondary"`
	Action string `json:"action" jsonschema:"insert or eject"`
	ISOID  string `json:"iso_id,omitempty" jsonschema:"UUID of an ISO to insert (from the ISO library)"`
	ISOURL string `json:"iso_url,omitempty" jsonschema:"URL of a custom ISO to insert"`
}

type ForgeEnableInput struct {
	ID      string   `json:"id" jsonschema:"UUID of the instance"`
	DiskIDs []string `json:"disk_ids" jsonschema:"UUIDs of the disks to place under Forge"`
}

// ── handlers ─────────────────────────────────────────────────────────────────

func deployInstanceTool(ctx context.Context, cl *client.Client, in InstanceDeployInput) (InstanceResult, error) {
	body := map[string]any{"image_id": in.ImageID}
	if in.Hostname != "" {
		body["hostname"] = in.Hostname
	}
	if in.Timezone != "" {
		body["timezone"] = in.Timezone
	}
	if len(in.SSHKeys) > 0 {
		body["ssh_keys"] = in.SSHKeys
	}
	return deployAndWait(ctx, cl, in.ID, body, cl.DeployInstance)
}

func selfDeployInstance(ctx context.Context, cl *client.Client, in InstanceSelfDeployInput) (InstanceResult, error) {
	body := map[string]any{"image_id": in.ImageID, "hostname": in.Hostname}
	return deployAndWait(ctx, cl, in.ID, body, cl.SelfDeployInstance)
}

// deployAndWait runs a deploy that returns a top-level task_id, waits for the
// deploy task to complete, then hydrates the instance.
func deployAndWait(ctx context.Context, cl *client.Client, id string, body map[string]any,
	deploy func(context.Context, string, map[string]any) (map[string]any, error)) (InstanceResult, error) {
	resp, err := deploy(ctx, id, body)
	if err != nil {
		return InstanceResult{}, err
	}
	taskID, _ := resp["task_id"].(string)
	if taskID == "" {
		return InstanceResult{}, fmt.Errorf("deploy response did not include a task_id")
	}
	if err := waitForInstanceDeploy(ctx, cl, id, taskID, defaultCreateTimeout); err != nil {
		return InstanceResult{}, fmt.Errorf("instance %s deploy task %s did not complete: %w", id, taskID, err)
	}
	obj, err := cl.GetInstance(ctx, id)
	if err != nil {
		return InstanceResult{}, err
	}
	return InstanceResult{Instance: obj}, nil
}

func listInstanceBackups(ctx context.Context, cl *client.Client, in InstanceIDInput) (ItemsResult, error) {
	return itemsResult(cl.ListInstanceBackups(ctx, in.ID))
}

func createInstanceBackup(ctx context.Context, cl *client.Client, in CreateInstanceBackupInput) (OKResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Encrypted != nil {
		body["encrypted"] = *in.Encrypted
	}
	if in.BackupType != "" {
		body["backup_type"] = in.BackupType
	}
	if in.BackupDevice != "" {
		body["backup_device"] = in.BackupDevice
	}
	if err := cl.CreateInstanceBackup(ctx, in.ID, body); err != nil {
		return OKResult{}, err
	}
	return okResult("backup queued"), nil
}

func deleteInstanceBackup(ctx context.Context, cl *client.Client, in DeleteInstanceBackupInput) (DeleteResult, error) {
	if err := cl.DeleteInstanceBackup(ctx, in.ID, in.BackupID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.BackupID, Deleted: true}, nil
}

func restoreInstanceBackup(ctx context.Context, cl *client.Client, in RestoreInstanceBackupInput) (OKResult, error) {
	body := map[string]any{}
	if in.TargetDiskID != "" {
		body["target_disk_id"] = in.TargetDiskID
	}
	if err := cl.RestoreInstanceBackup(ctx, in.ID, in.BackupID, body); err != nil {
		return OKResult{}, err
	}
	return okResult("restore queued"), nil
}

func destroyInstanceBackups(ctx context.Context, cl *client.Client, in BulkDestroyInstanceBackupsInput) (OKResult, error) {
	if err := cl.BulkDestroyInstanceBackups(ctx, in.ID, map[string]any{"backups": in.Backups}); err != nil {
		return OKResult{}, err
	}
	return okResult("backups destroy queued"), nil
}

func listInstanceDisks(ctx context.Context, cl *client.Client, in InstanceIDInput) (ItemsResult, error) {
	return itemsResult(cl.ListInstanceDisks(ctx, in.ID))
}

func instanceDiskAction(ctx context.Context, cl *client.Client, in InstanceDiskActionInput) (ObjectResult, error) {
	return objectResult(cl.InstanceDiskAction(ctx, in.ID, in.StorageID, in.Action, nil))
}

func listInstanceIPs(ctx context.Context, cl *client.Client, in InstanceIDInput) (ItemsResult, error) {
	return itemsResult(cl.ListInstanceIPs(ctx, in.ID))
}

func setInstanceIPRdns(ctx context.Context, cl *client.Client, in SetInstanceIPRdnsInput) (OKResult, error) {
	if err := cl.SetInstanceIPRdns(ctx, in.ID, in.IPID, map[string]any{"rdns": in.Rdns}); err != nil {
		return OKResult{}, err
	}
	return okResult("rdns set"), nil
}

func enableInstancePublicNetwork(ctx context.Context, cl *client.Client, in InstanceIDInput) (ObjectResult, error) {
	return objectResult(cl.EnableInstancePublicInterface(ctx, in.ID))
}

func instanceISOAction(ctx context.Context, cl *client.Client, in InstanceISOActionInput) (ObjectResult, error) {
	body := map[string]any{}
	if in.ISOID != "" {
		body["iso_id"] = in.ISOID
	}
	if in.ISOURL != "" {
		body["iso_url"] = in.ISOURL
		body["custom_iso"] = 1
	}
	return objectResult(cl.InstanceISOAction(ctx, in.ID, in.Device, in.Action, body))
}

func forgeEnable(ctx context.Context, cl *client.Client, in ForgeEnableInput) (ObjectResult, error) {
	return objectResult(cl.ForgeEnable(ctx, in.ID, map[string]any{"disk_ids": in.DiskIDs}))
}

func forgeCommit(ctx context.Context, cl *client.Client, in InstanceIDInput) (OKResult, error) {
	if err := cl.ForgeCommit(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("forge commit queued"), nil
}

func forgeDiscard(ctx context.Context, cl *client.Client, in InstanceIDInput) (OKResult, error) {
	if err := cl.ForgeDiscard(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("forge discard queued"), nil
}

func instanceDeployImages(ctx context.Context, cl *client.Client, in InstanceIDInput) (ObjectResult, error) {
	return objectResult(cl.GetInstanceDeployImages(ctx, in.ID))
}

func registerInstanceOpsTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.instance.deploy", Description: "Deploy (or redeploy) an OS image onto an instance and wait until deployed."}, deployInstanceTool)
	Register(s, deps, Spec{Name: "user.instance.self_deploy", Description: "Rebuild a self-provisioned instance from an image and wait until deployed."}, selfDeployInstance)
	Register(s, deps, Spec{Name: "user.instance.deploy_images", Description: "List the OS images available to deploy on an instance (grouped by distro)."}, instanceDeployImages)

	Register(s, deps, Spec{Name: "user.instance.list_backups", Description: "List an instance's backups."}, listInstanceBackups)
	Register(s, deps, Spec{Name: "user.instance.create_backup", Description: "Create a backup of an instance."}, createInstanceBackup)
	Register(s, deps, Spec{Name: "user.instance.restore_backup", Description: "Restore an instance from one of its backups."}, restoreInstanceBackup)
	Register(s, deps, Spec{
		Name:        "user.instance.delete_backup",
		Description: "Delete an instance backup. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteInstanceBackup)
	Register(s, deps, Spec{
		Name:        "user.instance.destroy_backups",
		Description: "Destroy multiple instance backups at once. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, destroyInstanceBackups)

	Register(s, deps, Spec{Name: "user.instance.list_disks", Description: "List an instance's disks."}, listInstanceDisks)
	Register(s, deps, Spec{Name: "user.instance.disk_action", Description: "Run attach, detach, or delete on an instance disk."}, instanceDiskAction)

	Register(s, deps, Spec{Name: "user.instance.list_ips", Description: "List an instance's IP addresses."}, listInstanceIPs)
	Register(s, deps, Spec{Name: "user.instance.set_ip_rdns", Description: "Set the reverse DNS for an instance IP."}, setInstanceIPRdns)
	Register(s, deps, Spec{Name: "user.instance.enable_public_network", Description: "Enable the public network interface on an instance."}, enableInstancePublicNetwork)

	Register(s, deps, Spec{Name: "user.instance.iso_action", Description: "Insert or eject an ISO on an instance's primary/secondary device."}, instanceISOAction)

	Register(s, deps, Spec{Name: "user.instance.forge_enable", Description: "Enable Forge layered snapshots on an instance's disks."}, forgeEnable)
	Register(s, deps, Spec{Name: "user.instance.forge_commit", Description: "Commit the current Forge layer on an instance."}, forgeCommit)
	Register(s, deps, Spec{Name: "user.instance.forge_discard", Description: "Discard the current Forge layer on an instance."}, forgeDiscard)
}
