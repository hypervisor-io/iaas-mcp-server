package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM Sandboxes API key tools, mirroring the MV1-26 user_api endpoints.
// The plaintext key is shown exactly once, by create; the listing never
// carries key material. delete is confirm-gated (destructive).

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmApiKeyTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmApiKeysInput struct{}

type CreateMicrovmApiKeyInput struct {
	Name string `json:"name" jsonschema:"display name for the key"`
}

// DeleteMicrovmApiKeyInput revokes an API key. Confirm-gated (destructive) -
// agents authenticating with the revoked key lose access immediately.
type DeleteMicrovmApiKeyInput struct {
	ID string `json:"id" jsonschema:"UUID of the API key to revoke"`
	Confirmation
}

type MicrovmApiKeyListResult struct {
	Keys  []map[string]any `json:"keys"`
	Count int              `json:"count"`
}

type MicrovmApiKeyResult struct {
	Key       map[string]any `json:"key"`
	Plaintext string         `json:"plaintext,omitempty"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listMicrovmApiKeys(ctx context.Context, cl *client.Client, _ ListMicrovmApiKeysInput) (MicrovmApiKeyListResult, error) {
	items, err := cl.ListMicrovmApiKeys(ctx)
	if err != nil {
		return MicrovmApiKeyListResult{}, err
	}
	return MicrovmApiKeyListResult{Keys: items, Count: len(items)}, nil
}

// createMicrovmApiKey returns the created key object plus the shown-once
// plaintext secret; no other tool response ever carries it again.
func createMicrovmApiKey(ctx context.Context, cl *client.Client, in CreateMicrovmApiKeyInput) (MicrovmApiKeyResult, error) {
	result, err := cl.CreateMicrovmApiKey(ctx, in.Name)
	if err != nil {
		return MicrovmApiKeyResult{}, err
	}
	key, _ := result["key"].(map[string]any)
	plaintext, _ := result["plaintext"].(string)
	return MicrovmApiKeyResult{Key: key, Plaintext: plaintext}, nil
}

func deleteMicrovmApiKey(ctx context.Context, cl *client.Client, in DeleteMicrovmApiKeyInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovmApiKey(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerMicrovmApiKeyTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.api_key.list", Description: "List the caller's MicroVM Sandboxes API keys (never returns the plaintext key)."}, listMicrovmApiKeys)
	Register(s, deps, Spec{Name: "user.microvm.api_key.create", Description: "Create a new API key. The plaintext key is returned ONLY in this response."}, createMicrovmApiKey)
	Register(s, deps, Spec{Name: "user.microvm.api_key.delete", Description: "Revoke an API key. Irreversible. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovmApiKey)
}
