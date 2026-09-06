package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Serverless App tools, mirroring the MV2-25 user_api endpoints. An app is a
// scale-to-zero MicroVM deployed from a container image (oci) or a git
// repository, reachable at its default <slug>.apps.<domain> URL.
// delete is confirm-gated (destructive: it destroys the running VM).

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmAppTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmAppsInput struct{}

type GetMicrovmAppInput struct {
	ID string `json:"id" jsonschema:"UUID of the app"`
}

type CreateMicrovmAppInput struct {
	HypervisorGroupID  string `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (location) to place the app in"`
	Slug               string `json:"slug" jsonschema:"app name, used in its default <slug>.apps.<domain> URL"`
	SourceKind         string `json:"source_kind" jsonschema:"oci or git"`
	SourceImage        string `json:"source_image,omitempty" jsonschema:"container image reference, required when source_kind is oci"`
	SourceRepo         string `json:"source_repo,omitempty" jsonschema:"git repository URL, required when source_kind is git"`
	SourceBranch       string `json:"source_branch,omitempty" jsonschema:"git branch, defaults to main"`
	Port               int    `json:"port,omitempty" jsonschema:"container port, defaults to 8080"`
	MinInstances       int    `json:"min_instances,omitempty" jsonschema:"0 (scale to zero, default) or 1 (never pause)"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds,omitempty" jsonschema:"seconds of inactivity before pausing, defaults to 300"`
	HealthPath         string `json:"health_path,omitempty" jsonschema:"HTTP path for the deploy health check, defaults to /"`
}

type DeployMicrovmAppInput struct {
	ID         string `json:"id" jsonschema:"UUID of the app"`
	RevisionID string `json:"revision_id" jsonschema:"UUID of the built revision to deploy"`
}

type RollbackMicrovmAppInput struct {
	ID         string `json:"id" jsonschema:"UUID of the app"`
	RevisionID string `json:"revision_id" jsonschema:"UUID of a previously deployed revision to roll back to"`
}

type StopMicrovmAppInput struct {
	ID string `json:"id" jsonschema:"UUID of the app"`
}

type StartMicrovmAppInput struct {
	ID string `json:"id" jsonschema:"UUID of the app"`
}

type SetMicrovmAppEnvInput struct {
	ID  string            `json:"id" jsonschema:"UUID of the app"`
	Env map[string]string `json:"env" jsonschema:"full replacement set of environment variables (missing keys are removed)"`
}

// DeleteMicrovmAppInput permanently destroys an app. Confirm-gated
// (destructive) - the running VM and its DNS record are gone for good.
type DeleteMicrovmAppInput struct {
	ID string `json:"id" jsonschema:"UUID of the app to delete"`
	Confirmation
}

type MicrovmAppResult struct {
	App map[string]any `json:"app"`
}

type MicrovmAppListResult struct {
	Apps  []map[string]any `json:"apps"`
	Count int              `json:"count"`
}

type MicrovmAppActionResult struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listMicrovmApp(ctx context.Context, cl *client.Client, _ ListMicrovmAppsInput) (MicrovmAppListResult, error) {
	items, err := cl.ListApps(ctx)
	if err != nil {
		return MicrovmAppListResult{}, err
	}
	return MicrovmAppListResult{Apps: items, Count: len(items)}, nil
}

func getMicrovmApp(ctx context.Context, cl *client.Client, in GetMicrovmAppInput) (MicrovmAppResult, error) {
	app, err := cl.GetApp(ctx, in.ID)
	if err != nil {
		return MicrovmAppResult{}, err
	}
	return MicrovmAppResult{App: app}, nil
}

// createMicrovmApp maps the flat source_kind/source_image/source_repo inputs
// onto the nested "source" object the API expects: {image} for oci,
// {repo,branch} for git.
func createMicrovmApp(ctx context.Context, cl *client.Client, in CreateMicrovmAppInput) (MicrovmAppResult, error) {
	source := map[string]any{}
	if in.SourceKind == "oci" {
		source["image"] = in.SourceImage
	} else {
		source["repo"] = in.SourceRepo
		source["branch"] = in.SourceBranch
	}

	app, err := cl.CreateApp(ctx, map[string]any{
		"hypervisor_group_id":  in.HypervisorGroupID,
		"slug":                 in.Slug,
		"source_kind":          in.SourceKind,
		"source":               source,
		"port":                 in.Port,
		"min_instances":        in.MinInstances,
		"idle_timeout_seconds": in.IdleTimeoutSeconds,
		"health_path":          in.HealthPath,
	})
	if err != nil {
		return MicrovmAppResult{}, err
	}
	return MicrovmAppResult{App: app}, nil
}

