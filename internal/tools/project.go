package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Project tools, mirroring the iaas_project and iaas_project_assignment
// resources. Projects group resources; a resource is assigned to a project via
// a single endpoint that also unassigns (project_id null). Valid resource types
// are instance, vpc, load_balancer, s3_bucket, managed_database.

func init() {
	toolRegistrars = append(toolRegistrars, registerProjectTools)
}

type CreateProjectInput struct {
	Name        string `json:"name" jsonschema:"project name (max 64 chars)"`
	Description string `json:"description,omitempty" jsonschema:"optional description"`
	Color       string `json:"color,omitempty" jsonschema:"optional hex color #RRGGBB"`
}

type GetProjectInput struct {
	ID string `json:"id" jsonschema:"UUID of the project"`
}

type ListProjectsInput struct{}

type UpdateProjectInput struct {
	ID          string `json:"id" jsonschema:"UUID of the project"`
	Name        string `json:"name" jsonschema:"project name (required by the API even on update)"`
	Description string `json:"description,omitempty" jsonschema:"new description"`
	Color       string `json:"color,omitempty" jsonschema:"new hex color #RRGGBB"`
}

type DeleteProjectInput struct {
	ID string `json:"id" jsonschema:"UUID of the project to delete"`
	Confirmation
}

// AssignResourceInput assigns a resource to a project. resource_type must be one
// of instance, vpc, load_balancer, s3_bucket, managed_database.
type AssignResourceInput struct {
	ResourceType string `json:"resource_type" jsonschema:"instance, vpc, load_balancer, s3_bucket, or managed_database"`
	ResourceID   string `json:"resource_id" jsonschema:"UUID of the resource"`
	ProjectID    string `json:"project_id" jsonschema:"UUID of the project to assign into"`
}

type UnassignResourceInput struct {
	ResourceType string `json:"resource_type" jsonschema:"instance, vpc, load_balancer, s3_bucket, or managed_database"`
	ResourceID   string `json:"resource_id" jsonschema:"UUID of the resource to unassign"`
}

type ProjectResult struct {
	Project map[string]any `json:"project"`
}

type ProjectListResult struct {
	Projects []map[string]any `json:"projects"`
	Count    int              `json:"count"`
}

func createProject(ctx context.Context, cl *client.Client, in CreateProjectInput) (ProjectResult, error) {
	body := map[string]any{"name": in.Name}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Color != "" {
		body["color"] = in.Color
	}
	obj, err := cl.CreateProject(ctx, body)
	if err != nil {
		return ProjectResult{}, err
	}
	return ProjectResult{Project: obj}, nil
}

func getProject(ctx context.Context, cl *client.Client, in GetProjectInput) (ProjectResult, error) {
	obj, err := cl.GetProject(ctx, in.ID)
	if err != nil {
		return ProjectResult{}, err
	}
	return ProjectResult{Project: obj}, nil
}

func listProjects(ctx context.Context, cl *client.Client, _ ListProjectsInput) (ProjectListResult, error) {
	items, err := cl.ListProjects(ctx)
	if err != nil {
		return ProjectListResult{}, err
	}
	return ProjectListResult{Projects: items, Count: len(items)}, nil
}

func updateProject(ctx context.Context, cl *client.Client, in UpdateProjectInput) (ProjectResult, error) {
	body := map[string]any{"name": in.Name}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.Color != "" {
		body["color"] = in.Color
	}
	obj, err := cl.UpdateProject(ctx, in.ID, body)
	if err != nil {
		return ProjectResult{}, err
	}
	return ProjectResult{Project: obj}, nil
}

func deleteProject(ctx context.Context, cl *client.Client, in DeleteProjectInput) (DeleteResult, error) {
	if err := cl.DeleteProject(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func assignResource(ctx context.Context, cl *client.Client, in AssignResourceInput) (OKResult, error) {
	if err := cl.AssignResourceToProject(ctx, in.ResourceType, in.ResourceID, in.ProjectID); err != nil {
		return OKResult{}, err
	}
	return okResult("resource assigned to project"), nil
}

func unassignResource(ctx context.Context, cl *client.Client, in UnassignResourceInput) (OKResult, error) {
	// An empty project id unassigns (the endpoint sends project_id:null).
	if err := cl.AssignResourceToProject(ctx, in.ResourceType, in.ResourceID, ""); err != nil {
		return OKResult{}, err
	}
	return okResult("resource unassigned from project"), nil
}

func registerProjectTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.project.create", Description: "Create a project (a group for organizing resources)."}, createProject)
	Register(s, deps, Spec{Name: "user.project.list", Description: "List all projects owned by the caller."}, listProjects)
	Register(s, deps, Spec{Name: "user.project.get", Description: "Get a project by UUID."}, getProject)
	Register(s, deps, Spec{Name: "user.project.update", Description: "Update a project's name, description, or color."}, updateProject)
	Register(s, deps, Spec{
		Name:        "user.project.delete",
		Description: "Delete a project. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteProject)
	Register(s, deps, Spec{
		Name:        "user.project.assign_resource",
		Description: "Assign a resource (instance, vpc, load_balancer, s3_bucket, managed_database) to a project.",
	}, assignResource)
	Register(s, deps, Spec{
		Name:        "user.project.unassign_resource",
		Description: "Unassign a resource from its project.",
	}, unassignResource)
}
