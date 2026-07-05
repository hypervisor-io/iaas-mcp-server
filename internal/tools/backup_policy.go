package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Backup policy tools, mirroring the iaas_instance_backup_policy and
// iaas_db_backup_policy resources. All synchronous. Members (instances /
// databases) are attached and detached one at a time via dedicated endpoints.

func init() {
	toolRegistrars = append(toolRegistrars, registerBackupPolicyTools)
}

// ── instance backup policy ──────────────────────────────────────────────────

type CreateInstanceBackupPolicyInput struct {
	Name                string `json:"name" jsonschema:"policy name"`
	FullBackupFrequency string `json:"full_backup_frequency" jsonschema:"daily or weekly"`
	FullBackupTime      string `json:"full_backup_time" jsonschema:"time of day H:i (UTC)"`
	FullBackupDay       *int   `json:"full_backup_day,omitempty" jsonschema:"day of week 0-6 (required for weekly)"`
	MaxIncrementalChain int    `json:"max_incremental_chain" jsonschema:"max incremental chain length (0-30)"`
	RetentionCount      int    `json:"retention_count" jsonschema:"number of full backups to retain (1-365)"`
	BackupDevice        string `json:"backup_device" jsonschema:"primary or all"`
}

type InstanceBackupPolicyIDInput struct {
	ID string `json:"id" jsonschema:"UUID of the instance backup policy"`
}

type ListInstanceBackupPoliciesInput struct{}

type DeleteInstanceBackupPolicyInput struct {
	ID string `json:"id" jsonschema:"UUID of the policy to delete"`
	Confirmation
}

type InstanceBackupPolicyMemberInput struct {
	PolicyID   string `json:"policy_id" jsonschema:"UUID of the backup policy"`
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
}

type BackupPolicyResult struct {
	Policy map[string]any `json:"policy"`
}

type BackupPolicyListResult struct {
	Policies []map[string]any `json:"policies"`
	Count    int              `json:"count"`
}

// ── db backup policy ────────────────────────────────────────────────────────

type CreateDBBackupPolicyInput struct {
	Name                     string `json:"name" jsonschema:"policy name"`
	S3Endpoint               string `json:"s3_endpoint" jsonschema:"S3 endpoint URL"`
	S3Bucket                 string `json:"s3_bucket" jsonschema:"S3 bucket name"`
	S3Region                 string `json:"s3_region" jsonschema:"S3 region"`
	S3AccessKey              string `json:"s3_access_key" jsonschema:"S3 access key"`
	S3SecretKey              string `json:"s3_secret_key" jsonschema:"S3 secret key"`
	S3PathPrefix             string `json:"s3_path_prefix,omitempty" jsonschema:"optional S3 path prefix"`
	FullBackupFrequency      string `json:"full_backup_frequency" jsonschema:"daily or weekly"`
	FullBackupTime           string `json:"full_backup_time" jsonschema:"time of day H:i (UTC)"`
	FullBackupDay            *int   `json:"full_backup_day,omitempty" jsonschema:"day of week (weekly)"`
	IncrementalFrequency     string `json:"incremental_frequency" jsonschema:"none, 1h, 2h, 4h, 6h, or 12h"`
	PitrEnabled              *bool  `json:"pitr_enabled,omitempty" jsonschema:"point-in-time recovery enabled"`
	RetentionFullCount       int    `json:"retention_full_count" jsonschema:"full backups to retain (1-365)"`
	RetentionIncrementalDays int    `json:"retention_incremental_days" jsonschema:"incremental retention days (1-365)"`
	RetentionPitrHours       int    `json:"retention_pitr_hours" jsonschema:"PITR retention hours (1-720)"`
	EncryptionEnabled        *bool  `json:"encryption_enabled,omitempty" jsonschema:"encryption enabled"`
}

type DBBackupPolicyIDInput struct {
	ID string `json:"id" jsonschema:"UUID of the db backup policy"`
}

type ListDBBackupPoliciesInput struct{}