func deployMicrovmApp(ctx context.Context, cl *client.Client, in DeployMicrovmAppInput) (MicrovmAppActionResult, error) {
	if err := cl.DeployAppRevision(ctx, in.ID, in.RevisionID); err != nil {
		return MicrovmAppActionResult{}, err
	}
	return MicrovmAppActionResult{ID: in.ID, Done: true}, nil
}

func rollbackMicrovmApp(ctx context.Context, cl *client.Client, in RollbackMicrovmAppInput) (MicrovmAppActionResult, error) {
	if err := cl.RollbackAppRevision(ctx, in.ID, in.RevisionID); err != nil {
		return MicrovmAppActionResult{}, err
	}
	return MicrovmAppActionResult{ID: in.ID, Done: true}, nil
}

func stopMicrovmApp(ctx context.Context, cl *client.Client, in StopMicrovmAppInput) (MicrovmAppActionResult, error) {
	if err := cl.StopApp(ctx, in.ID); err != nil {
		return MicrovmAppActionResult{}, err
	}
	return MicrovmAppActionResult{ID: in.ID, Done: true}, nil
}

func startMicrovmApp(ctx context.Context, cl *client.Client, in StartMicrovmAppInput) (MicrovmAppActionResult, error) {
	if err := cl.StartApp(ctx, in.ID); err != nil {
		return MicrovmAppActionResult{}, err
	}
	return MicrovmAppActionResult{ID: in.ID, Done: true}, nil
}

func setMicrovmAppEnv(ctx context.Context, cl *client.Client, in SetMicrovmAppEnvInput) (MicrovmAppActionResult, error) {
	if err := cl.SetAppEnv(ctx, in.ID, in.Env); err != nil {
		return MicrovmAppActionResult{}, err
	}
	return MicrovmAppActionResult{ID: in.ID, Done: true}, nil
}

func deleteMicrovmApp(ctx context.Context, cl *client.Client, in DeleteMicrovmAppInput) (MicrovmAppActionResult, error) {
	// No manual confirm check here - registerMicrovmAppTools calls Register
	// with Spec{Destructive: true} for this handler, and the framework
	// (framework.go's Register) refuses the call before this function ever
	// runs unless the input carried confirm:true. Confirmation is only
	// embedded on DeleteMicrovmAppInput so its field shows up in the
	// generated JSON schema.
	if err := cl.DeleteApp(ctx, in.ID); err != nil {
		return MicrovmAppActionResult{}, err
	}
	return MicrovmAppActionResult{ID: in.ID, Done: true}, nil
}

func registerMicrovmAppTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.microvm.app.list",
		Description: "List all Serverless Apps owned by the caller.",
	}, listMicrovmApp)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.get",
		Description: "Get a Serverless App by UUID, including its revisions.",
	}, getMicrovmApp)

	Register(s, deps, Spec{
		Name: "user.microvm.app.create",
		Description: "Create a Serverless App from a container image (source_kind=oci) or a git " +
			"repository (source_kind=git), and dispatch its first build.",
	}, createMicrovmApp)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.deploy",
		Description: "Deploy a built revision to the app (blue/green: health-checked before traffic switches).",
	}, deployMicrovmApp)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.rollback",
		Description: "Roll back the app to a previously deployed revision.",
	}, rollbackMicrovmApp)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.stop",
		Description: "Stop the app's running instance. Its URL and DNS record are kept.",
	}, stopMicrovmApp)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.start",
		Description: "Redeploy the app's current revision after a stop.",
	}, startMicrovmApp)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.set_env",
		Description: "Replace the app's environment variables. Missing keys are removed (full sync, not a merge).",
	}, setMicrovmAppEnv)

	Register(s, deps, Spec{
		Name:        "user.microvm.app.delete",
		Description: "Delete a Serverless App and destroy its running VM. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteMicrovmApp)
}
