package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// CI runner pool and job tools, mirroring the MV3-23/MV3-24 user_api
// endpoints. A pool is a CI-provider configuration row (a GitHub App
// installation or a GitLab runner registration) that maps CI jobs onto
// MicroVM capacity in a hypervisor group under a runner-kind plan. GitHub
// pools have no create tool by design - the panel's GitHub App install flow
// (MV3-7) owns their creation, same restriction as MV3-25's OpenTofu
// resource - but update and delete accept them. delete is confirm-gated
// (destructive). gitlab_token is write-only: it is sent to the API (which
// verifies it against GitLab and stores it encrypted) and never echoed in
// any tool result.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmRunnerTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListRunnerPoolsInput struct{}

type CreateGitlabRunnerPoolInput struct {
	GitlabURL         string   `json:"gitlab_url" jsonschema:"base URL of the GitLab instance, e.g. https://gitlab.com"`
	GitlabToken       string   `json:"gitlab_token" jsonschema:"GitLab runner authentication token (glrt-...); verified with GitLab on create, write-only, never returned in a result"`
	GitlabTagList     []string `json:"gitlab_tag_list,omitempty" jsonschema:"tags the runners advertise; GitLab jobs pick runners by tag"`
	GitlabRunUntagged *bool    `json:"gitlab_run_untagged,omitempty" jsonschema:"whether the runners also pick up untagged jobs"`
	HypervisorGroupID string   `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (location) the runner MicroVMs run in"`
	PlanID            string   `json:"plan_id" jsonschema:"UUID of the runner-kind MicroVM plan"`
	MaxConcurrent     int      `json:"max_concurrent,omitempty" jsonschema:"max concurrent jobs for this pool, 1-20; the API default applies when unset"`
	WarmCount         int      `json:"warm_count" jsonschema:"number of pre-warmed standby runners, 0-5"`
}

// UpdateRunnerPoolInput carries the union of the github and gitlab update
// fields (MV3-12's UpdateGithub/UpdateGitlabRunnerPoolRequest); which set the
// API honors depends on the provider path segment. Only fields the caller
// actually set are sent.
type UpdateRunnerPoolInput struct {
	Provider          string   `json:"provider" jsonschema:"pool provider path segment: github or gitlab"`
	ID                string   `json:"id" jsonschema:"UUID of the pool to update"`
	Labels            []string `json:"labels,omitempty" jsonschema:"github only: runner labels, 1-5 lowercase alphanumeric plus hyphen"`
	GitlabURL         string   `json:"gitlab_url,omitempty" jsonschema:"gitlab only: new base URL of the GitLab instance"`
	GitlabToken       string   `json:"gitlab_token,omitempty" jsonschema:"gitlab only: replacement runner authentication token (glrt-...); write-only, never returned in a result"`
	GitlabTagList     []string `json:"gitlab_tag_list,omitempty" jsonschema:"gitlab only: tags the runners advertise"`
	GitlabRunUntagged *bool    `json:"gitlab_run_untagged,omitempty" jsonschema:"gitlab only: whether the runners also pick up untagged jobs"`
	HypervisorGroupID string   `json:"hypervisor_group_id,omitempty" jsonschema:"UUID of a new hypervisor group for the runner MicroVMs"`
	PlanID            string   `json:"plan_id,omitempty" jsonschema:"UUID of a new runner-kind MicroVM plan"`
	MaxConcurrent     int      `json:"max_concurrent,omitempty" jsonschema:"max concurrent jobs for this pool, 1-20"`
	WarmCount         *int     `json:"warm_count,omitempty" jsonschema:"gitlab only: number of pre-warmed standby runners, 0-5"`
	Enabled           *bool    `json:"enabled,omitempty" jsonschema:"github only: enable or disable the pool"`
}

// DeleteRunnerPoolInput removes a pool (either provider). Confirm-gated
// (destructive) - the pool configuration is gone for good and its warm
// runners are torn down.
type DeleteRunnerPoolInput struct {
	ID string `json:"id" jsonschema:"UUID of the pool to delete"`
	Confirmation
}

type ListRunnerJobsInput struct {
	PoolID string `json:"pool_id,omitempty" jsonschema:"restrict the job history to one pool"`
}

type RunnerPoolResult struct {
	Pool map[string]any `json:"pool"`
}

type RunnerPoolListResult struct {
	Pools []map[string]any `json:"pools"`
	Count int              `json:"count"`
}

