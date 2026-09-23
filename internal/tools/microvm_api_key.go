package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM Sandboxes API key tools. A key authenticates the ACCOUNT OWNER -
// the E2B-compatible endpoints and this key store are shared. The plaintext
// secret is returned exactly once, at creation, and never again by list/get
// (list-side objects carry id/name/prefix/last_used_at only). Delete is
// confirm-gated: a revoked key starts failing every request immediately.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmApiKeyTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmApiKeysInput struct{}

type CreateMicrovmApiKeyInput struct {
	Name string `json:"name" jsonschema:"label identifying the key, max 100 characters"`
}

type DeleteMicrovmApiKeyInput struct {
	ID string `json:"id" jsonschema:"UUID of the API key to revoke"`
	Confirmation
}

type MicrovmApiKeyListResult struct {
	Keys  []map[string]any `json:"keys"`
	Count int              `json:"count"`
}

// MicrovmApiKeyCreateResult carries the created key (id/name/prefix) and the
// plaintext secret. Plaintext is shown ONLY here - it can never be retrieved
// again by any other tool.
type MicrovmApiKeyCreateResult struct {
	Key       map[string]any `json:"key"`
	Plaintext string         `json:"plaintext"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listMicrovmApiKeys(ctx context.Context, cl *client.Client, in ListMicrovmApiKeysInput) (MicrovmApiKeyListResult, error) {
	keys, err := cl.ListMicrovmApiKeys(ctx)
	if err != nil {
		return MicrovmApiKeyListResult{}, err
	}
	return MicrovmApiKeyListResult{Keys: keys, Count: len(keys)}, nil
}

func createMicrovmApiKey(ctx context.Context, cl *client.Client, in CreateMicrovmApiKeyInput) (MicrovmApiKeyCreateResult, error) {
	env, err := cl.CreateMicrovmApiKey(ctx, in.Name)
	if err != nil {
		return MicrovmApiKeyCreateResult{}, err
	}
	key, _ := env["key"].(map[string]any)
	plaintext, _ := env["plaintext"].(string)
	return MicrovmApiKeyCreateResult{Key: key, Plaintext: plaintext}, nil
}

func deleteMicrovmApiKey(ctx context.Context, cl *client.Client, in DeleteMicrovmApiKeyInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovmApiKey(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerMicrovmApiKeyTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.api_key.list", Description: "List the caller's microVM Sandboxes API keys (id/name/prefix/last_used_at; plaintext secrets are shown only once, at creation)."}, listMicrovmApiKeys)
	Register(s, deps, Spec{Name: "user.microvm.api_key.create", Description: "Create a microVM Sandboxes API key. The plaintext secret is returned exactly once in this response and can never be retrieved again - store it immediately."}, createMicrovmApiKey)
	Register(s, deps, Spec{Name: "user.microvm.api_key.delete", Description: "Revoke a microVM Sandboxes API key. Requests authenticated with it start failing immediately. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovmApiKey)
}
