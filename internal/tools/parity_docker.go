package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Docker deployment control tools: install the engine, retry, check status, and
// start/stop/restart a deployment.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityDockerTools)
}

type DockerInstallInput struct {
	InstanceID string `json:"instance_id" jsonschema:"UUID of the instance"`
}

type DockerDeploymentRefInput struct {
	InstanceID   string `json:"instance_id" jsonschema:"UUID of the instance"`
	DeploymentID string `json:"deployment_id" jsonschema:"UUID of the docker deployment"`
}

type DockerControlInput struct {
	InstanceID   string `json:"instance_id" jsonschema:"UUID of the instance"`
	DeploymentID string `json:"deployment_id" jsonschema:"UUID of the docker deployment"`
	Action       string `json:"action" jsonschema:"start, stop, or restart"`
}

func installDockerEngine(ctx context.Context, cl *client.Client, in DockerInstallInput) (OKResult, error) {
	if _, err := cl.InstallDockerEngine(ctx, in.InstanceID); err != nil {
		return OKResult{}, err
	}
	if err := waitForDockerEnabled(ctx, cl, in.InstanceID, defaultCreateTimeout); err != nil {
		return OKResult{}, err
	}
	return okResult("docker engine installed"), nil
}

func retryDockerDeployment(ctx context.Context, cl *client.Client, in DockerDeploymentRefInput) (ObjectResult, error) {
	return objectResult(cl.RetryDockerDeployment(ctx, in.InstanceID, in.DeploymentID))
}

func checkDockerDeploymentStatus(ctx context.Context, cl *client.Client, in DockerDeploymentRefInput) (ObjectResult, error) {
	return objectResult(cl.CheckDockerDeploymentStatus(ctx, in.InstanceID, in.DeploymentID))
}

func controlDockerDeployment(ctx context.Context, cl *client.Client, in DockerControlInput) (ObjectResult, error) {
	return objectResult(cl.ControlDockerDeployment(ctx, in.InstanceID, in.DeploymentID, in.Action))
}

func registerParityDockerTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.docker_deployment.install", Description: "Install the Docker engine on an instance and wait until enabled."}, installDockerEngine)
	Register(s, deps, Spec{Name: "user.docker_deployment.retry", Description: "Retry a failed Docker deployment."}, retryDockerDeployment)
	Register(s, deps, Spec{Name: "user.docker_deployment.check_status", Description: "Refresh a Docker deployment's status from the host."}, checkDockerDeploymentStatus)
	Register(s, deps, Spec{Name: "user.docker_deployment.control", Description: "Start, stop, or restart a Docker deployment."}, controlDockerDeployment)
}
