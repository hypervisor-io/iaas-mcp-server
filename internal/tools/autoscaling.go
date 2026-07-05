package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Autoscaling tools, mirroring the iaas_autoscaling_group and
// iaas_autoscaling_policy resources. Group create/update/pause/resume are
// synchronous; delete is async (poll to 404). Policies are children keyed by
// group id (no policy SHOW route; the client resolves them by scanning the
// group). The group SHOW envelope key is "scaling_group" (create uses "group").

func init() {
	toolRegistrars = append(toolRegistrars, registerAutoscalingTools)
}

// ── group inputs / outputs ──────────────────────────────────────────────────

type CreateAutoscalingGroupInput struct {
	Name              string   `json:"name" jsonschema:"scaling group name"`
	HypervisorGroupID string   `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group"`
	PlanID            string   `json:"plan_id" jsonschema:"UUID of the instance plan"`
	ImageID           string   `json:"image_id" jsonschema:"UUID of the OS image"`
	VPCID             string   `json:"vpc_id,omitempty" jsonschema:"optional VPC UUID"`
	VPCSubnetID       string   `json:"vpc_subnet_id,omitempty" jsonschema:"optional VPC subnet UUID (with vpc_id)"`
	LoadBalancerID    string   `json:"load_balancer_id,omitempty" jsonschema:"optional load balancer UUID"`
	LBBackendID       string   `json:"lb_backend_id,omitempty" jsonschema:"optional LB backend UUID (with load_balancer_id)"`
	MinInstances      *int     `json:"min_instances,omitempty" jsonschema:"minimum instance count"`
	MaxInstances      *int     `json:"max_instances,omitempty" jsonschema:"maximum instance count"`
	CloudInit         string   `json:"cloud_init,omitempty" jsonschema:"optional cloud-init user data"`
	SSHKeys           []string `json:"ssh_keys,omitempty" jsonschema:"SSH key UUIDs"`
	SecurityGroupIDs  []string `json:"security_group_ids,omitempty" jsonschema:"security group UUIDs"`
}

type GetAutoscalingGroupInput struct {
	ID string `json:"id" jsonschema:"UUID of the scaling group"`
}

type ListAutoscalingGroupsInput struct{}

type UpdateAutoscalingGroupInput struct {
	ID           string  `json:"id" jsonschema:"UUID of the scaling group"`
	Name         *string `json:"name,omitempty"`
	MinInstances *int    `json:"min_instances,omitempty"`
	MaxInstances *int    `json:"max_instances,omitempty"`
}

type AutoscalingGroupIDInput struct {
	ID string `json:"id" jsonschema:"UUID of the scaling group"`
}

type DeleteAutoscalingGroupInput struct {
	ID string `json:"id" jsonschema:"UUID of the scaling group to delete"`
	Confirmation
}

type AutoscalingGroupResult struct {
	Group map[string]any `json:"group"`
}

type AutoscalingGroupListResult struct {
	Groups []map[string]any `json:"groups"`
	Count  int              `json:"count"`
}

// ── policy inputs / outputs ─────────────────────────────────────────────────

type CreateAutoscalingPolicyInput struct {
	GroupID            string  `json:"group_id" jsonschema:"UUID of the parent scaling group"`
	Metric             string  `json:"metric" jsonschema:"cpu or memory"`
	ScaleUpThreshold   float64 `json:"scale_up_threshold" jsonschema:"threshold to scale up"`
	ScaleDownThreshold float64 `json:"scale_down_threshold" jsonschema:"threshold to scale down"`
	ScaleUpStep        *int    `json:"scale_up_step,omitempty"`
	ScaleDownStep      *int    `json:"scale_down_step,omitempty"`
	ScaleUpCooldown    *int    `json:"scale_up_cooldown,omitempty"`
	ScaleDownCooldown  *int    `json:"scale_down_cooldown,omitempty"`
	EvaluationInterval *int    `json:"evaluation_interval,omitempty"`
	EvaluationWindow   *int    `json:"evaluation_window,omitempty"`
}

type GetAutoscalingPolicyInput struct {
	GroupID  string `json:"group_id" jsonschema:"UUID of the parent scaling group"`
	PolicyID string `json:"policy_id" jsonschema:"UUID of the policy"`
}

type UpdateAutoscalingPolicyInput struct {
	GroupID            string   `json:"group_id" jsonschema:"UUID of the parent scaling group"`
	PolicyID           string   `json:"policy_id" jsonschema:"UUID of the policy"`
	ScaleUpThreshold   *float64 `json:"scale_up_threshold,omitempty"`
	ScaleDownThreshold *float64 `json:"scale_down_threshold,omitempty"`
	ScaleUpStep        *int     `json:"scale_up_step,omitempty"`
	ScaleDownStep      *int     `json:"scale_down_step,omitempty"`
}

