package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Managed database tools, mirroring the iaas_managed_database, iaas_db_replica,
// and iaas_db_parameter_group resources. Create is async (poll status to
// "active", fail "error"); delete converges to 404. Restart/resize/upgrade/
// reset_password/resync/retry/acknowledge are actions. A replica is its own
// database row, so its get/delete reuse the managed_database tools by the
// replica's id.
//
// promote, backup, and restore have no client methods (managed out-of-band), so
// they are not exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerManagedDatabaseTools)
}

// ── database inputs / outputs ───────────────────────────────────────────────

type CreateManagedDatabaseInput struct {
	Name              string `json:"name" jsonschema:"database name"`
	Engine            string `json:"engine" jsonschema:"mysql, mariadb, or postgresql"`
	EngineVersion     string `json:"engine_version" jsonschema:"engine version"`
	DBPlanID          string `json:"db_plan_id" jsonschema:"UUID of the database plan"`
	VPCID             string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	VPCSubnetID       string `json:"vpc_subnet_id" jsonschema:"UUID of the VPC subnet"`
	HypervisorGroupID string `json:"hypervisor_group_id,omitempty" jsonschema:"optional hypervisor group UUID"`
}

type GetManagedDatabaseInput struct {
	ID string `json:"id" jsonschema:"UUID of the managed database"`
}

type ListManagedDatabasesInput struct{}

type DeleteManagedDatabaseInput struct {
	ID string `json:"id" jsonschema:"UUID of the managed database to delete"`
	Confirmation
}

type ManagedDatabaseIDInput struct {
	ID string `json:"id" jsonschema:"UUID of the managed database"`
}

type ResizeManagedDatabaseInput struct {
	ID       string `json:"id" jsonschema:"UUID of the managed database"`
	DBPlanID string `json:"db_plan_id" jsonschema:"UUID of the new database plan"`
}

type UpgradeManagedDatabaseInput struct {
	ID            string `json:"id" jsonschema:"UUID of the managed database"`
	TargetVersion string `json:"target_version" jsonschema:"target engine version to upgrade to"`
}

type ManagedDatabaseResult struct {
	ManagedDatabase map[string]any `json:"managed_database"`
}

type ManagedDatabaseListResult struct {
	Databases []map[string]any `json:"databases"`
	Count     int              `json:"count"`
}

type PasswordResult struct {
	Password string `json:"password"`
}

// ── replica inputs / outputs ────────────────────────────────────────────────

type CreateDatabaseReplicaInput struct {
	PrimaryID   string `json:"primary_id" jsonschema:"UUID of the primary database"`
	DBPlanID    string `json:"db_plan_id" jsonschema:"UUID of the database plan for the replica"`
	VPCSubnetID string `json:"vpc_subnet_id" jsonschema:"UUID of the VPC subnet for the replica"`
	Name        string `json:"name,omitempty" jsonschema:"optional replica name"`
}

// ── parameter group inputs / outputs ────────────────────────────────────────

type CreateDBParameterGroupInput struct {
	Name       string         `json:"name" jsonschema:"parameter group name"`
	Engine     string         `json:"engine" jsonschema:"mysql, mariadb, or postgresql"`
	Parameters map[string]any `json:"parameters" jsonschema:"engine parameters as key/value pairs"`
}

type GetDBParameterGroupInput struct {
	ID string `json:"id" jsonschema:"UUID of the parameter group"`
}

type ListDBParameterGroupsInput struct{}

type UpdateDBParameterGroupInput struct {
	ID         string         `json:"id" jsonschema:"UUID of the parameter group"`
	Name       *string        `json:"name,omitempty" jsonschema:"new name"`
	Parameters map[string]any `json:"parameters,omitempty" jsonschema:"replacement parameter map"`
}

type DeleteDBParameterGroupInput struct {
	ID string `json:"id" jsonschema:"UUID of the parameter group to delete"`
	Confirmation
}

type DBParameterGroupResult struct {
	ParameterGroup map[string]any `json:"parameter_group"`
}

type DBParameterGroupListResult struct {
	ParameterGroups []map[string]any `json:"parameter_groups"`
	Count           int              `json:"count"`
}

// ── database handlers ───────────────────────────────────────────────────────

