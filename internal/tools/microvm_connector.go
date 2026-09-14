package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM connector tools, mirroring the MVU2-3 user_api endpoints. A
// connector is an integration (github_app | gitlab_runner) that launches
// MicroVMs automatically for incoming CI jobs. delete is confirm-gated
// (destructive: queued jobs stop dispatching). A connector's config (tokens,
// installation ids) is never serialized by the API.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmConnectorTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmConnectorsInput struct{}

type ListMicrovmConnectorJobsInput struct{}

type CreateGithubConnectorInput struct {
	GitSourceID       string   `json:"git_source_id" jsonschema:"UUID of an owned github_app Git Source that has an installation_id"`
	Name              string   `json:"name" jsonschema:"connector name"`
	Labels            []string `json:"labels,omitempty" jsonschema:"GitHub workflow job labels this connector picks up"`
	HypervisorGroupID string   `json:"hypervisor_group_id,omitempty" jsonschema:"UUID of the hypervisor group (location) jobs run in"`
	PlanID            string   `json:"plan_id,omitempty" jsonschema:"UUID of a plan in the location's plan group"`
	ImageID           string   `json:"image_id,omitempty" jsonschema:"UUID of the image jobs run, defaults to the base image named ci-runner"`
	MaxConcurrent     int      `json:"max_concurrent,omitempty" jsonschema:"max jobs running at once, 1-20, default 1"`
	WarmCount         int      `json:"warm_count,omitempty" jsonschema:"warm MicroVMs kept ready, 0-5, default 0"`
}

type CreateGitlabConnectorInput struct {
	Name              string   `json:"name" jsonschema:"connector name"`
	GitlabURL         string   `json:"gitlab_url" jsonschema:"GitLab instance URL"`
	GitlabToken       string   `json:"gitlab_token" jsonschema:"runner authentication token (glrt-...); stored encrypted and never returned"`
	GitlabTagList     []string `json:"gitlab_tag_list,omitempty" jsonschema:"GitLab job tags this connector picks up"`
	GitlabRunUntagged *bool    `json:"gitlab_run_untagged,omitempty" jsonschema:"also pick up untagged jobs"`
	HypervisorGroupID string   `json:"hypervisor_group_id,omitempty" jsonschema:"UUID of the hypervisor group (location) jobs run in"`
	PlanID            string   `json:"plan_id,omitempty" jsonschema:"UUID of a plan in the location's plan group"`
	ImageID           string   `json:"image_id,omitempty" jsonschema:"UUID of the image jobs run, defaults to the base image named ci-runner"`
	MaxConcurrent     int      `json:"max_concurrent,omitempty" jsonschema:"max jobs running at once, 1-20, default 1"`
	WarmCount         int      `json:"warm_count,omitempty" jsonschema:"warm MicroVMs kept ready, 0-5, default 0"`
}

// UpdateMicrovmConnectorInput carries only the fields to change; nil pointers
// and nil slices are omitted from the request body. Enabling without a plan
// and location fails 422 needs_plan.
type UpdateMicrovmConnectorInput struct {
	ID                string   `json:"id" jsonschema:"UUID of the connector"`
	Name              string   `json:"name,omitempty"`
	Labels            []string `json:"labels,omitempty" jsonschema:"full replacement label/tag list"`
	HypervisorGroupID string   `json:"hypervisor_group_id,omitempty"`
	PlanID            string   `json:"plan_id,omitempty"`
	ImageID           string   `json:"image_id,omitempty"`
	MaxConcurrent     *int     `json:"max_concurrent,omitempty" jsonschema:"1-20"`
	WarmCount         *int     `json:"warm_count,omitempty" jsonschema:"0-5"`
	Enabled           *bool    `json:"enabled,omitempty" jsonschema:"enable requires plan_id and hypervisor_group_id to be set"`
}

// DeleteMicrovmConnectorInput deletes a connector. Confirm-gated
// (destructive) - its queued jobs stop dispatching immediately.
type DeleteMicrovmConnectorInput struct {
	ID string `json:"id" jsonschema:"UUID of the connector to delete"`
	Confirmation
}

// MicrovmConnectorListResult is the INDEX payload: the connectors plus the
// create-context lists the agent needs before creating one (git_sources,
// github_app_configured).
type MicrovmConnectorListResult struct {
	Connectors          []map[string]any `json:"connectors"`
	Count               int              `json:"count"`
	GitSources          []map[string]any `json:"git_sources"`
	GithubAppConfigured bool             `json:"github_app_configured"`
}

type MicrovmConnectorJobListResult struct {
	Jobs  []map[string]any `json:"jobs"`
	Count int              `json:"count"`
}

