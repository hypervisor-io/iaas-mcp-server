package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// SSH key tools, mirroring the iaas_ssh_key resource. Full CRUD, all
// synchronous. public_key is immutable (only name/comments update).

func init() {
	toolRegistrars = append(toolRegistrars, registerSSHKeyTools)
}

type CreateSSHKeyInput struct {
	Name      string `json:"name" jsonschema:"name of the SSH key"`
	PublicKey string `json:"public_key" jsonschema:"the OpenSSH public key material"`
}

type GetSSHKeyInput struct {
	ID string `json:"id" jsonschema:"UUID of the SSH key"`
}

type ListSSHKeysInput struct{}

type UpdateSSHKeyInput struct {
	ID       string  `json:"id" jsonschema:"UUID of the SSH key"`
	Name     *string `json:"name,omitempty" jsonschema:"new name"`
	Comments *string `json:"comments,omitempty" jsonschema:"new comments"`
}

type DeleteSSHKeyInput struct {
	ID string `json:"id" jsonschema:"UUID of the SSH key to delete"`
	Confirmation
}

type SSHKeyResult struct {
	SSHKey map[string]any `json:"ssh_key"`
}

type SSHKeyListResult struct {
	SSHKeys []map[string]any `json:"ssh_keys"`
	Count   int              `json:"count"`
}

func createSSHKey(ctx context.Context, cl *client.Client, in CreateSSHKeyInput) (SSHKeyResult, error) {
	obj, err := cl.CreateSSHKey(ctx, in.Name, in.PublicKey)
	if err != nil {
		return SSHKeyResult{}, err
	}
	return SSHKeyResult{SSHKey: obj}, nil
}

func getSSHKey(ctx context.Context, cl *client.Client, in GetSSHKeyInput) (SSHKeyResult, error) {
	obj, err := cl.GetSSHKey(ctx, in.ID)
	if err != nil {
		return SSHKeyResult{}, err
	}
	return SSHKeyResult{SSHKey: obj}, nil
}

func listSSHKeys(ctx context.Context, cl *client.Client, _ ListSSHKeysInput) (SSHKeyListResult, error) {
	items, err := cl.ListSSHKeys(ctx)
	if err != nil {
		return SSHKeyListResult{}, err
	}
	return SSHKeyListResult{SSHKeys: items, Count: len(items)}, nil
}

func updateSSHKey(ctx context.Context, cl *client.Client, in UpdateSSHKeyInput) (SSHKeyResult, error) {
	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.Comments != nil {
		fields["comments"] = *in.Comments
	}
	obj, err := cl.UpdateSSHKey(ctx, in.ID, fields)
	if err != nil {
		return SSHKeyResult{}, err
	}
	return SSHKeyResult{SSHKey: obj}, nil
}

func deleteSSHKey(ctx context.Context, cl *client.Client, in DeleteSSHKeyInput) (DeleteResult, error) {
	if err := cl.DeleteSSHKey(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerSSHKeyTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.ssh_key.create", Description: "Register an SSH public key."}, createSSHKey)
	Register(s, deps, Spec{Name: "user.ssh_key.list", Description: "List all SSH keys owned by the caller."}, listSSHKeys)
	Register(s, deps, Spec{Name: "user.ssh_key.get", Description: "Get an SSH key by UUID."}, getSSHKey)
	Register(s, deps, Spec{Name: "user.ssh_key.update", Description: "Update an SSH key's name or comments."}, updateSSHKey)
	Register(s, deps, Spec{
		Name:        "user.ssh_key.delete",
		Description: "Delete an SSH key. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteSSHKey)
}
