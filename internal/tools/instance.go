package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Golden instance tools. These mirror the iaas_instance provider resource's
// semantics exactly: the TWO-PHASE async create (record row -> deploy OS ->
// poll task to "completed"), the bare-model get, and the confirm-gated async
// delete that converges on a 404. Same endpoints, same required fields, same
// waiter predicates.

// ── inputs / outputs ────────────────────────────────────────────────────────

// CreateInstanceInput is user.instance.create's arguments. location_id,
// plan_id, and image_id are required (phase 1 records the row from
// location+plan; phase 2 deploys the OS from image). The rest are optional and
// mirror the provider's plan fields.
type CreateInstanceInput struct {
	LocationID  string   `json:"location_id" jsonschema:"UUID of the location/region to deploy in"`
	PlanID      string   `json:"plan_id" jsonschema:"UUID of the instance plan (sizing)"`
	ImageID     string   `json:"image_id" jsonschema:"UUID of the OS image to deploy"`
	VPCID       string   `json:"vpc_id,omitempty" jsonschema:"optional UUID of a VPC to attach at create time"`
	VPCSubnetID string   `json:"vpc_subnet_id,omitempty" jsonschema:"optional UUID of a VPC subnet (required with vpc_id)"`
	Hostname    string   `json:"hostname,omitempty" jsonschema:"optional hostname for the instance"`
	SSHKeys     []string `json:"ssh_keys,omitempty" jsonschema:"optional list of SSH key UUIDs to install"`
	Timezone    string   `json:"timezone,omitempty" jsonschema:"optional IANA timezone, e.g. Europe/London"`
	Cloudcfg    string   `json:"cloudcfg,omitempty" jsonschema:"optional cloud-init user-data (YAML or JSON)"`
}

// GetInstanceInput identifies one instance.
type GetInstanceInput struct {
	ID string `json:"id" jsonschema:"UUID of the instance"`
}

// ListInstancesInput takes no arguments (tenant scope is implicit in the token).
type ListInstancesInput struct{}

// DeleteInstanceInput is confirm-gated: Confirmation is embedded so the
// framework refuses the delete unless confirm:true is supplied.
type DeleteInstanceInput struct {
	ID string `json:"id" jsonschema:"UUID of the instance to delete"`
	Confirmation
}

// InstanceResult wraps a single instance object (the bare SHOW model).
type InstanceResult struct {
	Instance map[string]any `json:"instance"`
}

// InstanceListResult wraps the instance list plus its count.
type InstanceListResult struct {
	Instances []map[string]any `json:"instances"`
	Count     int              `json:"count"`
}

// DeleteResult reports a converged delete.
type DeleteResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// ── handlers ────────────────────────────────────────────────────────────────

// createInstance runs the provider's two-phase create then hydrates the result
// from SHOW. Phase 1 records the row (sync, returns id); phase 2 deploys the OS
// (async, returns task_id); the waiter converges on task status "completed".
func createInstance(ctx context.Context, cl *client.Client, in CreateInstanceInput) (InstanceResult, error) {
	// PHASE 1: record the instance row (sync, returns id).
	phase1 := map[string]any{
		"location_id": in.LocationID,
		"plan_id":     in.PlanID,
	}
	if in.VPCID != "" {
		phase1["vpc_id"] = in.VPCID
	}
	if in.VPCSubnetID != "" {
		phase1["vpc_subnet_id"] = in.VPCSubnetID
	}
	if in.Hostname != "" {
		phase1["hostname"] = in.Hostname
	}

	created, err := cl.CreateCSInstance(ctx, phase1)
	if err != nil {
		return InstanceResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return InstanceResult{}, fmt.Errorf("create response did not include an instance id")
	}

	// PHASE 2: deploy the OS (async, returns a top-level task_id).
	phase2 := map[string]any{"image_id": in.ImageID}
	if len(in.SSHKeys) > 0 {
		phase2["ssh_keys"] = in.SSHKeys
	}
	if in.Hostname != "" {
		phase2["hostname"] = in.Hostname
	}
	if in.Timezone != "" {
		phase2["timezone"] = in.Timezone
	}
	if in.Cloudcfg != "" {
		phase2["cloudcfg"] = in.Cloudcfg
	}

	deployResp, err := cl.DeployInstance(ctx, id, phase2)
	if err != nil {
		return InstanceResult{}, err
	}
	taskID, _ := deployResp["task_id"].(string)
	if taskID == "" {
		return InstanceResult{}, fmt.Errorf("deploy response did not include a task_id")
	}

	// ASYNC convergence: poll the deploy task until "completed" (fail "failed").
	if err := waitForInstanceDeploy(ctx, cl, id, taskID, defaultCreateTimeout); err != nil {
		return InstanceResult{}, fmt.Errorf("instance %s deploy task %s did not complete: %w", id, taskID, err)
	}

	// Hydrate from the now-deployed instance (bare SHOW model).
	obj, err := cl.GetInstance(ctx, id)
	if err != nil {
		return InstanceResult{}, err
	}
	return InstanceResult{Instance: obj}, nil
}

// getInstance returns the bare instance model; a 404 maps to a not-found error.
func getInstance(ctx context.Context, cl *client.Client, in GetInstanceInput) (InstanceResult, error) {
	obj, err := cl.GetInstance(ctx, in.ID)
	if err != nil {
		return InstanceResult{}, err
	}
	return InstanceResult{Instance: obj}, nil
}

// listInstances returns every instance visible to the token (auto-paginated).
func listInstances(ctx context.Context, cl *client.Client, _ ListInstancesInput) (InstanceListResult, error) {
	items, err := cl.ListCSInstances(ctx)
	if err != nil {
		return InstanceListResult{}, err
	}
	return InstanceListResult{Instances: items, Count: len(items)}, nil
}

// deleteInstance enqueues the delete then converges on a 404. Confirm-gated by
// the framework (DeleteInstanceInput embeds Confirm).
func deleteInstance(ctx context.Context, cl *client.Client, in DeleteInstanceInput) (DeleteResult, error) {
	if err := cl.DeleteCSInstance(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	if err := waitForInstanceGone(ctx, cl, in.ID, defaultDeleteTimeout); err != nil {
		return DeleteResult{}, fmt.Errorf("instance %s was not removed: %w", in.ID, err)
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// registerInstanceTools registers the four golden instance tools.
func registerInstanceTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name: "user.instance.create",
		Description: "Create and deploy a Cloud Service instance (VM). Records the instance, deploys the " +
			"OS image, and waits until the deploy task completes; returns the deployed instance.",
	}, createInstance)

	Register(s, deps, Spec{
		Name:        "user.instance.list",
		Description: "List all Cloud Service instances owned by the caller.",
	}, listInstances)

	Register(s, deps, Spec{
		Name:        "user.instance.get",
		Description: "Get a single Cloud Service instance by its UUID.",
	}, getInstance)

	Register(s, deps, Spec{
		Name: "user.instance.delete",
		Description: "Delete a Cloud Service instance and wait until it is fully removed. DESTRUCTIVE and " +
			"irreversible: requires \"confirm\": true.",
		Destructive: true,
	}, deleteInstance)
}