type MicrovmConnectorResult struct {
	Connector map[string]any `json:"connector"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listMicrovmConnectors(ctx context.Context, cl *client.Client, _ ListMicrovmConnectorsInput) (MicrovmConnectorListResult, error) {
	env, err := cl.ListMicrovmConnectors(ctx)
	if err != nil {
		return MicrovmConnectorListResult{}, err
	}
	pag, _ := env["connectors"].(map[string]any)
	configured, _ := env["github_app_configured"].(bool)
	connectors := asObjectList(pag["data"])
	return MicrovmConnectorListResult{
		Connectors:          connectors,
		Count:               len(connectors),
		GitSources:          asObjectList(env["git_sources"]),
		GithubAppConfigured: configured,
	}, nil
}

func listMicrovmConnectorJobs(ctx context.Context, cl *client.Client, _ ListMicrovmConnectorJobsInput) (MicrovmConnectorJobListResult, error) {
	jobs, err := cl.ListMicrovmConnectorJobs(ctx)
	if err != nil {
		return MicrovmConnectorJobListResult{}, err
	}
	return MicrovmConnectorJobListResult{Jobs: jobs, Count: len(jobs)}, nil
}

func createGithubConnector(ctx context.Context, cl *client.Client, in CreateGithubConnectorInput) (MicrovmConnectorResult, error) {
	body := map[string]any{
		"git_source_id": in.GitSourceID,
		"name":          in.Name,
	}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.HypervisorGroupID != "" {
		body["hypervisor_group_id"] = in.HypervisorGroupID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.MaxConcurrent > 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	if in.WarmCount > 0 {
		body["warm_count"] = in.WarmCount
	}
	connector, err := cl.CreateGithubMicrovmConnector(ctx, body)
	if err != nil {
		return MicrovmConnectorResult{}, err
	}
	return MicrovmConnectorResult{Connector: connector}, nil
}

func createGitlabConnector(ctx context.Context, cl *client.Client, in CreateGitlabConnectorInput) (MicrovmConnectorResult, error) {
	body := map[string]any{
		"name":         in.Name,
		"gitlab_url":   in.GitlabURL,
		"gitlab_token": in.GitlabToken,
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
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.MaxConcurrent > 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	if in.WarmCount > 0 {
		body["warm_count"] = in.WarmCount
	}
	connector, err := cl.CreateGitlabMicrovmConnector(ctx, body)
	if err != nil {
		return MicrovmConnectorResult{}, err
	}
	return MicrovmConnectorResult{Connector: connector}, nil
}

// updateMicrovmConnector sends only the fields the caller actually set, so an
// omitted field keeps its current value server-side.
func updateMicrovmConnector(ctx context.Context, cl *client.Client, in UpdateMicrovmConnectorInput) (MicrovmConnectorResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.HypervisorGroupID != "" {
		body["hypervisor_group_id"] = in.HypervisorGroupID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.MaxConcurrent != nil {
		body["max_concurrent"] = *in.MaxConcurrent
	}
	if in.WarmCount != nil {
		body["warm_count"] = *in.WarmCount
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	connector, err := cl.UpdateMicrovmConnector(ctx, in.ID, body)
	if err != nil {
		return MicrovmConnectorResult{}, err
	}
	return MicrovmConnectorResult{Connector: connector}, nil
}

func deleteMicrovmConnector(ctx context.Context, cl *client.Client, in DeleteMicrovmConnectorInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovmConnector(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerMicrovmConnectorTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.connector.list", Description: "List the caller's connectors plus the create context (git sources, whether the GitHub App is configured)."}, listMicrovmConnectors)
	Register(s, deps, Spec{Name: "user.microvm.connector.jobs", Description: "List connector jobs (queued/dispatched/running/completed/failed), newest first."}, listMicrovmConnectorJobs)
	Register(s, deps, Spec{Name: "user.microvm.connector.create_github", Description: "Create a github_app connector from an installed github_app Git Source."}, createGithubConnector)
	Register(s, deps, Spec{Name: "user.microvm.connector.create_gitlab", Description: "Register a GitLab runner and create its connector. The token is stored encrypted and never returned."}, createGitlabConnector)
	Register(s, deps, Spec{Name: "user.microvm.connector.update", Description: "Update a connector's name, labels, location, plan, image, concurrency or enabled flag. Enabling without plan + location fails 422 needs_plan."}, updateMicrovmConnector)
	Register(s, deps, Spec{Name: "user.microvm.connector.delete", Description: "Delete a connector; queued jobs stop dispatching. Irreversible. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovmConnector)
}
