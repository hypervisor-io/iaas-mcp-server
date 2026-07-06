package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Managed-database action tools and backup-policy update/reset tools, closing
// coverage gaps. All synchronous ({success,message}); backup returns the backup
// record.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityDBTools)
}

type DBRestoreInput struct {
	ID       string `json:"id" jsonschema:"UUID of the managed database"`
	BackupID string `json:"backup_id" jsonschema:"UUID of the backup to restore"`
}

type DBRestorePitrInput struct {
	ID         string `json:"id" jsonschema:"UUID of the managed database"`
	TargetTime string `json:"target_time" jsonschema:"point-in-time to restore to (a date/time)"`
}

type DBApplyParameterGroupInput struct {
	ID               string `json:"id" jsonschema:"UUID of the managed database"`
	ParameterGroupID string `json:"parameter_group_id" jsonschema:"UUID of the parameter group to apply"`
}

type UpdateInstanceBackupPolicyInput struct {
	ID                  string `json:"id" jsonschema:"UUID of the policy"`
	Name                string `json:"name" jsonschema:"policy name"`
	FullBackupFrequency string `json:"full_backup_frequency" jsonschema:"daily or weekly"`
	FullBackupTime      string `json:"full_backup_time" jsonschema:"time of day H:i (UTC)"`
	FullBackupDay       *int   `json:"full_backup_day,omitempty" jsonschema:"day of week 0-6 (required for weekly)"`
	MaxIncrementalChain int    `json:"max_incremental_chain" jsonschema:"max incremental chain length (0-30)"`
	RetentionCount      int    `json:"retention_count" jsonschema:"full backups to retain (1-365)"`
	BackupDevice        string `json:"backup_device" jsonschema:"primary or all"`
}

type UpdateDBBackupPolicyInput struct {
	ID                       string  `json:"id" jsonschema:"UUID of the db backup policy"`
	Name                     *string `json:"name,omitempty"`
	S3Endpoint               *string `json:"s3_endpoint,omitempty"`
	S3Bucket                 *string `json:"s3_bucket,omitempty"`
	S3Region                 *string `json:"s3_region,omitempty"`
	S3AccessKey              *string `json:"s3_access_key,omitempty"`
	S3SecretKey              *string `json:"s3_secret_key,omitempty"`
	S3PathPrefix             *string `json:"s3_path_prefix,omitempty"`
	FullBackupFrequency      *string `json:"full_backup_frequency,omitempty"`
	FullBackupTime           *string `json:"full_backup_time,omitempty"`
	FullBackupDay            *int    `json:"full_backup_day,omitempty"`
	IncrementalFrequency     *string `json:"incremental_frequency,omitempty"`
	PitrEnabled              *bool   `json:"pitr_enabled,omitempty"`
	RetentionFullCount       *int    `json:"retention_full_count,omitempty"`
	RetentionIncrementalDays *int    `json:"retention_incremental_days,omitempty"`
	RetentionPitrHours       *int    `json:"retention_pitr_hours,omitempty"`
	EncryptionEnabled        *bool   `json:"encryption_enabled,omitempty"`
}

type TestDBBackupPolicyConnectionInput struct {
	S3Endpoint   string `json:"s3_endpoint" jsonschema:"S3 endpoint URL"`
	S3Bucket     string `json:"s3_bucket" jsonschema:"S3 bucket"`
	S3Region     string `json:"s3_region" jsonschema:"S3 region"`
	PolicyID     string `json:"policy_id,omitempty" jsonschema:"optional existing policy UUID to reuse stored creds"`
	S3AccessKey  string `json:"s3_access_key,omitempty" jsonschema:"S3 access key (or reuse policy creds)"`
	S3SecretKey  string `json:"s3_secret_key,omitempty" jsonschema:"S3 secret key (or reuse policy creds)"`
	S3PathPrefix string `json:"s3_path_prefix,omitempty" jsonschema:"optional path prefix"`
}

