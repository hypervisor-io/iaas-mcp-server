package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM tools, mirroring the MVU2-2 user_api endpoints. A MicroVM runs an
// image version with a plan, lifetime, hooks, network and ingress; what used
// to be a sandbox, a serverless app or a CI runner is now an attribute
// combination of this one resource. kill and delete are confirm-gated
// (destructive); delete also soft-deletes the row and releases the name.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmsInput struct {
	Search      string `json:"search,omitempty" jsonschema:"filter by name substring"`
	State       string `json:"state,omitempty" jsonschema:"filter by state: creating, running, paused, stopped, killed, error"`
	ImageID     string `json:"image_id,omitempty" jsonschema:"filter by image UUID"`
	ConnectorID string `json:"connector_id,omitempty" jsonschema:"filter by connector UUID"`
}

type GetMicrovmInput struct {
	ID string `json:"id" jsonschema:"UUID of the MicroVM"`
}

// CreateMicrovmInput carries the U3 Create MicroVM body. "ingress", "network"
// and "lifecycle_hooks" are passed through verbatim (C6/C8 shapes); unset
// optional fields are omitted so the API applies its own defaults.
type CreateMicrovmInput struct {
	HypervisorGroupID  string           `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (location) to place the MicroVM in"`
	ImageID            string           `json:"image_id" jsonschema:"UUID of a ready image visible to the account"`
	Name               string           `json:"name" jsonschema:"lowercase letters, digits and dashes, max 63, unique within the account"`
	ImageVersionID     string           `json:"image_version_id,omitempty" jsonschema:"UUID of the image version to run, defaults to the image's current version"`
	PlanID             string           `json:"plan_id,omitempty" jsonschema:"UUID of a plan in the location's plan group, defaults to its first enabled plan"`
	Ingress            map[string]any   `json:"ingress,omitempty" jsonschema:"{http: {enabled, port?, health_path?, extra_ports?}, shell: {enabled}}"`
	Network            []map[string]any `json:"network,omitempty" jsonschema:"network spec entries, e.g. [{kind: isolated}] (the default), [{kind: public, subnet_id}], [{kind: vpc, vpc_subnet_id}]"`
	MaxLifetimeSeconds int              `json:"max_lifetime_seconds,omitempty" jsonschema:"seconds before the MicroVM auto-pauses or kills, from the last start"`
	OnTimeout          string           `json:"on_timeout,omitempty" jsonschema:"pause or kill, default pause"`
	IdleTimeoutSeconds int              `json:"idle_timeout_seconds,omitempty" jsonschema:"seconds of inactivity before pausing; omit to never idle-pause"`
	AlwaysOn           *bool            `json:"always_on,omitempty" jsonschema:"never idle-pause and restart when the keeper finds it dead"`
	Env                string           `json:"env,omitempty" jsonschema:"environment variables, stored encrypted and never returned"`
	LifecycleHooks     map[string]any   `json:"lifecycle_hooks,omitempty" jsonschema:"hooks {port?, run|resume|suspend|terminate: {enabled, timeout, payload?}}"`
	Domain             string           `json:"domain,omitempty" jsonschema:"custom hostname to attach at create time"`
	Secure             *bool            `json:"secure,omitempty" jsonschema:"gate envd access behind the MicroVM's access token"`
}

type SetMicrovmTimeoutInput struct {
	ID      string `json:"id" jsonschema:"UUID of the MicroVM"`
	Timeout int    `json:"timeout" jsonschema:"new TTL in seconds, overwrites the previous one"`
}

type DeployMicrovmInput struct {
	ID             string `json:"id" jsonschema:"UUID of the MicroVM"`
	ImageVersionID string `json:"image_version_id" jsonschema:"UUID of a newer image version to deploy (health-gated before traffic switches)"`
}

type RollbackMicrovmInput struct {
	ID             string `json:"id" jsonschema:"UUID of the MicroVM"`
	ImageVersionID string `json:"image_version_id" jsonschema:"UUID of a previously deployed image version to roll back to"`
}