func createManagedDatabase(ctx context.Context, cl *client.Client, in CreateManagedDatabaseInput) (ManagedDatabaseResult, error) {
	body := map[string]any{
		"name":           in.Name,
		"engine":         in.Engine,
		"engine_version": in.EngineVersion,
		"db_plan_id":     in.DBPlanID,
		"vpc_id":         in.VPCID,
		"vpc_subnet_id":  in.VPCSubnetID,
	}
	if in.HypervisorGroupID != "" {
		body["hypervisor_group_id"] = in.HypervisorGroupID
	}
	return createManagedDatabaseAndWait(ctx, cl, func() (map[string]any, error) {
		return cl.CreateManagedDatabase(ctx, body)
	})
}

func createManagedDatabaseAndWait(ctx context.Context, cl *client.Client, create func() (map[string]any, error)) (ManagedDatabaseResult, error) {
	created, err := create()
	if err != nil {
		return ManagedDatabaseResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return ManagedDatabaseResult{}, fmt.Errorf("create response did not include a database id")
	}
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetManagedDatabase(ctx, id) },
		"status", []string{"active"}, []string{"error"}, defaultCreateTimeout)
	if err != nil {
		return ManagedDatabaseResult{}, fmt.Errorf("database %s did not become active: %w", id, err)
	}
	obj, err := cl.GetManagedDatabase(ctx, id)
	if err != nil {
		return ManagedDatabaseResult{}, err
	}
	return ManagedDatabaseResult{ManagedDatabase: obj}, nil
}

func getManagedDatabase(ctx context.Context, cl *client.Client, in GetManagedDatabaseInput) (ManagedDatabaseResult, error) {
	obj, err := cl.GetManagedDatabase(ctx, in.ID)
	if err != nil {
		return ManagedDatabaseResult{}, err
	}
	return ManagedDatabaseResult{ManagedDatabase: obj}, nil
}

func listManagedDatabases(ctx context.Context, cl *client.Client, _ ListManagedDatabasesInput) (ManagedDatabaseListResult, error) {
	items, err := cl.ListManagedDatabases(ctx)
	if err != nil {
		return ManagedDatabaseListResult{}, err
	}
	return ManagedDatabaseListResult{Databases: items, Count: len(items)}, nil
}