type DeleteAutoscalingPolicyInput struct {
	GroupID  string `json:"group_id" jsonschema:"UUID of the parent scaling group"`
	PolicyID string `json:"policy_id" jsonschema:"UUID of the policy to delete"`
}

type AutoscalingPolicyResult struct {
	Policy map[string]any `json:"policy"`
}

// ── group handlers ──────────────────────────────────────────────────────────

func createAutoscalingGroup(ctx context.Context, cl *client.Client, in CreateAutoscalingGroupInput) (AutoscalingGroupResult, error) {
	body := map[string]any{
		"name":                in.Name,
		"hypervisor_group_id": in.HypervisorGroupID,
		"plan_id":             in.PlanID,
		"image_id":            in.ImageID,
	}
	if in.VPCID != "" {
		body["vpc_id"] = in.VPCID
	}
	if in.VPCSubnetID != "" {
		body["vpc_subnet_id"] = in.VPCSubnetID
	}
	if in.LoadBalancerID != "" {
		body["load_balancer_id"] = in.LoadBalancerID
	}
	if in.LBBackendID != "" {
		body["lb_backend_id"] = in.LBBackendID
	}
	if in.MinInstances != nil {
		body["min_instances"] = *in.MinInstances
	}
	if in.MaxInstances != nil {
		body["max_instances"] = *in.MaxInstances
	}
	if in.CloudInit != "" {
		body["cloud_init"] = in.CloudInit
	}
	if in.SSHKeys != nil {
		body["ssh_keys"] = in.SSHKeys
	}
	if in.SecurityGroupIDs != nil {
		body["security_group_ids"] = in.SecurityGroupIDs
	}
	obj, err := cl.CreateAutoscalingGroup(ctx, body)
	if err != nil {
		return AutoscalingGroupResult{}, err
	}
	return AutoscalingGroupResult{Group: obj}, nil
}

func getAutoscalingGroup(ctx context.Context, cl *client.Client, in GetAutoscalingGroupInput) (AutoscalingGroupResult, error) {
	obj, err := cl.GetAutoscalingGroup(ctx, in.ID)
	if err != nil {
		return AutoscalingGroupResult{}, err
	}
	return AutoscalingGroupResult{Group: obj}, nil
}

func listAutoscalingGroups(ctx context.Context, cl *client.Client, _ ListAutoscalingGroupsInput) (AutoscalingGroupListResult, error) {
	items, err := cl.ListAutoscalingGroups(ctx)
	if err != nil {
		return AutoscalingGroupListResult{}, err
	}
	return AutoscalingGroupListResult{Groups: items, Count: len(items)}, nil
}

func updateAutoscalingGroup(ctx context.Context, cl *client.Client, in UpdateAutoscalingGroupInput) (AutoscalingGroupResult, error) {
	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.MinInstances != nil {
		fields["min_instances"] = *in.MinInstances
	}
	if in.MaxInstances != nil {
		fields["max_instances"] = *in.MaxInstances
	}
	obj, err := cl.UpdateAutoscalingGroup(ctx, in.ID, fields)
	if err != nil {
		return AutoscalingGroupResult{}, err
	}
	return AutoscalingGroupResult{Group: obj}, nil
}

func pauseAutoscalingGroup(ctx context.Context, cl *client.Client, in AutoscalingGroupIDInput) (AutoscalingGroupResult, error) {
	obj, err := cl.PauseAutoscalingGroup(ctx, in.ID)
	if err != nil {
		return AutoscalingGroupResult{}, err
	}
	return AutoscalingGroupResult{Group: obj}, nil
}

func resumeAutoscalingGroup(ctx context.Context, cl *client.Client, in AutoscalingGroupIDInput) (AutoscalingGroupResult, error) {
	obj, err := cl.ResumeAutoscalingGroup(ctx, in.ID)
	if err != nil {
		return AutoscalingGroupResult{}, err
	}
	return AutoscalingGroupResult{Group: obj}, nil
}