type SetMicrovmEnvInput struct {
	ID  string            `json:"id" jsonschema:"UUID of the MicroVM"`
	Env map[string]string `json:"env" jsonschema:"environment variables to set; a key missing here keeps its value, set a key to null server-side to remove it"`
}

type AddMicrovmDomainInput struct {
	ID       string `json:"id" jsonschema:"UUID of the MicroVM"`
	Hostname string `json:"hostname" jsonschema:"custom hostname to attach"`
}

// RemoveMicrovmDomainInput detaches a custom hostname. Confirm-gated
// (destructive) - traffic to that hostname stops immediately.
type RemoveMicrovmDomainInput struct {
	ID       string `json:"id" jsonschema:"UUID of the MicroVM"`
	DomainID string `json:"domain_id" jsonschema:"UUID of the domain row to detach"`
	Confirmation
}

// KillMicrovmInput permanently destroys the MicroVM's current run on the
// node. Confirm-gated (destructive) - the run and its disk are gone for good;
// the MicroVM row survives.
type KillMicrovmInput struct {
	ID string `json:"id" jsonschema:"UUID of the MicroVM to kill"`
	Confirmation
}

// DeleteMicrovmInput kills (when needed) and deletes the MicroVM.
// Confirm-gated (destructive) - the row is soft-deleted and its name is
// released.
type DeleteMicrovmInput struct {
	ID string `json:"id" jsonschema:"UUID of the MicroVM to delete"`
	Confirmation
}

type GetMicrovmLogsInput struct {
	ID    string `json:"id" jsonschema:"UUID of the MicroVM"`
	Limit int    `json:"limit,omitempty" jsonschema:"max log lines, server default 1000"`
}

type MicrovmResult struct {
	Microvm map[string]any `json:"microvm"`
}

// MicrovmShowResult is the SHOW envelope: the presented microvm plus its
// recent runs, the image's versions, domains and env key names (values are
// never serialized).
type MicrovmShowResult struct {
	Microvm  map[string]any   `json:"microvm"`
	Runs     []map[string]any `json:"runs"`
	Versions []map[string]any `json:"versions"`
	Domains  []map[string]any `json:"domains"`
	EnvKeys  []string         `json:"env_keys"`
}

type MicrovmListResult struct {
	Microvms []map[string]any `json:"microvms"`
	Count    int              `json:"count"`
}

type MicrovmRunResult struct {
	Run map[string]any `json:"run"`
}

type MicrovmEnvResult struct {
	Keys []string `json:"keys"`
}

type MicrovmDomainResult struct {
	Domain map[string]any `json:"domain"`
}