type DeleteDBBackupPolicyInput struct {
	ID string `json:"id" jsonschema:"UUID of the policy to delete"`
	Confirmation
}

type DBBackupPolicyMemberInput struct {
	PolicyID   string `json:"policy_id" jsonschema:"UUID of the backup policy"`
	DatabaseID string `json:"database_id" jsonschema:"UUID of the managed database"`
}

// ── instance policy handlers ────────────────────────────────────────────────

func createInstanceBackupPolicy(ctx context.Context, cl *client.Client, in CreateInstanceBackupPolicyInput) (BackupPolicyResult, error) {
	body := map[string]any{
		"name":                  in.Name,
		"full_backup_frequency": in.FullBackupFrequency,
		"full_backup_time":      in.FullBackupTime,
		"max_incremental_chain": in.MaxIncrementalChain,
		"retention_count":       in.RetentionCount,
		"backup_device":         in.BackupDevice,
	}
	if in.FullBackupDay != nil {
		body["full_backup_day"] = *in.FullBackupDay
	}
	obj, err := cl.CreateInstanceBackupPolicy(ctx, body)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	return BackupPolicyResult{Policy: obj}, nil
}

func getInstanceBackupPolicy(ctx context.Context, cl *client.Client, in InstanceBackupPolicyIDInput) (BackupPolicyResult, error) {
	obj, err := cl.GetInstanceBackupPolicy(ctx, in.ID)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	return BackupPolicyResult{Policy: obj}, nil
}

func listInstanceBackupPolicies(ctx context.Context, cl *client.Client, _ ListInstanceBackupPoliciesInput) (BackupPolicyListResult, error) {
	items, err := cl.ListInstanceBackupPolicies(ctx)
	if err != nil {
		return BackupPolicyListResult{}, err
	}
	return BackupPolicyListResult{Policies: items, Count: len(items)}, nil
}