func deleteAutoscalingGroup(ctx context.Context, cl *client.Client, in DeleteAutoscalingGroupInput) (DeleteResult, error) {
	if err := cl.DeleteAutoscalingGroup(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	err := waitForGone(ctx, func() (map[string]any, error) { return cl.GetAutoscalingGroup(ctx, in.ID) }, defaultDeleteTimeout)
	if err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// ── policy handlers ─────────────────────────────────────────────────────────

func createAutoscalingPolicy(ctx context.Context, cl *client.Client, in CreateAutoscalingPolicyInput) (AutoscalingPolicyResult, error) {
	body := map[string]any{
		"metric":               in.Metric,
		"scale_up_threshold":   in.ScaleUpThreshold,
		"scale_down_threshold": in.ScaleDownThreshold,
	}
	if in.ScaleUpStep != nil {
		body["scale_up_step"] = *in.ScaleUpStep
	}
	if in.ScaleDownStep != nil {
		body["scale_down_step"] = *in.ScaleDownStep
	}
	if in.ScaleUpCooldown != nil {
		body["scale_up_cooldown"] = *in.ScaleUpCooldown
	}
	if in.ScaleDownCooldown != nil {
		body["scale_down_cooldown"] = *in.ScaleDownCooldown
	}
	if in.EvaluationInterval != nil {
		body["evaluation_interval"] = *in.EvaluationInterval
	}
	if in.EvaluationWindow != nil {
		body["evaluation_window"] = *in.EvaluationWindow
	}
	obj, err := cl.CreateAutoscalingPolicy(ctx, in.GroupID, body)
	if err != nil {
		return AutoscalingPolicyResult{}, err
	}
	return AutoscalingPolicyResult{Policy: obj}, nil
}

func getAutoscalingPolicy(ctx context.Context, cl *client.Client, in GetAutoscalingPolicyInput) (AutoscalingPolicyResult, error) {
	obj, err := cl.GetAutoscalingPolicy(ctx, in.GroupID, in.PolicyID)
	if err != nil {
		return AutoscalingPolicyResult{}, err
	}
	return AutoscalingPolicyResult{Policy: obj}, nil
}

func updateAutoscalingPolicy(ctx context.Context, cl *client.Client, in UpdateAutoscalingPolicyInput) (AutoscalingPolicyResult, error) {
	body := map[string]any{}
	if in.ScaleUpThreshold != nil {
		body["scale_up_threshold"] = *in.ScaleUpThreshold
	}
	if in.ScaleDownThreshold != nil {
		body["scale_down_threshold"] = *in.ScaleDownThreshold
	}
	if in.ScaleUpStep != nil {
		body["scale_up_step"] = *in.ScaleUpStep
	}
	if in.ScaleDownStep != nil {
		body["scale_down_step"] = *in.ScaleDownStep
	}
	obj, err := cl.UpdateAutoscalingPolicy(ctx, in.GroupID, in.PolicyID, body)
	if err != nil {
		return AutoscalingPolicyResult{}, err
	}
	return AutoscalingPolicyResult{Policy: obj}, nil
}

func deleteAutoscalingPolicy(ctx context.Context, cl *client.Client, in DeleteAutoscalingPolicyInput) (DeleteResult, error) {
	if err := cl.DeleteAutoscalingPolicy(ctx, in.GroupID, in.PolicyID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.PolicyID, Deleted: true}, nil
}

func registerAutoscalingTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.autoscaling_group.create", Description: "Create an autoscaling group."}, createAutoscalingGroup)
	Register(s, deps, Spec{Name: "user.autoscaling_group.list", Description: "List all autoscaling groups owned by the caller."}, listAutoscalingGroups)
	Register(s, deps, Spec{Name: "user.autoscaling_group.get", Description: "Get an autoscaling group by UUID (with policies)."}, getAutoscalingGroup)
	Register(s, deps, Spec{Name: "user.autoscaling_group.update", Description: "Update an autoscaling group's name or min/max instances."}, updateAutoscalingGroup)
	Register(s, deps, Spec{Name: "user.autoscaling_group.pause", Description: "Pause an autoscaling group."}, pauseAutoscalingGroup)
	Register(s, deps, Spec{Name: "user.autoscaling_group.resume", Description: "Resume an autoscaling group."}, resumeAutoscalingGroup)
	Register(s, deps, Spec{
		Name:        "user.autoscaling_group.delete",
		Description: "Delete an autoscaling group and wait until removed. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteAutoscalingGroup)

	Register(s, deps, Spec{Name: "user.autoscaling_policy.create", Description: "Add a scaling policy to an autoscaling group."}, createAutoscalingPolicy)
	Register(s, deps, Spec{Name: "user.autoscaling_policy.get", Description: "Get an autoscaling policy by group and policy UUID."}, getAutoscalingPolicy)
	Register(s, deps, Spec{Name: "user.autoscaling_policy.update", Description: "Update an autoscaling policy's thresholds or steps."}, updateAutoscalingPolicy)
	Register(s, deps, Spec{Name: "user.autoscaling_policy.delete", Description: "Delete an autoscaling policy."}, deleteAutoscalingPolicy)
}
