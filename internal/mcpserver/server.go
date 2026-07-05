// Package mcpserver assembles the stateless Streamable-HTTP MCP server: the
// mcp.Server itself, the bearer-auth middleware, and the health endpoint,
// as one http.Handler main.go can hand to an http.Server.
package mcpserver

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

const (
	// Name is the MCP Implementation name advertised to clients on initialize.
	Name = "iaas-mcp-server"

	// mcpPath is where the Streamable-HTTP MCP endpoint is mounted. Chosen to
	// match the MCP ecosystem convention (e.g. the SDK's own auth example).
	mcpPath = "/mcp"

	// healthzPath is a plain liveness endpoint for load balancers/orchestrators;
	// it is intentionally outside the MCP protocol and unauthenticated.
	healthzPath = "/healthz"
)

// Options configures New.
type Options struct {
	// Version is the MCP Implementation version advertised on initialize
	// (e.g. a build tag or git SHA). May be empty.
	Version string

	// TokenSource builds a per-request IaaS API client from an authenticated
	// request's bearer token. Phase 1 registers no tools, so nothing calls it
	// yet, but it is threaded through newServer now so Phase 2 only has to add
	// mcp.AddTool calls there, not rewire construction.
	TokenSource iaasauth.TokenSource
}

// New builds the top-level http.Handler for the process: the MCP
// Streamable-HTTP endpoint (behind bearer-token auth) plus /healthz.
func New(opts Options) http.Handler {
	// Build ONE server with tools registered once, and hand it to every
	// request. The SDK explicitly allows getServer to return the same server
	// repeatedly; tools carry no per-request state (the caller's token flows
	// through the request context, not the server), so a single shared server
	// is correct for the stateless transport and avoids re-reflecting tool
	// schemas on every request.
	server := newServer(opts)
	getServer := func(*http.Request) *mcp.Server { return server }

	mcpHandler := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		// Stateless: no session store. Any request can be handled by any
		// replica, per D1 (specs/17-opentofu-mcp-api-trisync.md) - this is
		// what lets the server scale horizontally with no shared session
		// state or sticky routing.
		Stateless: true,
	})

	// auth.RequireBearerToken is the SDK's own bearer-auth middleware. Using
	// it (rather than a hand-rolled check) means a future OAuth 2.1 facade is
	// a drop-in replacement of iaasauth.Verifier plus these Options, with no
	// change to mcpHandler, getServer, or any tool - see the iaasauth package
	// doc comment for the full wiring story.
	//
	// opts left nil for v1: there is no protected-resource-metadata URL to
	// advertise yet (that arrives with the OAuth 2.1 facade). A missing or
	// malformed bearer token still gets the MCP auth error: HTTP 401.
	protectedMCP := auth.RequireBearerToken(iaasauth.Verifier, nil)(mcpHandler)

	mux := http.NewServeMux()
	mux.Handle(mcpPath, protectedMCP)
	mux.HandleFunc(healthzPath, healthzHandler)
	return mux
}

// newServer constructs the MCP server and registers the golden tools. Each
// tool obtains its per-request *client.Client from opts.TokenSource(ctx), so
// the shared server safely serves concurrent tenants: the token is on the
// request context, never on the server.
func newServer(opts Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    Name,
		Version: opts.Version,
	}, nil)

	tools.RegisterAll(server, tools.Deps{TokenSource: opts.TokenSource})

	return server
}

// healthzHandler is a minimal liveness probe: 200 if the process is up and
// serving. It deliberately does not check connectivity to the platform API -
// that is a readiness concern for a later phase, not a liveness one.
func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