func backupManagedDatabase(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (ObjectResult, error) {
	return objectResult(cl.BackupManagedDatabase(ctx, in.ID))
}

func promoteManagedDatabase(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (OKResult, error) {
	if err := cl.PromoteManagedDatabase(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("promote queued"), nil
}

func restoreManagedDatabase(ctx context.Context, cl *client.Client, in DBRestoreInput) (OKResult, error) {
	if err := cl.RestoreManagedDatabase(ctx, in.ID, map[string]any{"backup_id": in.BackupID}); err != nil {
		return OKResult{}, err
	}
	return okResult("restore queued"), nil
}

func restoreManagedDatabasePitr(ctx context.Context, cl *client.Client, in DBRestorePitrInput) (OKResult, error) {
	if err := cl.RestoreManagedDatabasePitr(ctx, in.ID, map[string]any{"target_time": in.TargetTime}); err != nil {
		return OKResult{}, err
	}
	return okResult("point-in-time restore queued"), nil
}

func retryManagedDatabasePitr(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (OKResult, error) {
	if err := cl.RetryManagedDatabasePitr(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("PITR configuration retry queued"), nil
}

func applyDatabaseParameterGroup(ctx context.Context, cl *client.Client, in DBApplyParameterGroupInput) (OKResult, error) {
	if err := cl.ApplyDatabaseParameterGroup(ctx, in.ID, map[string]any{"parameter_group_id": in.ParameterGroupID}); err != nil {
		return OKResult{}, err
	}
	return okResult("parameter group applied"), nil
}

func updateInstanceBackupPolicy(ctx context.Context, cl *client.Client, in UpdateInstanceBackupPolicyInput) (BackupPolicyResult, error) {
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
	obj, err := cl.UpdateInstanceBackupPolicy(ctx, in.ID, body)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	return BackupPolicyResult{Policy: obj}, nil
}

func resetInstanceBackupPolicyFailures(ctx context.Context, cl *client.Client, in InstanceBackupPolicyIDInput) (OKResult, error) {
	if err := cl.ResetInstanceBackupPolicyFailures(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("failures reset"), nil
}

func updateDBBackupPolicy(ctx context.Context, cl *client.Client, in UpdateDBBackupPolicyInput) (BackupPolicyResult, error) {
	body := map[string]any{}
	setStr := func(k string, v *string) {
		if v != nil {
			body[k] = *v
		}
	}
	setInt := func(k string, v *int) {
		if v != nil {
			body[k] = *v
		}
	}
	setBool := func(k string, v *bool) {
		if v != nil {
			body[k] = *v
		}
	}
	setStr("name", in.Name)
	setStr("s3_endpoint", in.S3Endpoint)
	setStr("s3_bucket", in.S3Bucket)
	setStr("s3_region", in.S3Region)
	setStr("s3_access_key", in.S3AccessKey)
	setStr("s3_secret_key", in.S3SecretKey)
	setStr("s3_path_prefix", in.S3PathPrefix)
	setStr("full_backup_frequency", in.FullBackupFrequency)
	setStr("full_backup_time", in.FullBackupTime)
	setInt("full_backup_day", in.FullBackupDay)
	setStr("incremental_frequency", in.IncrementalFrequency)
	setBool("pitr_enabled", in.PitrEnabled)
	setInt("retention_full_count", in.RetentionFullCount)
	setInt("retention_incremental_days", in.RetentionIncrementalDays)
	setInt("retention_pitr_hours", in.RetentionPitrHours)
	setBool("encryption_enabled", in.EncryptionEnabled)

	obj, err := cl.UpdateDBBackupPolicy(ctx, in.ID, body)
	if err != nil {
		return BackupPolicyResult{}, err
	}
	return BackupPolicyResult{Policy: obj}, nil
}

func resetDBBackupPolicyFailures(ctx context.Context, cl *client.Client, in DBBackupPolicyIDInput) (OKResult, error) {
	if err := cl.ResetDBBackupPolicyFailures(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("failures reset"), nil
}

func testDBBackupPolicyConnection(ctx context.Context, cl *client.Client, in TestDBBackupPolicyConnectionInput) (OKResult, error) {
	body := map[string]any{"s3_endpoint": in.S3Endpoint, "s3_bucket": in.S3Bucket, "s3_region": in.S3Region}
	if in.PolicyID != "" {
		body["policy_id"] = in.PolicyID
	}
	if in.S3AccessKey != "" {
		body["s3_access_key"] = in.S3AccessKey
	}
	if in.S3SecretKey != "" {
		body["s3_secret_key"] = in.S3SecretKey
	}
	if in.S3PathPrefix != "" {
		body["s3_path_prefix"] = in.S3PathPrefix
	}
	if _, err := cl.TestDBBackupPolicyConnection(ctx, body); err != nil {
		return OKResult{}, err
	}
	return okResult("S3 connection ok"), nil
}

func registerParityDBTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.managed_database.backup", Description: "Take an on-demand backup of a managed database."}, backupManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.promote", Description: "Promote a managed database replica to primary."}, promoteManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.restore", Description: "Restore a managed database from a backup."}, restoreManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.restore_pitr", Description: "Restore a managed database to a point in time."}, restoreManagedDatabasePitr)
	Register(s, deps, Spec{Name: "user.managed_database.retry_pitr", Description: "Retry a managed database's PITR configuration."}, retryManagedDatabasePitr)
	Register(s, deps, Spec{Name: "user.managed_database.apply_parameter_group", Description: "Apply a parameter group to a managed database."}, applyDatabaseParameterGroup)

	Register(s, deps, Spec{Name: "user.instance_backup_policy.update", Description: "Update an instance backup policy (all fields required)."}, updateInstanceBackupPolicy)
	Register(s, deps, Spec{Name: "user.instance_backup_policy.reset_failures", Description: "Reset an instance backup policy's failure counters."}, resetInstanceBackupPolicyFailures)

	Register(s, deps, Spec{Name: "user.db_backup_policy.update", Description: "Update a database backup policy's fields."}, updateDBBackupPolicy)
	Register(s, deps, Spec{Name: "user.db_backup_policy.reset_failures", Description: "Reset a database backup policy's failure counters."}, resetDBBackupPolicyFailures)
	Register(s, deps, Spec{Name: "user.db_backup_policy.test_connection", Description: "Test a database backup policy's S3 connection."}, testDBBackupPolicyConnection)
}
