package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// User script tools, mirroring the iaas_user_script resource. Full CRUD, all
// synchronous. There is no SHOW route, so get lists and filters by id (handled
// in the client).

func init() {
	toolRegistrars = append(toolRegistrars, registerUserScriptTools)
}

type CreateUserScriptInput struct {
	Name        string `json:"name" jsonschema:"name of the script"`
	Type        string `json:"type" jsonschema:"script type (e.g. bash, cloud-init)"`
	Content     string `json:"content" jsonschema:"the script body"`
	Description string `json:"description,omitempty" jsonschema:"optional description"`
	Shebang     string `json:"shebang,omitempty" jsonschema:"optional shebang line"`
}

type GetUserScriptInput struct {
	ID string `json:"id" jsonschema:"UUID of the user script"`
}

type ListUserScriptsInput struct{}

type UpdateUserScriptInput struct {
	ID          string  `json:"id" jsonschema:"UUID of the user script"`
	Name        *string `json:"name,omitempty" jsonschema:"new name"`
	Content     *string `json:"content,omitempty" jsonschema:"new content"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
	Shebang     *string `json:"shebang,omitempty" jsonschema:"new shebang"`
}

type DeleteUserScriptInput struct {
	ID string `json:"id" jsonschema:"UUID of the user script to delete"`
	Confirmation
}

type UserScriptResult struct {
	Script map[string]any `json:"script"`
}

type UserScriptListResult struct {
	Scripts []map[string]any `json:"scripts"`
	Count   int              `json:"count"`
}

func createUserScript(ctx context.Context, cl *client.Client, in CreateUserScriptInput) (UserScriptResult, error) {
	fields := map[string]any{"name": in.Name, "type": in.Type, "content": in.Content}
	if in.Description != "" {
		fields["description"] = in.Description
	}
	if in.Shebang != "" {
		fields["shebang"] = in.Shebang
	}
	obj, err := cl.CreateUserScript(ctx, fields)
	if err != nil {
		return UserScriptResult{}, err
	}
	return UserScriptResult{Script: obj}, nil
}

func getUserScript(ctx context.Context, cl *client.Client, in GetUserScriptInput) (UserScriptResult, error) {
	obj, err := cl.GetUserScript(ctx, in.ID)
	if err != nil {
		return UserScriptResult{}, err
	}
	return UserScriptResult{Script: obj}, nil
}

func listUserScripts(ctx context.Context, cl *client.Client, _ ListUserScriptsInput) (UserScriptListResult, error) {
	items, err := cl.ListUserScripts(ctx)
	if err != nil {
		return UserScriptListResult{}, err
	}
	return UserScriptListResult{Scripts: items, Count: len(items)}, nil
}

func updateUserScript(ctx context.Context, cl *client.Client, in UpdateUserScriptInput) (UserScriptResult, error) {
	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.Content != nil {
		fields["content"] = *in.Content
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.Shebang != nil {
		fields["shebang"] = *in.Shebang
	}
	obj, err := cl.UpdateUserScript(ctx, in.ID, fields)
	if err != nil {
		return UserScriptResult{}, err
	}
	return UserScriptResult{Script: obj}, nil
}

func deleteUserScript(ctx context.Context, cl *client.Client, in DeleteUserScriptInput) (DeleteResult, error) {
	if err := cl.DeleteUserScript(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerUserScriptTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.user_script.create", Description: "Create a user script (startup/cloud-init script)."}, createUserScript)
	Register(s, deps, Spec{Name: "user.user_script.list", Description: "List all user scripts owned by the caller."}, listUserScripts)
	Register(s, deps, Spec{Name: "user.user_script.get", Description: "Get a user script by UUID."}, getUserScript)
	Register(s, deps, Spec{Name: "user.user_script.update", Description: "Update a user script's fields."}, updateUserScript)
	Register(s, deps, Spec{
		Name:        "user.user_script.delete",
		Description: "Delete a user script. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteUserScript)
}