type RunnerJobListResult struct {
	Jobs  []map[string]any `json:"jobs"`
	Count int              `json:"count"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listRunnerPools(ctx context.Context, cl *client.Client, _ ListRunnerPoolsInput) (RunnerPoolListResult, error) {
	items, err := cl.ListRunnerPools(ctx)
	if err != nil {
		return RunnerPoolListResult{}, err
	}
	return RunnerPoolListResult{Pools: items, Count: len(items)}, nil
}

// createGitlabRunnerPool sends the required fields plus only the optionals
// the caller set. gitlab_token rides along to the API (which verifies it
// against GitLab) but is write-only: the API response never carries it, so
// no result ever echoes it. warm_count is required and 0 is a legal value,
// so it is always sent.
func createGitlabRunnerPool(ctx context.Context, cl *client.Client, in CreateGitlabRunnerPoolInput) (RunnerPoolResult, error) {
	body := map[string]any{
		"gitlab_url":          in.GitlabURL,
		"gitlab_token":        in.GitlabToken,
		"hypervisor_group_id": in.HypervisorGroupID,
		"plan_id":             in.PlanID,
		"warm_count":          in.WarmCount,
	}
	if in.GitlabTagList != nil {
		body["gitlab_tag_list"] = in.GitlabTagList
	}
	if in.GitlabRunUntagged != nil {
		body["gitlab_run_untagged"] = *in.GitlabRunUntagged
	}
	if in.MaxConcurrent > 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	obj, err := cl.CreateGitlabRunnerPool(ctx, body)
	if err != nil {
		return RunnerPoolResult{}, err
	}
	return RunnerPoolResult{Pool: obj}, nil
}

// updateRunnerPool PUTs through the provider-specific route
// /microvm/runners/pools/{provider}/{id}; the client rejects any provider
// other than github/gitlab before a request is attempted. Only the fields
// the caller set are sent, so an unset field keeps its stored value; a set
// zero value (warm_count 0, enabled false) is sent deliberately.
func updateRunnerPool(ctx context.Context, cl *client.Client, in UpdateRunnerPoolInput) (RunnerPoolResult, error) {
	body := map[string]any{}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.GitlabURL != "" {
		body["gitlab_url"] = in.GitlabURL
	}
	if in.GitlabToken != "" {
		body["gitlab_token"] = in.GitlabToken
	}
	if in.GitlabTagList != nil {
		body["gitlab_tag_list"] = in.GitlabTagList
	}
	if in.GitlabRunUntagged != nil {
		body["gitlab_run_untagged"] = *in.GitlabRunUntagged
	}
	if in.HypervisorGroupID != "" {
		body["hypervisor_group_id"] = in.HypervisorGroupID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.MaxConcurrent > 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	if in.WarmCount != nil {
		body["warm_count"] = *in.WarmCount
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.UpdateRunnerPool(ctx, in.Provider, in.ID, body)
	if err != nil {
		return RunnerPoolResult{}, err
	}
	return RunnerPoolResult{Pool: obj}, nil
}

func deleteRunnerPool(ctx context.Context, cl *client.Client, in DeleteRunnerPoolInput) (DeleteResult, error) {
	if err := cl.DeleteRunnerPool(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func listRunnerJobs(ctx context.Context, cl *client.Client, in ListRunnerJobsInput) (RunnerJobListResult, error) {
	items, err := cl.ListRunnerJobs(ctx, in.PoolID)
	if err != nil {
		return RunnerJobListResult{}, err
	}
	return RunnerJobListResult{Jobs: items, Count: len(items)}, nil
}

func registerMicrovmRunnerTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.runner.pool.list", Description: "List the caller's CI runner pools (GitHub and GitLab)."}, listRunnerPools)
	Register(s, deps, Spec{Name: "user.microvm.runner.pool.create_gitlab", Description: "Create a GitLab CI runner pool. The runner authentication token is verified with GitLab and is write-only: it is never returned in a result. GitHub pools have no create tool: the panel's GitHub App install flow owns their creation."}, createGitlabRunnerPool)
	Register(s, deps, Spec{Name: "user.microvm.runner.pool.update", Description: "Update a CI runner pool; accepts github and gitlab pools. Only the fields passed are changed."}, updateRunnerPool)
	Register(s, deps, Spec{Name: "user.microvm.runner.pool.delete", Description: "Delete a CI runner pool (github or gitlab). Irreversible. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteRunnerPool)
	Register(s, deps, Spec{Name: "user.microvm.runner.job.list", Description: "List the caller's CI runner job history, optionally restricted to one pool."}, listRunnerJobs)
}
