package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM sandbox tools, mirroring the MV1-25 user_api endpoints. A sandbox is
// a short-lived MicroVM in a hypervisor group with an auto-pause/kill TTL.
// kill is confirm-gated (destructive, irreversible).

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmSandboxTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListSandboxesInput struct {
	State string `json:"state,omitempty" jsonschema:"filter by state: running, paused, killed"`
}

type GetSandboxInput struct {
	ID string `json:"id" jsonschema:"UUID of the sandbox"`
}

type CreateSandboxInput struct {
	HypervisorGroupID string         `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (location) to place the sandbox in"`
	Template          string         `json:"template" jsonschema:"base template name or a custom template id/alias"`
	Timeout           int            `json:"timeout,omitempty" jsonschema:"seconds before the sandbox auto-pauses or kills, default 15"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	EnvVars           map[string]any `json:"env_vars,omitempty"`
	Secure            *bool          `json:"secure,omitempty"`
	OnTimeout         string         `json:"on_timeout,omitempty" jsonschema:"pause or kill, default pause"`
}

type SetSandboxTimeoutInput struct {
	ID      string `json:"id" jsonschema:"UUID of the sandbox"`
	Timeout int    `json:"timeout" jsonschema:"new TTL in seconds, overwrites the previous one"`
}

// KillSandboxInput permanently destroys a sandbox. Confirm-gated
// (destructive) - the sandbox and its disk are gone for good.
type KillSandboxInput struct {
	ID string `json:"id" jsonschema:"UUID of the sandbox to kill"`
	Confirmation
}

type SandboxResult struct {
	Sandbox map[string]any `json:"sandbox"`
}

type SandboxListResult struct {
	Sandboxes []map[string]any `json:"sandboxes"`
	Count     int              `json:"count"`
}

type SandboxLogsResult struct {
	Logs []map[string]any `json:"logs"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listSandboxes(ctx context.Context, cl *client.Client, in ListSandboxesInput) (SandboxListResult, error) {
	items, err := cl.ListSandboxes(ctx, in.State)
	if err != nil {
		return SandboxListResult{}, err
	}
	return SandboxListResult{Sandboxes: items, Count: len(items)}, nil
}

func getSandbox(ctx context.Context, cl *client.Client, in GetSandboxInput) (SandboxResult, error) {
	obj, err := cl.GetSandbox(ctx, in.ID)
	if err != nil {
		return SandboxResult{}, err
	}
	return SandboxResult{Sandbox: obj}, nil
}

// createSandbox sends only the fields the caller actually set; unset optional
// fields are omitted so the API applies its own defaults (timeout 15,
// on_timeout pause).
func createSandbox(ctx context.Context, cl *client.Client, in CreateSandboxInput) (SandboxResult, error) {
	body := map[string]any{"hypervisor_group_id": in.HypervisorGroupID, "template": in.Template}
	if in.Timeout > 0 {
		body["timeout"] = in.Timeout
	}
	if in.Metadata != nil {
		body["metadata"] = in.Metadata
	}
	if in.EnvVars != nil {
		body["env_vars"] = in.EnvVars
	}
	if in.Secure != nil {
		body["secure"] = *in.Secure
	}
	if in.OnTimeout != "" {
		body["on_timeout"] = in.OnTimeout
	}
	obj, err := cl.CreateSandbox(ctx, body)
	if err != nil {
		return SandboxResult{}, err
	}
	return SandboxResult{Sandbox: obj}, nil
}

func pauseSandbox(ctx context.Context, cl *client.Client, in GetSandboxInput) (SandboxResult, error) {
	obj, err := cl.PauseSandbox(ctx, in.ID)
	if err != nil {
		return SandboxResult{}, err
	}
	return SandboxResult{Sandbox: obj}, nil
}

func resumeSandbox(ctx context.Context, cl *client.Client, in GetSandboxInput) (SandboxResult, error) {
	obj, err := cl.ResumeSandbox(ctx, in.ID)
	if err != nil {
		return SandboxResult{}, err
	}
	return SandboxResult{Sandbox: obj}, nil
}

func setSandboxTimeout(ctx context.Context, cl *client.Client, in SetSandboxTimeoutInput) (SandboxResult, error) {
	obj, err := cl.SetSandboxTimeout(ctx, in.ID, in.Timeout)
	if err != nil {
		return SandboxResult{}, err
	}
	return SandboxResult{Sandbox: obj}, nil
}

func killSandbox(ctx context.Context, cl *client.Client, in KillSandboxInput) (DeleteResult, error) {
	if err := cl.KillSandbox(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func sandboxMetrics(ctx context.Context, cl *client.Client, in GetSandboxInput) (map[string]any, error) {
	return cl.GetSandboxMetrics(ctx, in.ID)
}

func sandboxLogs(ctx context.Context, cl *client.Client, in GetSandboxInput) (SandboxLogsResult, error) {
	logs, err := cl.GetSandboxLogs(ctx, in.ID)
	if err != nil {
		return SandboxLogsResult{}, err
	}
	return SandboxLogsResult{Logs: logs}, nil
}

func registerMicrovmSandboxTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.sandbox.list", Description: "List the caller's MicroVM sandboxes, optionally filtered by state."}, listSandboxes)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.get", Description: "Get a sandbox by UUID."}, getSandbox)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.create", Description: "Create a new sandbox in a hypervisor group from a base or custom template."}, createSandbox)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.pause", Description: "Pause a running sandbox (snapshots it on the node)."}, pauseSandbox)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.resume", Description: "Resume a paused sandbox."}, resumeSandbox)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.set_timeout", Description: "Overwrite a sandbox's auto-pause/kill TTL."}, setSandboxTimeout)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.kill", Description: "Permanently destroy a sandbox. Irreversible. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, killSandbox)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.metrics", Description: "Get the latest CPU/memory/disk sample for a sandbox."}, sandboxMetrics)
	Register(s, deps, Spec{Name: "user.microvm.sandbox.logs", Description: "Get recent console log lines for a sandbox."}, sandboxLogs)
}
