package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Docker deployment tools, mirroring the iaas_docker_deployment resource. All
// paths are instance-scoped. Deploy is async: if the Docker engine is not yet
// installed it is installed first (wait until enabled), then the app/compose is
// deployed and polled to "running" (fail "error"/"failed"). Delete is
// synchronous. There is no logs/control/retry client method, so none is exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerDockerTools)
}

type DeployDockerAppInput struct {
	InstanceID   string           `json:"instance_id" jsonschema:"UUID of the instance"`
	AppSlug      string           `json:"app_slug" jsonschema:"catalog app slug to deploy"`
	EnvVariables map[string]any   `json:"env_variables,omitempty" jsonschema:"optional environment variables"`
	PortMappings []map[string]any `json:"port_mappings,omitempty" jsonschema:"optional port mappings ({container_port,host_port,protocol})"`
}

type DeployDockerComposeInput struct {
	InstanceID   string           `json:"instance_id" jsonschema:"UUID of the instance"`
	AppName      string           `json:"app_name" jsonschema:"name for the compose deployment"`
	ComposeURL   string           `json:"compose_url" jsonschema:"HTTPS URL of the compose file (fetched server-side)"`
	EnvVariables map[string]any   `json:"env_variables,omitempty" jsonschema:"optional environment variables"`
	PortMappings []map[string]any `json:"port_mappings,omitempty" jsonschema:"optional port mappings"`
}

type GetDockerDeploymentInput struct {
	InstanceID   string `json:"instance_id" jsonschema:"UUID of the instance"`
	DeploymentID string `json:"deployment_id" jsonschema:"UUID of the deployment"`
}

type ListDockerDeploymentsInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
}

type DeleteDockerDeploymentInput struct {
	InstanceID   string `json:"instance_id" jsonschema:"UUID of the instance"`
	DeploymentID string `json:"deployment_id" jsonschema:"UUID of the deployment to delete"`
	Confirmation
}

type DockerDeploymentResult struct {
	Deployment map[string]any `json:"deployment"`
}

type DockerDeploymentListResult struct {
	Deployments []map[string]any `json:"deployments"`
	Count       int              `json:"count"`
}

// ensureDockerEngine installs the Docker engine if it is not enabled yet and
// waits until it reports enabled. Mirrors the resource's install phase.
func ensureDockerEngine(ctx context.Context, cl *client.Client, instanceID string) error {
	enabled, err := cl.DockerEnabled(ctx, instanceID)
	if err != nil {
		return err
	}
	if enabled {
		return nil
	}
	if _, err := cl.InstallDockerEngine(ctx, instanceID); err != nil {
		return err
	}
	return waitForDockerEnabled(ctx, cl, instanceID, defaultCreateTimeout)
}

func deployDockerApp(ctx context.Context, cl *client.Client, in DeployDockerAppInput) (DockerDeploymentResult, error) {
	if err := ensureDockerEngine(ctx, cl, in.InstanceID); err != nil {
		return DockerDeploymentResult{}, err
	}
	fields := map[string]any{"app_slug": in.AppSlug}
	if in.EnvVariables != nil {
		fields["env_variables"] = in.EnvVariables
	}
	if in.PortMappings != nil {
		fields["port_mappings"] = in.PortMappings
	}
	created, err := cl.DeployDockerApp(ctx, in.InstanceID, fields)
	if err != nil {
		return DockerDeploymentResult{}, err
	}
	return finishDockerDeploy(ctx, cl, in.InstanceID, created)
}

func deployDockerCompose(ctx context.Context, cl *client.Client, in DeployDockerComposeInput) (DockerDeploymentResult, error) {
	if err := ensureDockerEngine(ctx, cl, in.InstanceID); err != nil {
		return DockerDeploymentResult{}, err
	}
	fields := map[string]any{"app_name": in.AppName, "compose_url": in.ComposeURL}
	if in.EnvVariables != nil {
		fields["env_variables"] = in.EnvVariables
	}
	if in.PortMappings != nil {
		fields["port_mappings"] = in.PortMappings
	}
	created, err := cl.DeployDockerCompose(ctx, in.InstanceID, fields)
	if err != nil {
		return DockerDeploymentResult{}, err
	}
	return finishDockerDeploy(ctx, cl, in.InstanceID, created)
}

// finishDockerDeploy waits for a freshly-created deployment to reach "running"
// then hydrates it.
func finishDockerDeploy(ctx context.Context, cl *client.Client, instanceID string, created map[string]any) (DockerDeploymentResult, error) {
	id, _ := created["id"].(string)
	if id == "" {
		return DockerDeploymentResult{}, fmt.Errorf("deploy response did not include a deployment id")
	}
	err := waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetDockerDeployment(ctx, instanceID, id) },
		"status", []string{"running"}, []string{"error", "failed"}, defaultCreateTimeout)
	if err != nil {
		return DockerDeploymentResult{}, fmt.Errorf("docker deployment %s did not reach running: %w", id, err)
	}
	obj, err := cl.GetDockerDeployment(ctx, instanceID, id)
	if err != nil {
		return DockerDeploymentResult{}, err
	}
	return DockerDeploymentResult{Deployment: obj}, nil
}

func getDockerDeployment(ctx context.Context, cl *client.Client, in GetDockerDeploymentInput) (DockerDeploymentResult, error) {
	obj, err := cl.GetDockerDeployment(ctx, in.InstanceID, in.DeploymentID)
	if err != nil {
		return DockerDeploymentResult{}, err
	}
	return DockerDeploymentResult{Deployment: obj}, nil
}

func listDockerDeployments(ctx context.Context, cl *client.Client, in ListDockerDeploymentsInput) (DockerDeploymentListResult, error) {
	items, err := cl.ListDockerDeployments(ctx, in.InstanceID)
	if err != nil {
		return DockerDeploymentListResult{}, err
	}
	return DockerDeploymentListResult{Deployments: items, Count: len(items)}, nil
}

func deleteDockerDeployment(ctx context.Context, cl *client.Client, in DeleteDockerDeploymentInput) (DeleteResult, error) {
	if err := cl.DeleteDockerDeployment(ctx, in.InstanceID, in.DeploymentID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.DeploymentID, Deleted: true}, nil
}

func registerDockerTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.docker_deployment.deploy_app",
		Description: "Deploy a catalog Docker app on an instance (installs the engine if needed) and wait until running.",
	}, deployDockerApp)
	Register(s, deps, Spec{
		Name:        "user.docker_deployment.deploy_compose",
		Description: "Deploy a Docker Compose stack (from an HTTPS URL) on an instance and wait until running.",
	}, deployDockerCompose)
	Register(s, deps, Spec{Name: "user.docker_deployment.list", Description: "List Docker deployments on an instance."}, listDockerDeployments)
	Register(s, deps, Spec{Name: "user.docker_deployment.get", Description: "Get a Docker deployment by instance and deployment UUID."}, getDockerDeployment)
	Register(s, deps, Spec{
		Name:        "user.docker_deployment.delete",
		Description: "Delete a Docker deployment. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteDockerDeployment)
}
