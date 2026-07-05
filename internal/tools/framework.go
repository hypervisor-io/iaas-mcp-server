// Package tools is the MCP tool framework and the golden instance/vpc tools.
//
// A "tool" here is one MCP tool (an agent-callable action) bound to a business
// function that talks to the platform API through the shared
// terraform-provider-iaas client. The framework (this file) supplies the
// cross-cutting behavior every tool needs so each business function stays a
// thin, readable mapping onto client calls:
//
//   - per-request client acquisition from the TokenSource seam (the token
//     rides on ctx, put there by the bearer-auth middleware - see
//     internal/iaasauth),
//   - error mapping: a client *APIError becomes a clear MCP tool error with
//     the same hints spec 17 mandates (401/403 -> IP-lock/scope; 422 -> field
//     errors; 404 -> not found; 429 -> retryable),
//   - a destructive-op confirm gate (delete/destroy tools refuse unless the
//     caller passes confirm:true),
//   - an idempotency-key seam (an optional key on the input is threaded onto
//     ctx for client methods that support the Idempotency-Key header).
//
// Async convergence (poll a task/resource to a terminal state) lives in
// async.go and is invoked by the create/delete business functions directly,
// reusing the provider's waiter package and its Ready/Fail predicates.
package tools

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Deps are the collaborators every tool needs. It is built once in main and
// passed to Register. TokenSource is the ONLY way a tool obtains an API
// client, so auth stays isolated behind that seam.
type Deps struct {
	TokenSource iaasauth.TokenSource
}

// Spec is the static description of a tool: its wire name, its human/agent
// description, and whether it is destructive (which turns on the confirm gate).
type Spec struct {
	// Name is the MCP tool name, e.g. "user.instance.create".
	Name string
	// Description is shown to the agent; keep it action-oriented and note any
	// gate (e.g. "requires confirm:true").
	Description string
	// Destructive marks delete/destroy tools. When true, the framework refuses
	// the call unless the input carries confirm:true (see confirmer / Confirm).
	Destructive bool
	// Admin marks tools that call the admin API (prefix /v1). It does NOT change
	// auth (the same Bearer pass-through seam is used; the admin API authorizes
	// server-side), it only enriches a 401/403 into an admin-scope hint so the
	// caller knows to present an admin-scoped, IP-registered token.
	Admin bool
}

// Handler is a tool's business function: given the per-request client and the
// typed input, it performs the API calls and returns the typed output. It
// returns the RAW error from the client; the framework applies MapError so
// every tool maps errors identically.
type Handler[In, Out any] func(ctx context.Context, cl *client.Client, in In) (Out, error)

// Register wires one tool onto the MCP server: it builds the cross-cutting
// wrapper around fn and hands it to mcp.AddTool, which auto-generates the
// input/output JSON schema from the In/Out types and validates incoming
// arguments before the handler runs.
func Register[In, Out any](s *mcp.Server, deps Deps, spec Spec, fn Handler[In, Out]) {
	// AddTool panics on a schema error, so a name is only recorded once the tool
	// is actually, successfully registered.
	mcp.AddTool(s, &mcp.Tool{
		Name:        spec.Name,
		Description: spec.Description,
	}, wrap(deps, spec, fn))
	recordToolName(spec.Name)
}

// toolNameRecorder is a test-only hook. The MCP Go SDK exposes no public way to
// enumerate a server's registered tools without a live client session, so
// RegisteredToolNames captures each name at registration through this hook. It
// is nil in normal operation (production RegisterAll records nothing).
var (
	recorderMu       sync.Mutex
	toolNameRecorder func(string)
)

func recordToolName(name string) {
	if toolNameRecorder != nil {
		toolNameRecorder(name)
	}
}

// RegisteredToolNames returns the name of every tool RegisterAll registers. It
// registers all families onto a throwaway server (registration does not require
// a live token, so a zero-value Deps is fine) and captures each name via the
// recorder hook. Intended for the tri-sync coverage gate.
func RegisteredToolNames() []string {
	recorderMu.Lock()
	defer recorderMu.Unlock()

	var names []string
	toolNameRecorder = func(n string) { names = append(names, n) }
	defer func() { toolNameRecorder = nil }()

	RegisterAll(mcp.NewServer(&mcp.Implementation{Name: "trisync-enum", Version: "test"}, nil), Deps{})
	return names
}

// wrap turns a Handler into an mcp.ToolHandlerFor, layering (in order) the
// confirm gate, the idempotency-key seam, client acquisition, and error
// mapping around the business function.
func wrap[In, Out any](deps Deps, spec Spec, fn Handler[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out

		// Destructive-confirm gate. A destructive tool's input MUST embed
		// Confirm; refuse the call until confirm:true is supplied so an agent
		// cannot delete/destroy on a slip.
		if spec.Destructive && !confirmed(in) {
			return nil, zero, fmt.Errorf(
				"refusing destructive operation %q: pass \"confirm\": true to authorize it",
				spec.Name,
			)
		}

		// Idempotency-key seam: if the input carries a non-empty key, thread it
		// onto ctx so a client method that supports the Idempotency-Key header
		// (e.g. Kubernetes create, in a later phase) can pick it up. The golden
		// instance/vpc create endpoints do NOT carry the idempotency.user
		// middleware server-side, so they ignore it; the seam is here so those
		// later tools need no extra wiring.
		if key := idempotencyKeyOf(in); key != "" {
			ctx = withIdempotencyKey(ctx, key)
		}

		cl, err := deps.TokenSource(ctx)
		if err != nil {
			// No/invalid token reached the tool. In production the auth
			// middleware rejects this before the tool runs (HTTP 401); this is
			// the defensive in-band failure.
			return nil, zero, fmt.Errorf("authentication required: %w", err)
		}

		out, err := fn(ctx, cl, in)
		if err != nil {
			return nil, zero, mapToolError(spec, err)
		}
		return nil, out, nil
	}
}

