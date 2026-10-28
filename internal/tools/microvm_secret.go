package tools

import (
	"context"
	"fmt"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { toolRegistrars = append(toolRegistrars, registerMicrovmSecretTools) }

type ListMicrovmSecretsInput struct {
	Kind string `json:"kind,omitempty" jsonschema:"env (default), registry, or all"`
}
type CreateMicrovmSecretInput struct {
	Name     string  `json:"name" jsonschema:"Unique account environment variable name, uppercase letters digits underscores, max 100"`
	Value    string  `json:"value" jsonschema:"Write-only value, 1 to 8192 characters. Never returned. Whitespace is significant"`
	Kind     string  `json:"kind,omitempty" jsonschema:"env (default) or registry"`
	Registry *string `json:"registry,omitempty" jsonschema:"Registry host with optional port, required for registry credentials only"`
	Username *string `json:"username,omitempty" jsonschema:"Registry username, required for registry credentials only"`
}
type UpdateMicrovmSecretInput struct {
	ID       string  `json:"id" jsonschema:"Secret UUID"`
	Name     *string `json:"name,omitempty" jsonschema:"Replacement environment variable name"`
	Value    *string `json:"value,omitempty" jsonschema:"New write-only value. Omit to keep existing; empty is refused"`
	Kind     *string `json:"kind,omitempty" jsonschema:"Immutable kind, env or registry; a different kind is refused"`
	Registry *string `json:"registry,omitempty" jsonschema:"New registry host, refused while a kit references this credential"`
	Username *string `json:"username,omitempty" jsonschema:"Replacement registry username"`
}
type DeleteMicrovmSecretInput struct {
	ID string `json:"id" jsonschema:"Secret UUID"`
	Confirmation
}
type MicrovmSecretResult struct {
	Secret map[string]any `json:"secret"`
}
type MicrovmSecretListResult struct {
	Secrets []map[string]any `json:"secrets"`
	Count   int              `json:"count"`
}

func listMicrovmSecrets(ctx context.Context, cl *client.Client, in ListMicrovmSecretsInput) (MicrovmSecretListResult, error) {
	rows, err := cl.ListMicrovmSecrets(ctx, in.Kind)
	return MicrovmSecretListResult{Secrets: rows, Count: len(rows)}, err
}
func createMicrovmSecret(ctx context.Context, cl *client.Client, in CreateMicrovmSecretInput) (MicrovmSecretResult, error) {
	body := map[string]any{"name": in.Name, "value": in.Value}
	if in.Kind != "" {
		body["kind"] = in.Kind
	}
	if in.Registry != nil {
		body["registry"] = *in.Registry
	}
	if in.Username != nil {
		body["username"] = *in.Username
	}
	row, err := cl.CreateMicrovmSecret(ctx, body)
	return MicrovmSecretResult{Secret: row}, err
}
func updateMicrovmSecret(ctx context.Context, cl *client.Client, in UpdateMicrovmSecretInput) (MicrovmSecretResult, error) {
	body := map[string]any{}
	for k, v := range map[string]*string{"name": in.Name, "value": in.Value, "kind": in.Kind, "registry": in.Registry, "username": in.Username} {
		if v != nil {
			body[k] = *v
		}
	}
	if len(body) == 0 {
		return MicrovmSecretResult{}, fmt.Errorf("supply at least one field to update")
	}
	row, err := cl.UpdateMicrovmSecret(ctx, in.ID, body)
	return MicrovmSecretResult{Secret: row}, err
}
func deleteMicrovmSecret(ctx context.Context, cl *client.Client, in DeleteMicrovmSecretInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovmSecret(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}
func registerMicrovmSecretTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm_secret.list", Description: "List sandbox secret metadata. Defaults to env; choose registry or all explicitly. Secret values are never returned."}, listMicrovmSecrets)
	Register(s, deps, Spec{Name: "user.microvm_secret.create", Description: "Create an account environment secret or registry credential. Values are write-only and never returned. Registry kind requires registry and username."}, createMicrovmSecret)
	Register(s, deps, Spec{Name: "user.microvm_secret.update", Description: "Rename or rotate a secret. Omitted value keeps the old value. Existing sandboxes retain their copy. Kind is immutable and registry changes are refused while a kit uses it."}, updateMicrovmSecret)
	Register(s, deps, Spec{Name: "user.microvm_secret.delete", Description: "Delete a saved secret. Existing sandboxes retain their copy. Requires confirm true.", Destructive: true}, deleteMicrovmSecret)
}
