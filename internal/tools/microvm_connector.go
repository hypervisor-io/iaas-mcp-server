package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM connector tools (unified surface, MVU2-3). A connector is a CI
// integration (github_app | gitlab_runner) that dispatches on-demand
// microVMs as self-hosted runners for incoming jobs. list also returns the
// create-context catalog (plans/hypervisor_groups/images/git_sources) and
// whether the platform's GitHub App is configured, so an agent can build a
// valid create call without a second round trip. The connector's stored
// credentials (installation ids, GitLab tokens) are encrypted at rest and
// never serialized by any endpoint. delete is confirm-gated.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmConnectorTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmConnectorsInput struct{}

type ListMicrovmConnectorJobsInput struct{}

// CreateGithubMicrovmConnectorInput creates a github_app connector from an
// existing GitHub App git source (see GitSource) that already has an
// installation_id.
type CreateGithubMicrovmConnectorInput struct {
	GitSourceID   string   `json:"git_source_id" jsonschema:"UUID of a github_app-provider git source owned by the account"`
	Name          string   `json:"name" jsonschema:"connector name, max 100 characters"`
	Labels        []string `json:"labels,omitempty" jsonschema:"runner labels applied to every dispatched runner"`
	LocationID    string   `json:"location_id,omitempty" jsonschema:"the location runners deploy into"`
	PlanID        string   `json:"plan_id,omitempty" jsonschema:"a plan available in location_id"`
	ImageID       string   `json:"image_id,omitempty" jsonschema:"a ready image visible to the account"`
	MaxConcurrent int      `json:"max_concurrent,omitempty" jsonschema:"maximum concurrent runners (1-20)"`
	WarmCount     int      `json:"warm_count,omitempty" jsonschema:"runners kept warm and idle (0-5)"`
}

// CreateGitlabMicrovmConnectorInput registers a GitLab runner and creates its
// gitlab_runner connector from a runner registration token (glrt-...). The
// token is stored encrypted and never returned by any endpoint.
type CreateGitlabMicrovmConnectorInput struct {
	Name              string   `json:"name" jsonschema:"connector name, max 100 characters"`
	GitlabURL         string   `json:"gitlab_url" jsonschema:"the GitLab instance URL"`
	GitlabToken       string   `json:"gitlab_token" jsonschema:"a GitLab runner registration token, starting glrt-; stored encrypted and never returned"`
	GitlabTagList     []string `json:"gitlab_tag_list,omitempty" jsonschema:"GitLab runner tags"`
	GitlabRunUntagged bool     `json:"gitlab_run_untagged,omitempty" jsonschema:"whether the runner picks up untagged jobs"`
	LocationID        string   `json:"location_id,omitempty" jsonschema:"the location runners deploy into"`
	PlanID            string   `json:"plan_id,omitempty" jsonschema:"a plan available in location_id"`
	ImageID           string   `json:"image_id,omitempty" jsonschema:"a ready image visible to the account"`
	MaxConcurrent     int      `json:"max_concurrent,omitempty" jsonschema:"maximum concurrent runners (1-20)"`
	WarmCount         int      `json:"warm_count,omitempty" jsonschema:"runners kept warm and idle (0-5)"`
}

// UpdateMicrovmConnectorInput updates a connector's config. Setting
// enabled:true requires the connector to already have (or this request to
// supply) both a plan and a location - the API refuses with 422 needs_plan
// otherwise.
type UpdateMicrovmConnectorInput struct {
	ID            string   `json:"id" jsonschema:"UUID of the connector"`
	Name          string   `json:"name,omitempty" jsonschema:"connector name"`
	Labels        []string `json:"labels,omitempty" jsonschema:"runner labels/tags"`
	LocationID    string   `json:"location_id,omitempty" jsonschema:"the location runners deploy into"`
	PlanID        string   `json:"plan_id,omitempty" jsonschema:"a plan available in location_id"`
	ImageID       string   `json:"image_id,omitempty" jsonschema:"a ready image visible to the account"`
	MaxConcurrent int      `json:"max_concurrent,omitempty" jsonschema:"maximum concurrent runners (1-20)"`
	WarmCount     int      `json:"warm_count,omitempty" jsonschema:"runners kept warm and idle (0-5)"`
	Enabled       *bool    `json:"enabled,omitempty" jsonschema:"whether the connector is active; requires an existing or supplied plan and location"`
}

type DeleteMicrovmConnectorInput struct {
	ID string `json:"id" jsonschema:"UUID of the connector to delete"`
	Confirmation
}