// mapToolError applies MapError, then for an admin tool that got a 401/403
// replaces the generic access-denied wording with an admin-scope hint: the
// admin API needs an admin-scoped token that is registered from the calling IP
// (admin tokens are IP-locked) and enabled.
func mapToolError(spec Spec, err error) error {
	mapped := MapError(err)
	if !spec.Admin {
		return mapped
	}
	var apiErr *client.APIError
	if errors.As(mapped, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) {
		return fmt.Errorf(
			"admin authorization failed: this tool calls the admin API and needs an admin-scoped token "+
				"that is enabled and registered from this IP (admin tokens are IP-locked): %w",
			apiErr,
		)
	}
	return mapped
}

// MapError converts a client *APIError into a tool-facing error carrying the
// hints spec 17 requires. The client's APIError.Error() already appends the
// IP-lock/scope hint for 401/403 and formats 422 field errors, so MapError
// only prefixes a stable, machine-greppable category. Non-APIError errors
// (transport failures, waiter timeouts, terminal task-fail states) pass
// through unchanged.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Status {
	case 404:
		return fmt.Errorf("not found: %w", apiErr)
	case 401, 403:
		return fmt.Errorf("access denied: %w", apiErr)
	case 422:
		return fmt.Errorf("validation failed: %w", apiErr)
	case 429:
		// The shared client already retried with exponential backoff; a 429
		// that still reaches here is surfaced as retryable so the agent can
		// back off and try again.
		return fmt.Errorf("rate limited (retryable): %w", apiErr)
	default:
		return apiErr
	}
}

// OKResult is the structured output for action tools that mutate state but
// return no object (attach/detach, add/remove a child, enable/disable, resize,
// restart). OK is always true on success; a failure comes back as an error
// result instead.
type OKResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// okResult is the conventional success value for a void action.
func okResult(msg string) OKResult { return OKResult{OK: true, Message: msg} }

// asObjectList coerces an envelope array value (decoded as []any of
// map[string]any) into []map[string]any, dropping any non-object elements. It
// never panics on an absent or mistyped value; it returns an empty slice.
func asObjectList(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// ── confirm gate ────────────────────────────────────────────────────────────

// confirmer is implemented by any input that gates a destructive operation.
// Destructive tool inputs get it for free by embedding Confirm.
type confirmer interface {
	confirmed() bool
}

// Confirmation is embedded in a destructive tool's input struct to add the
// confirm:true gate. It is intentionally not required by the JSON schema
// (absent -> false -> refused) so the refusal message, not a schema error,
// explains what to do.
//
// The type name deliberately differs from its field name (Confirm): an
// embedded struct whose type and field share a name collides under Go field
// promotion, which hides the promoted field from reflect.VisibleFields and so
// drops it from the SDK's generated JSON schema.
type Confirmation struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"set to true to authorize this destructive, irreversible operation"`
}

func (c Confirmation) confirmed() bool { return c.Confirm }

// confirmed reports whether in authorizes a destructive op. An input that does
// not embed Confirm is treated as not-confirmed (fail closed).
func confirmed[In any](in In) bool {
	if c, ok := any(in).(confirmer); ok {
		return c.confirmed()
	}
	return false
}

// ── idempotency seam ────────────────────────────────────────────────────────

// idempotent is implemented by any input that offers an idempotency key.
// Mutating inputs whose endpoint supports it embed Idempotent.
type idempotent interface {
	idempotencyKey() string
}

// Idempotent is embedded in a mutating tool's input to accept an optional
// idempotency key. Client methods that support the Idempotency-Key header read
// it back from ctx via IdempotencyKeyFromContext.
type Idempotent struct {
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"optional key that lets the server deduplicate a retried mutation"`
}

func (i Idempotent) idempotencyKey() string { return i.IdempotencyKey }

// idempotencyKeyOf returns the input's idempotency key, or "" when it carries
// none (input does not embed Idempotent, or the field is empty).
func idempotencyKeyOf[In any](in In) string {
	if i, ok := any(in).(idempotent); ok {
		return i.idempotencyKey()
	}
	return ""
}

type idempotencyKeyCtx struct{}

// withIdempotencyKey returns ctx carrying key.
func withIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKeyCtx{}, key)
}

// IdempotencyKeyFromContext returns the idempotency key threaded onto ctx by
// the framework, or "" if none. A business function passes it to a client
// method that supports the Idempotency-Key header.
func IdempotencyKeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(idempotencyKeyCtx{}).(string); ok {
		return v
	}
	return ""
}