type MicrovmLogsResult struct {
	Lines []map[string]any `json:"lines"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listMicrovms(ctx context.Context, cl *client.Client, in ListMicrovmsInput) (MicrovmListResult, error) {
	items, err := cl.ListMicrovms(ctx, in.Search, in.State, in.ImageID, in.ConnectorID)
	if err != nil {
		return MicrovmListResult{}, err
	}
	return MicrovmListResult{Microvms: items, Count: len(items)}, nil
}

func getMicrovm(ctx context.Context, cl *client.Client, in GetMicrovmInput) (MicrovmShowResult, error) {
	env, err := cl.GetMicrovm(ctx, in.ID)
	if err != nil {
		return MicrovmShowResult{}, err
	}
	microvm, _ := env["microvm"].(map[string]any)
	envKeys := []string{}
	if arr, ok := env["env_keys"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				envKeys = append(envKeys, s)
			}
		}
	}
	return MicrovmShowResult{
		Microvm:  microvm,
		Runs:     asObjectList(env["runs"]),
		Versions: asObjectList(env["versions"]),
		Domains:  asObjectList(env["domains"]),
		EnvKeys:  envKeys,
	}, nil
}

// createMicrovm sends only the fields the caller actually set; unset optional
// fields are omitted so the API applies its own defaults (isolated network,
// on_timeout pause, no lifetime cap beyond the platform default).
func createMicrovm(ctx context.Context, cl *client.Client, in CreateMicrovmInput) (MicrovmResult, error) {
	body := map[string]any{
		"hypervisor_group_id": in.HypervisorGroupID,
		"image_id":            in.ImageID,
		"name":                in.Name,
	}
	if in.ImageVersionID != "" {
		body["image_version_id"] = in.ImageVersionID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if in.Ingress != nil {
		body["ingress"] = in.Ingress
	}
	if in.Network != nil {
		body["network"] = in.Network
	}
	if in.MaxLifetimeSeconds > 0 {
		body["max_lifetime_seconds"] = in.MaxLifetimeSeconds
	}
	if in.OnTimeout != "" {
		body["on_timeout"] = in.OnTimeout
	}
	if in.IdleTimeoutSeconds > 0 {
		body["idle_timeout_seconds"] = in.IdleTimeoutSeconds
	}
	if in.AlwaysOn != nil {
		body["always_on"] = *in.AlwaysOn
	}
	if in.Env != "" {
		body["env"] = in.Env
	}
	if in.LifecycleHooks != nil {
		body["lifecycle_hooks"] = in.LifecycleHooks
	}
	if in.Domain != "" {
		body["domain"] = in.Domain
	}
	if in.Secure != nil {
		body["secure"] = *in.Secure
	}
	microvm, err := cl.CreateMicrovm(ctx, body)
	if err != nil {
		return MicrovmResult{}, err
	}
	return MicrovmResult{Microvm: microvm}, nil
}

func pauseMicrovm(ctx context.Context, cl *client.Client, in GetMicrovmInput) (OKResult, error) {
	if err := cl.PauseMicrovm(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("paused"), nil
}

func resumeMicrovm(ctx context.Context, cl *client.Client, in GetMicrovmInput) (OKResult, error) {
	if err := cl.ResumeMicrovm(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("resumed"), nil
}

func stopMicrovm(ctx context.Context, cl *client.Client, in GetMicrovmInput) (OKResult, error) {
	if err := cl.StopMicrovm(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("stopped"), nil
}

func startMicrovm(ctx context.Context, cl *client.Client, in GetMicrovmInput) (OKResult, error) {
	if err := cl.StartMicrovm(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("started"), nil
}

func killMicrovm(ctx context.Context, cl *client.Client, in KillMicrovmInput) (OKResult, error) {
	if err := cl.KillMicrovm(ctx, in.ID); err != nil {
		return OKResult{}, err
	}
	return okResult("killed"), nil
}

func setMicrovmTimeout(ctx context.Context, cl *client.Client, in SetMicrovmTimeoutInput) (OKResult, error) {
	if err := cl.SetMicrovmTimeout(ctx, in.ID, in.Timeout); err != nil {
		return OKResult{}, err
	}
	return okResult("timeout updated"), nil
}

func deployMicrovm(ctx context.Context, cl *client.Client, in DeployMicrovmInput) (MicrovmRunResult, error) {
	run, err := cl.DeployMicrovm(ctx, in.ID, in.ImageVersionID)
	if err != nil {
		return MicrovmRunResult{}, err
	}
	return MicrovmRunResult{Run: run}, nil
}

func rollbackMicrovm(ctx context.Context, cl *client.Client, in RollbackMicrovmInput) (MicrovmRunResult, error) {
	run, err := cl.RollbackMicrovm(ctx, in.ID, in.ImageVersionID)
	if err != nil {
		return MicrovmRunResult{}, err
	}
	return MicrovmRunResult{Run: run}, nil
}

func setMicrovmEnv(ctx context.Context, cl *client.Client, in SetMicrovmEnvInput) (MicrovmEnvResult, error) {
	keys, err := cl.SetMicrovmEnv(ctx, in.ID, in.Env)
	if err != nil {
		return MicrovmEnvResult{}, err
	}
	return MicrovmEnvResult{Keys: keys}, nil
}

func addMicrovmDomain(ctx context.Context, cl *client.Client, in AddMicrovmDomainInput) (MicrovmDomainResult, error) {
	domain, err := cl.AddMicrovmDomain(ctx, in.ID, in.Hostname)
	if err != nil {
		return MicrovmDomainResult{}, err
	}
	return MicrovmDomainResult{Domain: domain}, nil
}

func removeMicrovmDomain(ctx context.Context, cl *client.Client, in RemoveMicrovmDomainInput) (OKResult, error) {
	if err := cl.RemoveMicrovmDomain(ctx, in.ID, in.DomainID); err != nil {
		return OKResult{}, err
	}
	return okResult("domain removed"), nil
}

func getMicrovmLogs(ctx context.Context, cl *client.Client, in GetMicrovmLogsInput) (MicrovmLogsResult, error) {
	lines, err := cl.GetMicrovmLogs(ctx, in.ID, in.Limit)
	if err != nil {
		return MicrovmLogsResult{}, err
	}
	return MicrovmLogsResult{Lines: lines}, nil
}

func getMicrovmMetrics(ctx context.Context, cl *client.Client, in GetMicrovmInput) (map[string]any, error) {
	return cl.GetMicrovmMetrics(ctx, in.ID)
}

func deleteMicrovm(ctx context.Context, cl *client.Client, in DeleteMicrovmInput) (DeleteResult, error) {
	if err := cl.DeleteMicrovm(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerMicrovmTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.microvm.vm.list", Description: "List the caller's MicroVMs, filterable by name, state, image and connector."}, listMicrovms)
	Register(s, deps, Spec{Name: "user.microvm.vm.get", Description: "Get a MicroVM by UUID, including its recent runs, image versions, domains and env key names (values are never returned)."}, getMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.create", Description: "Create a MicroVM from a ready image: pick a location, plan, ingress (http and/or shell), network, lifetime and hooks."}, createMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.pause", Description: "Pause a running MicroVM (snapshots it on the node)."}, pauseMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.resume", Description: "Resume a paused MicroVM."}, resumeMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.stop", Description: "Stop a MicroVM. Its hostname and DNS record are kept."}, stopMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.start", Description: "Start a stopped MicroVM again."}, startMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.kill", Description: "Permanently destroy the MicroVM's current run. The row survives; delete removes it. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, killMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.set_timeout", Description: "Overwrite the MicroVM's auto-pause/kill TTL."}, setMicrovmTimeout)
	Register(s, deps, Spec{Name: "user.microvm.vm.deploy", Description: "Deploy a newer image version (health-gated before ingress switches)."}, deployMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.rollback", Description: "Roll back to a previously deployed image version."}, rollbackMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.vm.set_env", Description: "Set environment variables. Returns only the merged key names, never values."}, setMicrovmEnv)
	Register(s, deps, Spec{Name: "user.microvm.vm.domain_add", Description: "Attach a custom hostname to a MicroVM with http ingress."}, addMicrovmDomain)
	Register(s, deps, Spec{Name: "user.microvm.vm.domain_remove", Description: "Detach a custom hostname. Traffic to it stops immediately. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, removeMicrovmDomain)
	Register(s, deps, Spec{Name: "user.microvm.vm.logs", Description: "Get recent console log lines for a MicroVM."}, getMicrovmLogs)
	Register(s, deps, Spec{Name: "user.microvm.vm.metrics", Description: "Get the latest CPU/memory/disk/network sample plus the per-minute series for a MicroVM."}, getMicrovmMetrics)
	Register(s, deps, Spec{Name: "user.microvm.vm.delete", Description: "Kill and delete a MicroVM, releasing its name. Irreversible. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovm)
}