func deleteInstanceBackupPolicy(ctx context.Context, cl *client.Client, in DeleteInstanceBackupPolicyInput) (DeleteResult, error) {
	if err := cl.DeleteInstanceBackupPolicy(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func attachInstanceBackupPolicy(ctx context.Context, cl *client.Client, in InstanceBackupPolicyMemberInput) (OKResult, error) {
	if err := cl.AttachInstanceToBackupPolicy(ctx, in.PolicyID, in.InstanceID); err != nil {
		return OKResult{}, err
	}
	return okResult("instance attached to backup policy"), nil
}

func detachInstanceBackupPolicy(ctx context.Context, cl *client.Client, in InstanceBackupPolicyMemberInput) (OKResult, error) {
	if err := cl.DetachInstanceFromBackupPolicy(ctx, in.PolicyID, in.InstanceID); err != nil {
		return OKResult{}, err
	}
	return okResult("instance detached from backup policy"), nil
}

// ── db policy handlers ──────────────────────────────────────────────────────

func createDBBackupPolicy(ctx context.Context, cl *client.Client, in CreateDBBackupPolicyInput) (BackupPolicyResult, error) {
	body := map[string]any{
		"name":                       in.Name,
		"s3_endpoint":                in.S3Endpoint,
		"s3_bucket":                  in.S3Bucket,
		"s3_region":                  in.S3Region,
		"s3_access_key":              in.S3AccessKey,
		"s3_secret_key":              in.S3SecretKey,
		"full_backup_frequency":      in.FullBackupFrequency,
		"full_backup_time":           in.FullBackupTime,
		"incremental_frequency":      in.IncrementalFrequency,
		"retention_full_count":       in.RetentionFullCount,
		"retention_incremental_days": in.RetentionIncrementalDays,
		"retention_pitr_hours":       in.RetentionPitrHours,
	}
	if in.S3PathPrefix != "" {
		body["s3_path_prefix"] = in.S3PathPrefix
	}
	if in.FullBackupDay != nil {
		body["full_backup_day"] = *in.FullBackupDay
	}
	if in.PitrEnabled != nil {
		body["pitr_enabled"] = *in.PitrEnabled
	}
	if in.EncryptionEnabled != nil {
		body["encryption_enabled"] = *in.EncryptionEnabled
	}
	obj, err := cl.CreateDBBackupPolicy(ctx, body)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	return BackupPolicyResult{Policy: obj}, nil
}

func getDBBackupPolicy(ctx context.Context, cl *client.Client, in DBBackupPolicyIDInput) (BackupPolicyResult, error) {
	obj, err := cl.GetDBBackupPolicy(ctx, in.ID)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	return BackupPolicyResult{Policy: obj}, nil
}

func listDBBackupPolicies(ctx context.Context, cl *client.Client, _ ListDBBackupPoliciesInput) (BackupPolicyListResult, error) {
	items, err := cl.ListDBBackupPolicies(ctx)
	if err != nil {
		return BackupPolicyListResult{}, err
	}
	return BackupPolicyListResult{Policies: items, Count: len(items)}, nil
}

func deleteDBBackupPolicy(ctx context.Context, cl *client.Client, in DeleteDBBackupPolicyInput) (DeleteResult, error) {
	if err := cl.DeleteDBBackupPolicy(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func attachDBBackupPolicy(ctx context.Context, cl *client.Client, in DBBackupPolicyMemberInput) (OKResult, error) {
	if err := cl.AttachDatabaseToBackupPolicy(ctx, in.PolicyID, in.DatabaseID); err != nil {
		return OKResult{}, err
	}
	return okResult("database attached to backup policy"), nil
}

func detachDBBackupPolicy(ctx context.Context, cl *client.Client, in DBBackupPolicyMemberInput) (OKResult, error) {
	if err := cl.DetachDatabaseFromBackupPolicy(ctx, in.PolicyID, in.DatabaseID); err != nil {
		return OKResult{}, err
	}
	return okResult("database detached from backup policy"), nil
}

func registerBackupPolicyTools(s *mcp.Server, deps Deps) {
	// Instance backup policies.
	Register(s, deps, Spec{Name: "user.instance_backup_policy.create", Description: "Create an instance backup policy."}, createInstanceBackupPolicy)
	Register(s, deps, Spec{Name: "user.instance_backup_policy.list", Description: "List all instance backup policies."}, listInstanceBackupPolicies)
	Register(s, deps, Spec{Name: "user.instance_backup_policy.get", Description: "Get an instance backup policy by UUID."}, getInstanceBackupPolicy)
	Register(s, deps, Spec{
		Name:        "user.instance_backup_policy.delete",
		Description: "Delete an instance backup policy. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteInstanceBackupPolicy)
	Register(s, deps, Spec{Name: "user.instance_backup_policy.attach_instance", Description: "Attach an instance to a backup policy."}, attachInstanceBackupPolicy)
	Register(s, deps, Spec{Name: "user.instance_backup_policy.detach_instance", Description: "Detach an instance from a backup policy."}, detachInstanceBackupPolicy)

	// DB backup policies.
	Register(s, deps, Spec{Name: "user.db_backup_policy.create", Description: "Create a database backup policy (S3 destination)."}, createDBBackupPolicy)
	Register(s, deps, Spec{Name: "user.db_backup_policy.list", Description: "List all database backup policies."}, listDBBackupPolicies)
	Register(s, deps, Spec{Name: "user.db_backup_policy.get", Description: "Get a database backup policy by UUID."}, getDBBackupPolicy)
	Register(s, deps, Spec{
		Name:        "user.db_backup_policy.delete",
		Description: "Delete a database backup policy. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDBBackupPolicy)
	Register(s, deps, Spec{Name: "user.db_backup_policy.attach_database", Description: "Attach a managed database to a backup policy."}, attachDBBackupPolicy)
	Register(s, deps, Spec{Name: "user.db_backup_policy.detach_database", Description: "Detach a managed database from a backup policy."}, detachDBBackupPolicy)
}
