package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Webhook tools: the published event-kind catalog (NUI-V-R19-WH1). Read-only,
// so not confirm-gated. Webhook subscription create/update/delete are NOT
// exposed here (see api-manifest.json: excluded, "outbound event-webhook
// subscription management not in the v1 curated allowlist; candidate for a
// later phase") -- this tool only lets an agent validate an event kind before
// it (or the caller) subscribes through the API directly.

func init() {
	toolRegistrars = append(toolRegistrars, registerWebhookTools)
}

func registerWebhookTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.webhook.event_kinds", Description: "List the published catalog of webhook event kinds (event_kinds values a webhook subscription may name, or \"*\" for all)."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.GetWebhookEventKinds(ctx))
		})
}
