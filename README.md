# iaas-mcp-server

A [Model Context Protocol](https://modelcontextprotocol.io) server that lets AI agents manage
the Hypervisor.io platform (instances, VPCs, Kubernetes, databases, and more) over the existing
user/admin REST API.

This repo implements Phase 1 of
[`docs/plans/2026-07-06-mcp-server-build.md`](../Master/docs/plans/2026-07-06-mcp-server-build.md)
(spec [`17-opentofu-mcp-api-trisync.md`](../Master/specs/17-opentofu-mcp-api-trisync.md) in the
Master repo): a scaffold only. **No tools are registered yet** - those land in Phase 2 onward.

## What's here

- A stateless **Streamable-HTTP** MCP server (`mcp.StreamableHTTPOptions.Stateless = true`,
  targeting the 2025-06-18+ MCP spec's stateless model): no session store, so any request can be
  served by any replica and the server scales horizontally with no sticky routing.
- Auth: **bearer-token pass-through** for v1. The MCP server does not verify tokens itself; it
  relays whatever bearer token the caller presents straight to the platform API, which
  authenticates it there. Token handling lives entirely behind one seam
  (`internal/iaasauth.Verifier` + `internal/iaasauth.TokenSource`) so a later OAuth 2.1 + PKCE
  facade (Dynamic Client Registration, RFC 9728 protected-resource metadata, JWT/JWKS
  verification) can replace it without touching the HTTP wiring or any tool handler.
- Reuses the OpenTofu provider's tested API client and async waiter -
  `github.com/hypervisor-io/terraform-provider-iaas/{client,waiter}` - via a `replace` directive
  pointing at a sibling checkout, so this server and the provider share one client implementation
  against one source of truth (the platform API).
- `/healthz`: a plain, unauthenticated liveness endpoint.

## IP-lock / auth note

Bearer pass-through only works because platform user API tokens are no longer hard IP-locked
(`allowed_ips` is opt-in, not mandatory). If a token you pass through is IP-restricted on the
platform side, calls will fail with the platform's normal 401/403 - the MCP server does not add
or remove any IP restriction, it is purely a relay.

## Running it

Requires Go 1.25+ (the MCP Go SDK's own `go.mod` floors there) and a sibling checkout of
`terraform-provider-iaas` at `../terraform-provider-iaas` (see the `replace` directive in
`go.mod`).

```bash
export IAAS_API_ENDPOINT="https://panel.example.com/api"   # required
export IAAS_MCP_LISTEN=":8080"                              # optional, default shown
export IAAS_API_TIMEOUT="30s"                                # optional, default shown
export IAAS_API_INSECURE="false"                             # optional, default shown

go run .
```

Then:

```bash
curl http://localhost:8080/healthz
# ok

curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -H "Authorization: Bearer <platform-api-token>" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"smoke-test","version":"0.0.1"}}}'
```

A request to `/mcp` with no `Authorization` header gets an MCP auth error (HTTP 401).

## Environment variables

| Variable             | Required | Default | Meaning                                              |
|-----------------------|----------|---------|-------------------------------------------------------|
| `IAAS_API_ENDPOINT`   | yes      | -       | Base URL of the platform user API                     |
| `IAAS_MCP_LISTEN`     | no       | `:8080` | Address the HTTP server binds to                      |
| `IAAS_API_TIMEOUT`    | no       | `30s`   | Per-request timeout to the platform API (Go duration) |
| `IAAS_API_INSECURE`   | no       | `false` | Skip TLS verification on calls to the platform API    |

## Development

```bash
make build   # go build
make vet     # go vet
make fmt     # gofmt -w
make test    # go test ./...
make check   # all of the above
```

CI (`.github/workflows/ci.yml`) runs `go build`, `go vet`, `gofmt -l`, and `go test` on every
push and PR - no external services required. It checks out `terraform-provider-iaas` as a
sibling so the local `replace` directive resolves the same way it does on a dev machine; see the
comment in that workflow for the secret it needs until the client is promoted to its own tagged
module (spec 17, Phase 0).

## Roadmap

Tools come in later phases (see the build plan): a tool-registration framework plus two golden
tools (Phase 2), the full `user.*` surface (Phase 3), a curated `admin.*` allowlist (Phase 4), the
tri-sync manifest/CI gate keeping this server, the OpenTofu provider, and the platform API in
lockstep (Phase 5), and a published client walkthrough (Phase 6).
