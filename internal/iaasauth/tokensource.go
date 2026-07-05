// Package iaasauth is the single seam through which the platform API bearer
// token reaches the IaaS API client.
//
// D1 (specs/17-opentofu-mcp-api-trisync.md) picks token pass-through for v1:
// the MCP server does not verify tokens itself, it just relays whatever
// bearer token the caller presents straight to the platform API, which
// authenticates it there. This is deliberately isolated in one small package
// so that a later OAuth 2.1 + PKCE facade (DCR, RFC 9728 protected-resource
// metadata, JWT/JWKS verification) can replace Verifier with a real
// TokenVerifier without touching TokenSource's signature, main.go's HTTP
// wiring, or any tool handler downstream.
//
// Wiring (see main.go):
//  1. auth.RequireBearerToken(iaasauth.Verifier, opts) wraps the MCP
//     Streamable-HTTP handler. It extracts "Authorization: Bearer <token>"
//     from the incoming request, calls Verifier, and on success stashes the
//     resulting *auth.TokenInfo on the request's context before calling the
//     wrapped handler. On failure (missing/empty token) it writes the MCP
//     auth error itself: HTTP 401 with a WWW-Authenticate header, per the
//     streamable HTTP transport's auth model.
//  2. Because the server runs stateless (mcp.StreamableHTTPOptions.Stateless
//     = true), every JSON-RPC call - including tools/call - arrives as its
//     own independent HTTP request and is authenticated by step 1 before the
//     MCP server ever sees it. The context threaded into a tool handler is a
//     descendant of that same request's context (the SDK connects each
//     stateless request with server.Connect(req.Context(), ...) specifically
//     so middleware-added context values survive), so TokenSource(ctx) inside
//     a future tool handler recovers exactly the token that authenticated
//     that call.
package iaasauth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// tokenExtraKey is the key under which Verifier stores the raw bearer token
// in auth.TokenInfo.Extra, for TokenSource to read back out.
const tokenExtraKey = "iaas_bearer_token"

// assumedValidity is the Expiration auth.RequireBearerToken's own middleware
// requires every TokenInfo to carry (it 401s a zero Expiration, and a token
// past Expiration, since it was written for real OAuth tokens with a known
// lifetime). Pass-through has no such lifetime to report - the platform API
// is the sole authority on whether the token is still good, and re-checks it
// on every call anyway - so Verifier stamps a short, constant window that is
// re-derived fresh on every request (nothing is cached across requests in
// this stateless server). It only needs to outlive the single request being
// authenticated; if the underlying platform token is actually expired, the
// platform API rejects the call and that surfaces as a normal 401 from the
// client, mapped per spec 17's error-mapping rule, not as an MCP auth error.
const assumedValidity = 5 * time.Minute

// ErrNoToken is returned by a TokenSource when the request context carries
// no verified token. In normal operation this cannot happen for a request
// that reached the MCP handler, because auth.RequireBearerToken rejects
// tokenless requests before they get there; it exists as a defensive,
// directly testable failure mode.
var ErrNoToken = errors.New("iaasauth: no bearer token on request context")

// Verifier is an auth.TokenVerifier implementing the v1 pass-through
// strategy: it performs no independent validation of the token (there is no
// authorization server yet to ask) and instead treats the bearer token
// itself AS the platform API token, to be replayed verbatim against
// IAAS_API_ENDPOINT. The platform API is the one place that actually
// authenticates it.
//
// This is the only function in the codebase that reads the raw token out of
// the Authorization header (via the auth package's extraction) and decides
// whether a request may proceed.
func Verifier(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	if strings.TrimSpace(token) == "" {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{
		Expiration: time.Now().Add(assumedValidity),
		Extra:      map[string]any{tokenExtraKey: token},
	}, nil
}

// TokenSource builds a per-request *client.Client from the token that
// auth.RequireBearerToken (using Verifier) already attached to ctx. Tool
// handlers in later phases call this once per invocation; it must never be
// called with a context that did not pass through the auth middleware.
type TokenSource func(ctx context.Context) (*client.Client, error)

// NewTokenSource returns the default TokenSource, closed over the API
// endpoint/timeout/TLS settings that are constant for the process (only the
// bearer token varies per request).
func NewTokenSource(cfg Config) TokenSource {
	return func(ctx context.Context) (*client.Client, error) {
		info := auth.TokenInfoFromContext(ctx)
		if info == nil {
			return nil, ErrNoToken
		}
		token, _ := info.Extra[tokenExtraKey].(string)
		if token == "" {
			return nil, ErrNoToken
		}
		return client.New(cfg.APIEndpoint, token, cfg.RequestTimeout, cfg.Insecure), nil
	}
}

// Config is the subset of settings TokenSource needs to build a
// *client.Client. It mirrors (and is built from) config.Config, kept as its
// own tiny type here so this package does not import the top-level config
// package - it only needs three fields, not process-wide concerns like the
// listen address.
type Config struct {
	APIEndpoint    string
	RequestTimeout time.Duration
	Insecure       bool
}
