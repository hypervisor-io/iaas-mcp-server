package tools

import (
	"context"
	"fmt"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() { toolRegistrars = append(toolRegistrars, registerMicrovmMcpServerTools) }

type ListMicrovmMcpServersInput struct{}
type GetMicrovmMcpServerInput struct {
	ID string `json:"id" jsonschema:"MCP server UUID"`
}
type CreateMicrovmMcpServerInput struct {
	Provider    string             `json:"provider" jsonschema:"Catalog provider slug or custom"`
	Name        string             `json:"name" jsonschema:"Immutable unique account key, 1 to 32 lowercase letters digits hyphens, starts with letter or digit"`
	Label       string             `json:"label" jsonschema:"Display label, max 64 characters"`
	URL         *string            `json:"url,omitempty" jsonschema:"Required custom HTTPS Streamable HTTP endpoint"`
	AuthKind    *string            `json:"auth_kind,omitempty" jsonschema:"oauth (default for catalog), bearer, or none; required for custom"`
	Headers     *map[string]string `json:"headers,omitempty" jsonschema:"Up to 8 nonsecret custom headers, values at most 1024 bytes, no credential headers"`
	StaticToken *string            `json:"static_token,omitempty" jsonschema:"Required for bearer authentication. Write-only, never returned"`
	Enabled     *bool              `json:"enabled,omitempty" jsonschema:"Whether enabled, default true"`
}
type UpdateMicrovmMcpServerInput struct {
	ID          string             `json:"id" jsonschema:"MCP server UUID"`
	Label       *string            `json:"label,omitempty" jsonschema:"New display label"`
	URL         *string            `json:"url,omitempty" jsonschema:"Custom HTTPS endpoint. Changing identity is refused with active bindings"`
	AuthKind    *string            `json:"auth_kind,omitempty" jsonschema:"oauth, bearer, or none"`
	Headers     *map[string]string `json:"headers,omitempty" jsonschema:"Nonsecret custom headers; {} clears existing headers"`
	StaticToken *string            `json:"static_token,omitempty" jsonschema:"Only accepted when switching into bearer. Use rotate_token for an existing bearer token"`
	Enabled     *bool              `json:"enabled,omitempty" jsonschema:"Enable or disable the server. false is sent explicitly"`
}
type DeleteMicrovmMcpServerInput struct {
	ID string `json:"id" jsonschema:"MCP server UUID"`
	Confirmation
}
type ConnectMicrovmMcpServerInput struct {
	ID          string  `json:"id" jsonschema:"MCP server UUID"`
	ScopePreset *string `json:"scope_preset,omitempty" jsonschema:"Provider scope preset, such as read or read write"`
}
type DisconnectMicrovmMcpServerInput struct {
	ID string `json:"id" jsonschema:"MCP server UUID"`
	Confirmation
}
type RotateMicrovmMcpServerTokenInput struct {
	ID          string `json:"id" jsonschema:"MCP server UUID"`
	StaticToken string `json:"static_token" jsonschema:"New bearer token, 1 to 8192 characters. Never returned"`
	Confirmation
}
type MicrovmMcpServerResult struct {
	Server map[string]any `json:"server"`
}
type MicrovmMcpServerListResult struct {
	Servers []map[string]any `json:"servers"`
	Count   int              `json:"count"`
}
type MicrovmMcpProviderListResult struct {
	Providers []map[string]any `json:"providers"`
	Count     int              `json:"count"`
}
type MicrovmMcpConnectResult struct {
	AuthorizeURL string `json:"authorize_url"`
	Note         string `json:"note"`
}

func listMicrovmMcpProviders(ctx context.Context, cl *client.Client, in ListMicrovmMcpServersInput) (MicrovmMcpProviderListResult, error) {
	rows, err := cl.ListMicrovmMcpProviders(ctx)
	return MicrovmMcpProviderListResult{Providers: rows, Count: len(rows)}, err
}
func listMicrovmMcpServers(ctx context.Context, cl *client.Client, in ListMicrovmMcpServersInput) (MicrovmMcpServerListResult, error) {
	rows, err := cl.ListMicrovmMcpServers(ctx)
	return MicrovmMcpServerListResult{Servers: rows, Count: len(rows)}, err
}
func getMicrovmMcpServer(ctx context.Context, cl *client.Client, in GetMicrovmMcpServerInput) (MicrovmMcpServerResult, error) {
	row, err := cl.GetMicrovmMcpServer(ctx, in.ID)
	return MicrovmMcpServerResult{Server: row}, err
}
func microvmMcpWriteBody(label, url, kind, token *string, headers *map[string]string, enabled *bool) map[string]any {
	body := map[string]any{}
	for k, v := range map[string]*string{"label": label, "url": url, "auth_kind": kind, "static_token": token} {
		if v != nil {
			body[k] = *v
		}
	}
	if headers != nil {
		body["headers"] = *headers
	}
	if enabled != nil {
		body["enabled"] = *enabled
	}
	return body
}
func createMicrovmMcpServer(ctx context.Context, cl *client.Client, in CreateMicrovmMcpServerInput) (MicrovmMcpServerResult, error) {
	body := microvmMcpWriteBody(&in.Label, in.URL, in.AuthKind, in.StaticToken, in.Headers, in.Enabled)
	body["provider"] = in.Provider
	body["name"] = in.Name
	row, err := cl.CreateMicrovmMcpServer(ctx, body)
	return MicrovmMcpServerResult{Server: row}, err
}
func updateMicrovmMcpServer(ctx context.Context, cl *client.Client, in UpdateMicrovmMcpServerInput) (MicrovmMcpServerResult, error) {
	row, err := cl.UpdateMicrovmMcpServer(ctx, in.ID, microvmMcpWriteBody(in.Label, in.URL, in.AuthKind, in.StaticToken, in.Headers, in.Enabled))
	return MicrovmMcpServerResult{Server: row}, err
}
func deleteMicrovmMcpServer(ctx context.Context, cl *client.Client, in DeleteMicrovmMcpServerInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovmMcpServer(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}
func connectMicrovmMcpServer(ctx context.Context, cl *client.Client, in ConnectMicrovmMcpServerInput) (MicrovmMcpConnectResult, error) {
	row, err := cl.ConnectMicrovmMcpServer(ctx, in.ID, in.ScopePreset)
	if err != nil {
		return MicrovmMcpConnectResult{}, err
	}
	url, _ := row["authorize_url"].(string)
	if url == "" {
		return MicrovmMcpConnectResult{}, fmt.Errorf("MCP connect response omitted authorize_url")
	}
	return MicrovmMcpConnectResult{AuthorizeURL: url, Note: "Open authorize_url in a browser signed in as the same person who owns this API token. Authorization is not complete until the browser flow succeeds."}, nil
}
func disconnectMicrovmMcpServer(ctx context.Context, cl *client.Client, in DisconnectMicrovmMcpServerInput) (MicrovmMcpServerResult, error) {
	row, err := cl.DisconnectMicrovmMcpServer(ctx, in.ID)
	return MicrovmMcpServerResult{Server: row}, err
}
func rotateMicrovmMcpServerToken(ctx context.Context, cl *client.Client, in RotateMicrovmMcpServerTokenInput) (MicrovmMcpServerResult, error) {
	row, err := cl.RotateMicrovmMcpServerToken(ctx, in.ID, in.StaticToken)
	return MicrovmMcpServerResult{Server: row}, err
}
func registerMicrovmMcpServerTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.providers", Description: "List MCP provider catalog and custom option, auth modes and scope presets."}, listMicrovmMcpProviders)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.list", Description: "List account MCP servers with only the API token person's connection. No credential values are returned."}, listMicrovmMcpServers)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.get", Description: "Get a server revision, credential state and active binding summary without token values."}, getMicrovmMcpServer)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.create", Description: "Create an account MCP server. Bearer tokens are supplied by you, encrypted by Master and never returned."}, createMicrovmMcpServer)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.update", Description: "Edit server label, enabled flag or destination identity. Identity edits are refused while active bindings exist. Token is accepted only when transitioning into bearer."}, updateMicrovmMcpServer)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.delete", Description: "Delete an MCP server, refused with active bindings. Requires confirm true.", Destructive: true}, deleteMicrovmMcpServer)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.connect", Description: "Start OAuth sign-in. Returns authorize_url to open in the same API token person's browser session. Does not complete authorization."}, connectMicrovmMcpServer)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.disconnect", Description: "Disconnect only this API token person's OAuth connection. Other members and static account tokens are unchanged. Requires confirm true.", Destructive: true}, disconnectMicrovmMcpServer)
	Register(s, deps, Spec{Name: "user.microvm_mcp_server.rotate_token", Description: "Replace an existing bearer token without changing the server revision. Supply the new token; no token is returned. Requires confirm true.", Destructive: true}, rotateMicrovmMcpServerToken)
}