// MicrovmConnectorListResult is the flattened INDEX envelope: the
// connectors plus the create-context catalog the API returns alongside them.
type MicrovmConnectorListResult struct {
	Connectors          []map[string]any `json:"connectors"`
	Count               int              `json:"count"`
	Plans               []map[string]any `json:"plans"`
	HypervisorGroups    []map[string]any `json:"hypervisor_groups"`
	Images              []map[string]any `json:"images"`
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

func listMicrovmConnectors(ctx context.Context, cl *client.Client, in ListMicrovmConnectorsInput) (MicrovmConnectorListResult, error) {
	env, err := cl.ListMicrovmConnectors(ctx)
	if err != nil {
		return MicrovmConnectorListResult{}, err
	}
	pag, _ := env["connectors"].(map[string]any)
	connectors := asObjectList(pag["data"])
	configured, _ := env["github_app_configured"].(bool)
	return MicrovmConnectorListResult{
		Connectors:          connectors,
		Count:               len(connectors),
		Plans:               asObjectList(env["plans"]),
		HypervisorGroups:    asObjectList(env["hypervisor_groups"]),
		Images:              asObjectList(env["images"]),
		GitSources:          asObjectList(env["git_sources"]),
		GithubAppConfigured: configured,
	}, nil
}

func listMicrovmConnectorJobs(ctx context.Context, cl *client.Client, in ListMicrovmConnectorJobsInput) (MicrovmConnectorJobListResult, error) {
	jobs, err := cl.ListMicrovmConnectorJobs(ctx)
	if err != nil {
		return MicrovmConnectorJobListResult{}, err
	}
	return MicrovmConnectorJobListResult{Jobs: jobs, Count: len(jobs)}, nil
}

func createGithubConnectorBody(in CreateGithubMicrovmConnectorInput) map[string]any {
	body := map[string]any{
		"git_source_id": in.GitSourceID,
		"name":          in.Name,
	}
	if len(in.Labels) > 0 {
		body["labels"] = in.Labels
	}
	if in.LocationID != "" {
		body["location_id"] = in.LocationID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.MaxConcurrent != 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	if in.WarmCount != 0 {
		body["warm_count"] = in.WarmCount
	}
	return body
}

func createGithubMicrovmConnector(ctx context.Context, cl *client.Client, in CreateGithubMicrovmConnectorInput) (MicrovmConnectorResult, error) {
	connector, err := cl.CreateGithubMicrovmConnector(ctx, createGithubConnectorBody(in))
	if err != nil {
		return MicrovmConnectorResult{}, err
	}
	return MicrovmConnectorResult{Connector: connector}, nil
}

func createGitlabConnectorBody(in CreateGitlabMicrovmConnectorInput) map[string]any {
	body := map[string]any{
		"name":         in.Name,
		"gitlab_url":   in.GitlabURL,
		"gitlab_token": in.GitlabToken,
	}
	if len(in.GitlabTagList) > 0 {
		body["gitlab_tag_list"] = in.GitlabTagList
	}
	if in.GitlabRunUntagged {
		body["gitlab_run_untagged"] = in.GitlabRunUntagged
	}
	if in.LocationID != "" {
		body["location_id"] = in.LocationID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.MaxConcurrent != 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	if in.WarmCount != 0 {
		body["warm_count"] = in.WarmCount
	}
	return body
}

func createGitlabMicrovmConnector(ctx context.Context, cl *client.Client, in CreateGitlabMicrovmConnectorInput) (MicrovmConnectorResult, error) {
	connector, err := cl.CreateGitlabMicrovmConnector(ctx, createGitlabConnectorBody(in))
	if err != nil {
		return MicrovmConnectorResult{}, err
	}
	return MicrovmConnectorResult{Connector: connector}, nil
}

func updateMicrovmConnector(ctx context.Context, cl *client.Client, in UpdateMicrovmConnectorInput) (MicrovmConnectorResult, error) {
	body := map[string]any{}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.LocationID != "" {
		body["location_id"] = in.LocationID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.MaxConcurrent != 0 {
		body["max_concurrent"] = in.MaxConcurrent
	}
	if in.WarmCount != 0 {
		body["warm_count"] = in.WarmCount
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
	Register(s, deps, Spec{Name: "user.microvm.connector.list", Description: "List the account's CI runner connectors, plus the location/plan/image/git-source catalog needed to create or edit one."}, listMicrovmConnectors)
	Register(s, deps, Spec{Name: "user.microvm.connector.jobs", Description: "List the CI run history across the account's connectors, newest first."}, listMicrovmConnectorJobs)
	Register(s, deps, Spec{Name: "user.microvm.connector.create_github", Description: "Create a self-hosted GitHub Actions runner connector from a connected GitHub App git source. Warm runners are dispatched as on-demand microVMs."}, createGithubMicrovmConnector)
	Register(s, deps, Spec{Name: "user.microvm.connector.create_gitlab", Description: "Create a self-hosted GitLab Runner connector authenticated with a GitLab runner registration token. Warm runners are dispatched as on-demand microVMs."}, createGitlabMicrovmConnector)
	Register(s, deps, Spec{Name: "user.microvm.connector.update", Description: "Update a connector's settings. Setting enabled:true requires the connector to already have (or this call to supply) both a plan and a location - refused with 422 needs_plan otherwise."}, updateMicrovmConnector)
	Register(s, deps, Spec{Name: "user.microvm.connector.delete", Description: "Delete a connector and its warm runners. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovmConnector)
}
