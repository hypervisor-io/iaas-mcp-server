package tools

import (
	"context"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RestoreManagedDatabaseToNewInput struct {
	ID          string `json:"id" jsonschema:"UUID of the source managed database"`
	BackupID    string `json:"backup_id,omitempty" jsonschema:"completed S3 backup UUID; send exactly one of backup_id or target_time"`
	TargetTime  string `json:"target_time,omitempty" jsonschema:"ISO 8601 point inside the PITR window, max 64 characters; absent offset means UTC"`
	Name        string `json:"name,omitempty" jsonschema:"new database name, max 64 characters; defaults to source name with -restored"`
	DBPlanID    string `json:"db_plan_id,omitempty" jsonschema:"optional new plan UUID; defaults to source plan"`
	VPCID       string `json:"vpc_id,omitempty" jsonschema:"optional same-location VPC UUID; supply a subnet if setting this"`
	VPCSubnetID string `json:"vpc_subnet_id,omitempty" jsonschema:"subnet UUID of the VPC; defaults to source subnet only when vpc_id is omitted"`
}

func restoreManagedDatabaseToNew(ctx context.Context, cl *client.Client, in RestoreManagedDatabaseToNewInput) (client.ManagedDatabaseRestoreToNewResult, error) {
	body := map[string]any{}
	for key, value := range map[string]string{"backup_id": in.BackupID, "target_time": in.TargetTime, "name": in.Name, "db_plan_id": in.DBPlanID, "vpc_id": in.VPCID, "vpc_subnet_id": in.VPCSubnetID} {
		if value != "" {
			body[key] = value
		}
	}
	return cl.RestoreManagedDatabaseToNew(ctx, in.ID, body)
}
func init() { toolRegistrars = append(toolRegistrars, registerManagedDatabaseRestoreToNewTool) }
func registerManagedDatabaseRestoreToNewTool(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.managed_database.restore_to_new", Description: "Restore exactly one completed S3 backup or PITR target_time into a NEW database, leaving the source unchanged. Returns managed_database_id, task_id and restoring on acceptance; follow the new database until active. Billing starts on activation. Sent once: 503 deploy_dispatch_unconfirmed retains database/task IDs and ongoing reconciliation. Do not retry that request; follow the returned database. A transport failure may also leave work: inspect databases and tasks before retrying."}, restoreManagedDatabaseToNew)
}