func deleteManagedDatabase(ctx context.Context, cl *client.Client, in DeleteManagedDatabaseInput) (DeleteResult, error) {
	if err := cl.DeleteManagedDatabase(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	err := waitForGone(ctx, func() (map[string]any, error) { return cl.GetManagedDatabase(ctx, in.ID) }, defaultDeleteTimeout)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("database %s was not removed: %w", in.ID, err)
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func restartManagedDatabase(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (OKResult, error) {
	if err := cl.RestartManagedDatabase(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("database restart queued"), nil
}

func resetManagedDatabasePassword(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (PasswordResult, error) {
	obj, err := cl.ResetManagedDatabasePassword(ctx, in.ID)
	if err != nil {
		return PasswordResult{}, err
	}
	pw, _ := obj["password"].(string)
	return PasswordResult{Password: pw}, nil
}

func resizeManagedDatabase(ctx context.Context, cl *client.Client, in ResizeManagedDatabaseInput) (ManagedDatabaseResult, error) {
	obj, err := cl.ResizeManagedDatabase(ctx, in.ID, map[string]any{"db_plan_id": in.DBPlanID})
	if err != nil {
		return ManagedDatabaseResult{}, err
	}
	return ManagedDatabaseResult{ManagedDatabase: obj}, nil
}

func upgradeManagedDatabase(ctx context.Context, cl *client.Client, in UpgradeManagedDatabaseInput) (OKResult, error) {
	if err := cl.UpgradeManagedDatabase(ctx, in.ID, in.TargetVersion); err != nil {
		return OKResult{}, err
	}
	return okResult("database upgrade to " + in.TargetVersion + " queued"), nil
}

func resyncManagedDatabaseReplicas(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (OKResult, error) {
	if err := cl.ResyncManagedDatabaseReplicas(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("replica resync queued"), nil
}

func retryManagedDatabase(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (OKResult, error) {
	if err := cl.RetryManagedDatabase(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("database retry queued"), nil
}

func acknowledgeManagedDatabaseError(ctx context.Context, cl *client.Client, in ManagedDatabaseIDInput) (OKResult, error) {
	if err := cl.AcknowledgeManagedDatabaseError(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("error acknowledged"), nil
}

// ── replica handlers ────────────────────────────────────────────────────────

func createDatabaseReplica(ctx context.Context, cl *client.Client, in CreateDatabaseReplicaInput) (ManagedDatabaseResult, error) {
	body := map[string]any{"db_plan_id": in.DBPlanID, "vpc_subnet_id": in.VPCSubnetID}
	if in.Name != "" {
		body["name"] = in.Name
	}
	// A replica converges the same way a primary does (its own row -> active).
	return createManagedDatabaseAndWait(ctx, cl, func() (map[string]any, error) {
		return cl.CreateDatabaseReplica(ctx, in.PrimaryID, body)
	})
}

// ── parameter group handlers ────────────────────────────────────────────────

func createDBParameterGroup(ctx context.Context, cl *client.Client, in CreateDBParameterGroupInput) (DBParameterGroupResult, error) {
	body := map[string]any{"name": in.Name, "engine": in.Engine, "parameters": in.Parameters}
	obj, err := cl.CreateDBParameterGroup(ctx, body)
	if err != nil {
		return DBParameterGroupResult{}, err
	}
	return DBParameterGroupResult{ParameterGroup: obj}, nil
}

func getDBParameterGroup(ctx context.Context, cl *client.Client, in GetDBParameterGroupInput) (DBParameterGroupResult, error) {
	obj, err := cl.GetDBParameterGroup(ctx, in.ID)
	if err != nil {
		return DBParameterGroupResult{}, err
	}
	return DBParameterGroupResult{ParameterGroup: obj}, nil
}

func listDBParameterGroups(ctx context.Context, cl *client.Client, _ ListDBParameterGroupsInput) (DBParameterGroupListResult, error) {
	items, err := cl.ListDBParameterGroups(ctx)
	if err != nil {
		return DBParameterGroupListResult{}, err
	}
	return DBParameterGroupListResult{ParameterGroups: items, Count: len(items)}, nil
}

func updateDBParameterGroup(ctx context.Context, cl *client.Client, in UpdateDBParameterGroupInput) (DBParameterGroupResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Parameters != nil {
		body["parameters"] = in.Parameters
	}
	obj, err := cl.UpdateDBParameterGroup(ctx, in.ID, body)
	if err != nil {
		return DBParameterGroupResult{}, err
	}
	return DBParameterGroupResult{ParameterGroup: obj}, nil
}

func deleteDBParameterGroup(ctx context.Context, cl *client.Client, in DeleteDBParameterGroupInput) (DeleteResult, error) {
	if err := cl.DeleteDBParameterGroup(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerManagedDatabaseTools(s *mcp.Server, deps Deps) {
	// Databases.
	Register(s, deps, Spec{Name: "user.managed_database.create", Description: "Create a managed database and wait until it is active."}, createManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.list", Description: "List all managed databases owned by the caller."}, listManagedDatabases)
	Register(s, deps, Spec{Name: "user.managed_database.get", Description: "Get a managed database by UUID."}, getManagedDatabase)
	Register(s, deps, Spec{
		Name:        "user.managed_database.delete",
		Description: "Delete a managed database and wait until removed. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.restart", Description: "Restart a managed database."}, restartManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.reset_password", Description: "Reset the admin password; returns the new password once."}, resetManagedDatabasePassword)
	Register(s, deps, Spec{Name: "user.managed_database.resize", Description: "Resize a managed database to a new plan."}, resizeManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.upgrade", Description: "Upgrade a managed database to a target engine version."}, upgradeManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.resync_replicas", Description: "Resync a managed database's replicas."}, resyncManagedDatabaseReplicas)
	Register(s, deps, Spec{Name: "user.managed_database.retry", Description: "Retry a failed managed database provisioning."}, retryManagedDatabase)
	Register(s, deps, Spec{Name: "user.managed_database.acknowledge_error", Description: "Acknowledge a managed database's last error."}, acknowledgeManagedDatabaseError)

	// Replica (get/delete via the managed_database tools by the replica's id).
	Register(s, deps, Spec{Name: "user.db_replica.create", Description: "Create a read replica of a primary database and wait until active."}, createDatabaseReplica)

	// Parameter groups.
	Register(s, deps, Spec{Name: "user.db_parameter_group.create", Description: "Create a database parameter group."}, createDBParameterGroup)
	Register(s, deps, Spec{Name: "user.db_parameter_group.list", Description: "List all database parameter groups."}, listDBParameterGroups)
	Register(s, deps, Spec{Name: "user.db_parameter_group.get", Description: "Get a database parameter group by UUID."}, getDBParameterGroup)
	Register(s, deps, Spec{Name: "user.db_parameter_group.update", Description: "Update a parameter group's name or parameters."}, updateDBParameterGroup)
	Register(s, deps, Spec{
		Name:        "user.db_parameter_group.delete",
		Description: "Delete a database parameter group. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDBParameterGroup)
}
