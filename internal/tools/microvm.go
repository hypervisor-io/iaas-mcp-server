package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM tools (contract /spec/microvm/v3, C1-C8; see the Master repo's
// docs/superpowers/plans/2026-09-15-microvm-v3-network-ssh-catalog.md). A
// MicroVM runs an image on a plan with a network, lifetime and hooks;
// kill and delete are confirm-gated (destructive).
//
// C1: network[] entries are `public` or `vpc` only - there is no `isolated`
// kind on this contract, and the daemon rejects one with a 400.
// C3: ssh_key_ids installs the account's SSH public keys into the guest at
// create time.
// C8: a `public` entry needs no subnet_id (the platform auto-assigns a
// public IPv4 subnet after placement); `static_ip_id` maps one of the
// account's already-allocated static IPs onto the NIC instead.
// MVU7-12/LOC-4: the public API names the location `location_id`, never the
// internal `hypervisor_group_id` column - every MicroVM tool uses the
// canonical name.

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListMicrovmsInput struct {
	Search      string `json:"search,omitempty" jsonschema:"filter by name substring"`
	State       string `json:"state,omitempty" jsonschema:"filter by state: creating, running, paused, stopped, killed, error"`
	ImageID     string `json:"image_id,omitempty" jsonschema:"filter by image UUID"`
	ConnectorID string `json:"connector_id,omitempty" jsonschema:"filter by owning connector UUID"`
}

type GetMicrovmInput struct {
	ID string `json:"id" jsonschema:"UUID of the microVM"`
}

// MicrovmNetworkEntryInput is one entry of a Create MicroVM `network[]` array
// (contract C1/C8). `kind` is `public` or `vpc` - never `isolated`, which is
// not a kind on this contract.
type MicrovmNetworkEntryInput struct {
	Kind             string   `json:"kind" jsonschema:"public or vpc"`
	SubnetID         string   `json:"subnet_id,omitempty" jsonschema:"kind public only, and optional even then (C8.1): a public IPv4 subnet is picked automatically after placement when omitted; set this only to pin a specific subnet"`
	VpcSubnetID      string   `json:"vpc_subnet_id,omitempty" jsonschema:"required when kind is vpc; must be a VPC subnet owned by the account"`
	StaticIPID       string   `json:"static_ip_id,omitempty" jsonschema:"kind public only (C8.3): maps one of the account's already-allocated static IPs in location_id onto this NIC instead of an auto-assigned address"`
	SecurityGroupIDs []string `json:"security_group_ids,omitempty" jsonschema:"security groups owned by the account, applied to this NIC"`
	RateMbit         int      `json:"rate_mbit,omitempty" jsonschema:"per-NIC rate limit in Mbit/s"`
}

// CreateMicrovmInput carries the Create MicroVM body. "ingress" and
// "lifecycle_hooks" are passed through verbatim; unset optional fields are
// omitted so the API applies its own defaults.
type CreateMicrovmInput struct {
	LocationID         string                     `json:"location_id" jsonschema:"UUID of the location to deploy into"`
	ImageID            string                     `json:"image_id" jsonschema:"UUID of a ready image visible to the account"`
	Name               string                     `json:"name" jsonschema:"lowercase letters, digits and dashes, max 63, unique within the account"`
	ImageVersionID     string                     `json:"image_version_id,omitempty" jsonschema:"UUID of the image version to run, defaults to the image's current version"`
	PlanID             string                     `json:"plan_id,omitempty" jsonschema:"UUID of a plan available at location_id, defaults to its first enabled plan"`
	SSHKeyIDs          []string                   `json:"ssh_key_ids,omitempty" jsonschema:"SSH keys owned by the account (C3), installed to /root/.ssh/authorized_keys and every non-system user's authorized_keys"`
	Network            []MicrovmNetworkEntryInput `json:"network,omitempty" jsonschema:"one or more network interfaces: {kind: public} (a public IPv4 subnet is assigned automatically, C8.1), {kind: public, static_ip_id} (map an allocated static IP, C8.3), or {kind: vpc, vpc_subnet_id}. The first entry becomes eth0 and decides the default route. Omit entirely to use the account's stored default (user.microvm.settings.get); with no default set, the request is rejected"`
	Ingress            map[string]any             `json:"ingress,omitempty" jsonschema:"{http: {enabled, port?, health_path?, extra_ports?}, shell: {enabled}} - shell requires an envd-capable image"`
	MaxLifetimeSeconds int                        `json:"max_lifetime_seconds,omitempty" jsonschema:"hard lifetime cap in seconds, from the last start"`
	OnTimeout          string                     `json:"on_timeout,omitempty" jsonschema:"pause or kill when the lifetime/idle timeout elapses, default pause"`
	IdleTimeoutSeconds int                        `json:"idle_timeout_seconds,omitempty" jsonschema:"seconds of inactivity before pausing; omit to never idle-pause"`
	AlwaysOn           *bool                      `json:"always_on,omitempty" jsonschema:"disable lifetime/idle timeouts entirely"`
	Env                string                     `json:"env,omitempty" jsonschema:"newline-separated KEY=value environment variables, stored encrypted and never returned"`
	LifecycleHooks     map[string]any             `json:"lifecycle_hooks,omitempty" jsonschema:"hooks {port?, run|resume|suspend|terminate: {enabled, timeout, payload?}}"`
	Domain             string                     `json:"domain,omitempty" jsonschema:"custom hostname to attach immediately after creation"`
	Secure             *bool                      `json:"secure,omitempty" jsonschema:"gate envd access behind the microVM's access token"`
}

type SetMicrovmTimeoutInput struct {
	ID      string `json:"id" jsonschema:"UUID of the microVM"`
	Timeout int    `json:"timeout" jsonschema:"seconds from now until the on_timeout action fires, minimum 1"`
}

type DeployMicrovmInput struct {
	ID             string `json:"id" jsonschema:"UUID of the microVM"`
	ImageVersionID string `json:"image_version_id" jsonschema:"a version belonging to the microVM's current image_id"`
}

type RollbackMicrovmInput struct {
	ID             string `json:"id" jsonschema:"UUID of the microVM"`
	ImageVersionID string `json:"image_version_id" jsonschema:"a version belonging to the microVM's current image_id"`
}

type SetMicrovmEnvInput struct {
	ID  string            `json:"id" jsonschema:"UUID of the microVM"`
	Env map[string]string `json:"env" jsonschema:"replaces the microVM's environment variables wholesale; values are never returned by any read tool"`
}

type AddMicrovmDomainInput struct {
	ID       string `json:"id" jsonschema:"UUID of the microVM"`
	Hostname string `json:"hostname" jsonschema:"hostname to attach, max 255 characters"`
}

// RemoveMicrovmDomainInput detaches a custom hostname. Confirm-gated
// (destructive) - traffic to that hostname stops immediately.
type RemoveMicrovmDomainInput struct {
	ID       string `json:"id" jsonschema:"UUID of the microVM"`
	DomainID string `json:"domain_id" jsonschema:"UUID of the domain row to detach"`
	Confirmation
}

// KillMicrovmInput forcibly kills the microVM's current run immediately (no
// graceful shutdown). Confirm-gated (destructive) - the row and its
// resources are retained until deleted.
type KillMicrovmInput struct {
	ID string `json:"id" jsonschema:"UUID of the microVM to kill"`
	Confirmation
}

// DeleteMicrovmInput kills (if needed) and permanently deletes the microVM,
// releasing its IPs, VPC interfaces and disks. Confirm-gated (destructive) -
// a mapped static IP is released back to `allocated`, never freed for
// re-allocation to another account (C8.3).
type DeleteMicrovmInput struct {
	ID string `json:"id" jsonschema:"UUID of the microVM to delete"`
	Confirmation
}

type GetMicrovmLogsInput struct {
	ID    string `json:"id" jsonschema:"UUID of the microVM"`
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

// networkEntryBody converts one typed network entry into the wire shape.
func networkEntryBody(e MicrovmNetworkEntryInput) map[string]any {
	entry := map[string]any{"kind": e.Kind}
	if e.SubnetID != "" {
		entry["subnet_id"] = e.SubnetID
	}
	if e.VpcSubnetID != "" {
		entry["vpc_subnet_id"] = e.VpcSubnetID
	}
	if e.StaticIPID != "" {
		entry["static_ip_id"] = e.StaticIPID
	}
	if len(e.SecurityGroupIDs) > 0 {
		entry["security_group_ids"] = e.SecurityGroupIDs
	}
	if e.RateMbit > 0 {
		entry["rate_mbit"] = e.RateMbit
	}
	return entry
}

// createMicrovm sends only the fields the caller actually set; unset
// optional fields are omitted so the API applies its own defaults (the
// account's stored network default when network is omitted, on_timeout
// pause, no lifetime cap beyond the platform default).
func createMicrovm(ctx context.Context, cl *client.Client, in CreateMicrovmInput) (MicrovmResult, error) {
	body := map[string]any{
		"location_id": in.LocationID,
		"image_id":    in.ImageID,
		"name":        in.Name,
	}
	if in.ImageVersionID != "" {
		body["image_version_id"] = in.ImageVersionID
	}
	if in.PlanID != "" {
		body["plan_id"] = in.PlanID
	}
	if len(in.SSHKeyIDs) > 0 {
		body["ssh_key_ids"] = in.SSHKeyIDs
	}
	if in.Network != nil {
		network := make([]map[string]any, 0, len(in.Network))
		for _, e := range in.Network {
			network = append(network, networkEntryBody(e))
		}
		body["network"] = network
	}
	if in.Ingress != nil {
		body["ingress"] = in.Ingress
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
	Register(s, deps, Spec{Name: "user.microvm.list", Description: "List the caller's microVMs, filterable by name, state, image and connector."}, listMicrovms)
	Register(s, deps, Spec{Name: "user.microvm.get", Description: "Get a microVM by UUID, including its recent runs, image versions, domains and env key names (values are never returned)."}, getMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.create", Description: "Create a microVM from a ready image: pick a location, plan, SSH keys, network, lifetime and hooks."}, createMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.pause", Description: "Pause a running microVM (snapshots it on the node, not billed while paused)."}, pauseMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.resume", Description: "Resume a paused microVM."}, resumeMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.stop", Description: "Gracefully stop a microVM. Its hostname and DNS record are kept."}, stopMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.start", Description: "Boot a stopped microVM again."}, startMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.kill", Description: "Forcibly kill the microVM's current run immediately. The row survives; delete removes it. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, killMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.set_timeout", Description: "Overwrite the microVM's auto-pause/kill TTL (used by SDKs to extend a sandbox's life)."}, setMicrovmTimeout)
	Register(s, deps, Spec{Name: "user.microvm.deploy", Description: "Redeploy the microVM onto a specific version of its current image (health-gated before ingress switches)."}, deployMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.rollback", Description: "Roll the microVM back to a previous version of its current image."}, rollbackMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.set_env", Description: "Replace the microVM's environment variables wholesale. Returns only the merged key names, never values."}, setMicrovmEnv)
	Register(s, deps, Spec{Name: "user.microvm.domain_add", Description: "Attach a custom hostname to a microVM for HTTP ingress."}, addMicrovmDomain)
	Register(s, deps, Spec{Name: "user.microvm.domain_remove", Description: "Detach a custom hostname. Traffic to it stops immediately. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, removeMicrovmDomain)
	Register(s, deps, Spec{Name: "user.microvm.logs", Description: "Get recent console/agent log lines for a microVM."}, getMicrovmLogs)
	Register(s, deps, Spec{Name: "user.microvm.metrics", Description: "Get a current point-in-time CPU/memory/disk usage snapshot for a microVM."}, getMicrovmMetrics)
	Register(s, deps, Spec{Name: "user.microvm.delete", Description: "Kill (if running) and permanently delete a microVM, releasing its IPs, VPC interfaces and disks. A mapped static IP is released back to allocated, never freed to another account. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovm)
}
