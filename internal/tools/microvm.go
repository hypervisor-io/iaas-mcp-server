package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	Search      string            `json:"search,omitempty" jsonschema:"filter by name substring"`
	State       string            `json:"state,omitempty" jsonschema:"filter by state: creating, running, paused, stopped, killed, error"`
	ImageID     string            `json:"image_id,omitempty" jsonschema:"filter by image UUID"`
	ConnectorID string            `json:"connector_id,omitempty" jsonschema:"filter by owning connector UUID"`
	Metadata    map[string]string `json:"metadata,omitempty" jsonschema:"filter by customer metadata: every key=value pair must match exactly (AND)"`
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
	SecurityGroupIDs []string `json:"security_group_ids,omitempty" jsonschema:"same as the top-level security_group_ids, kept for existing clients. Groups apply to the whole sandbox, not to one NIC: every network entry must name the same set, otherwise the request is refused (422 security_groups_differ_per_nic). Prefer the top-level field"`
	RateMbit         int      `json:"rate_mbit,omitempty" jsonschema:"per-NIC rate limit in Mbit/s"`
}

// MicrovmVolumeInput is one entry of a Create MicroVM `volumes[]` array: a persistent data volume the sandbox starts with.
type MicrovmVolumeInput struct {
	VolumeID              string `json:"volume_id" jsonschema:"UUID of a free sandbox volume (user.volume.create with for sandbox) in location_id"`
	MountPath             string `json:"mount_path,omitempty" jsonschema:"absolute path where the guest mounts it: letters, digits and . _ + - /, not inside a system directory. Default /mnt/<volume name>"`
	ReadOnly              *bool  `json:"read_only,omitempty" jsonschema:"mount read-only"`
	Noexec                *bool  `json:"noexec,omitempty" jsonschema:"mount with noexec"`
	DeleteOnSandboxDelete *bool  `json:"delete_on_sandbox_delete,omitempty" jsonschema:"delete the volume when the sandbox is deleted (default keeps it with its data)"`
}

// CreateMicrovmInput carries the Create MicroVM body. "ingress" and
// "lifecycle_hooks" are passed through verbatim; unset optional fields are
// omitted so the API applies its own defaults.
type CreateMicrovmInput struct {
	LocationID         string                     `json:"location_id,omitempty" jsonschema:"UUID of the location to deploy into. Required unless snapshot_id is given (a snapshot fixes the location)"`
	ImageID            string                     `json:"image_id,omitempty" jsonschema:"UUID of a ready image visible to the account. Required unless snapshot_id is given; the two exclude each other"`
	SnapshotID         string                     `json:"snapshot_id,omitempty" jsonschema:"UUID of a ready sandbox disk snapshot (user.microvm.snapshot_list) to start the sandbox from instead of an image: it boots cold from a copy of the saved disk, in the snapshot's location and on the node that holds it. plan_id must hold the snapshot's root filesystem by the node's rule: any plan when that filesystem is 2 GiB or less, otherwise a plan disk of at least its size in whole GiB (disk_gib is the source's plan, not the test). Environment variables, secrets and the outbound policy are not inherited: set env and egress_policy here, or inherit_env"`
	InheritEnv         *bool                      `json:"inherit_env,omitempty" jsonschema:"with snapshot_id: also start with the source sandbox's current environment variables (env wins on a clash); fails if the source no longer exists"`
	FastStart          string                     `json:"fast_start,omitempty" jsonschema:"how the sandbox should start: allow (default) takes the fastest start the location offers and otherwise starts normally; cold_only always starts normally; require never starts normally and fails with HTTP 429 reason fast_start_unavailable (a cause such as pool_empty, snapshot_building, restore_saturated, node_kvm_fault, fast_start_unavailable or fast_start_unsupported, and a Retry-After when a retry is likely to help). Nothing is created on a 429, so the same call can be repeated. require is refused at once (cause fast_start_unsupported) for a snapshot fork, MCP servers, more than one network interface, a sandbox without shell ingress, or a location without fast start"`
	Name               string                     `json:"name" jsonschema:"lowercase letters, digits and dashes, max 63, unique within the account"`
	ImageVersionID     string                     `json:"image_version_id,omitempty" jsonschema:"UUID of the image version to run, defaults to the image's current version"`
	PlanID             string                     `json:"plan_id,omitempty" jsonschema:"UUID of a plan available at location_id. Its disk must hold the image (required_disk_gib on the image from user.microvm.image.get/list); a smaller plan is refused with reason plan_disk_too_small and nothing is created (an image of 2 GiB or less fits any plan). Defaults to the smallest enabled plan that holds the image"`
	SecurityGroupIDs   []string                   `json:"security_group_ids,omitempty" jsonschema:"security groups owned by the account (at most 10) that apply to the whole sandbox, every network interface, and are in force before the sandbox can receive traffic. Without groups the sandbox is open on every port. Change them later with user.security_group.attach_sandboxes / detach_sandboxes; the groups are returned as security_groups on every read"`
	SSHKeyIDs          []string                   `json:"ssh_key_ids,omitempty" jsonschema:"SSH keys owned by the account (C3), installed to /root/.ssh/authorized_keys and every non-system user's authorized_keys"`
	Volumes            []MicrovmVolumeInput       `json:"volumes,omitempty" jsonschema:"persistent data volumes (at most 4) the sandbox starts with, all free, in location_id and on ONE node: the sandbox is placed on that node and never elsewhere. They keep their data when the sandbox stops, pauses or is deleted. Snapshots of the sandbox do not contain them. Refused with a typed reason: volume_not_found, volume_in_use, volume_node_mismatch, volume_node_unavailable, volume_slots_exhausted, mount_path_conflict, mount_path_invalid"`
	Network            []MicrovmNetworkEntryInput `json:"network,omitempty" jsonschema:"one or more network interfaces: {kind: public} (a public IPv4 subnet is assigned automatically, C8.1), {kind: public, static_ip_id} (map an allocated static IP, C8.3), or {kind: vpc, vpc_subnet_id}. The first entry becomes eth0 and decides the default route. Omit entirely to use the account's stored default (user.microvm.settings.get); with no default set, the request is rejected"`
	Ingress            map[string]any             `json:"ingress,omitempty" jsonschema:"{http: {enabled, port?, health_path?, extra_ports?}, shell: {enabled}} - shell requires an envd-capable image"`
	MaxLifetimeSeconds int                        `json:"max_lifetime_seconds,omitempty" jsonschema:"hard lifetime cap in seconds, from the last start"`
	OnTimeout          string                     `json:"on_timeout,omitempty" jsonschema:"pause or kill when the lifetime/idle timeout elapses, default pause"`
	IdleTimeoutSeconds int                        `json:"idle_timeout_seconds,omitempty" jsonschema:"seconds of inactivity before pausing; omit to never idle-pause"`
	AlwaysOn           *bool                      `json:"always_on,omitempty" jsonschema:"disable lifetime/idle timeouts entirely"`
	Env                map[string]string          `json:"env,omitempty" jsonschema:"environment variables the microVM starts with, as an object of variable name (uppercase letters, digits, underscores) to value; max 100 variables, each value up to 8192 characters, kept exactly as sent; stored encrypted and never returned"`
	LifecycleHooks     map[string]any             `json:"lifecycle_hooks,omitempty" jsonschema:"hooks {port?, run|resume|suspend|terminate: {enabled, timeout, payload?}}"`
	Domain             string                     `json:"domain,omitempty" jsonschema:"custom hostname to attach immediately after creation"`
	Secure             *bool                      `json:"secure,omitempty" jsonschema:"gate envd access behind the microVM's access token"`
	EgressPolicy       *MicrovmEgressPolicyInput  `json:"egress_policy,omitempty" jsonschema:"outbound network policy the sandbox starts with (same document as user.microvm.egress.set). It is in force from the first boot and the sandbox is placed only on a node that can enforce it. Omit for unrestricted outbound access"`
	Metadata           map[string]string          `json:"metadata,omitempty" jsonschema:"customer metadata, string keys to string values: at most 32 keys, key 1-64 characters of letters digits _ . -, value at most 256 characters, keys starting with vc. are reserved"`
}

type SetMicrovmTimeoutInput struct {
	ID      string `json:"id" jsonschema:"UUID of the microVM"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"seconds from now until the on_timeout action fires, minimum 1, at most the platform maximum (28800 by default); give this OR no_limit"`
	NoLimit bool   `json:"no_limit,omitempty" jsonschema:"true removes the deadline: the microVM then has no lifetime cap and is billed for as long as it runs; give this OR timeout"`
}

// SetMicrovmMetadataInput merge-patches the customer metadata: a string sets
// the key, null removes it, keys not named are untouched.
type SetMicrovmMetadataInput struct {
	ID       string             `json:"id" jsonschema:"UUID of the microVM"`
	Metadata map[string]*string `json:"metadata" jsonschema:"merge patch: key to string value, or null to remove the key; the same limits as create apply (at most 32 keys after the merge)"`
}

type MicrovmMetadataResult struct {
	Metadata map[string]any `json:"metadata"`
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
	items, err := cl.ListMicrovmsByMetadata(ctx, in.Search, in.State, in.ImageID, in.ConnectorID, in.Metadata)
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
	body := map[string]any{"name": in.Name}
	if in.LocationID != "" {
		body["location_id"] = in.LocationID
	}
	if in.ImageID != "" {
		body["image_id"] = in.ImageID
	}
	if in.SnapshotID != "" {
		body["snapshot_id"] = in.SnapshotID
	}
	if in.InheritEnv != nil {
		body["inherit_env"] = *in.InheritEnv
	}
	if in.FastStart != "" {
		body["fast_start"] = in.FastStart
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
	if len(in.SecurityGroupIDs) > 0 {
		body["security_group_ids"] = in.SecurityGroupIDs
	}
	if len(in.Volumes) > 0 {
		volumes := make([]map[string]any, 0, len(in.Volumes))
		for _, v := range in.Volumes {
			item := map[string]any{"volume_id": v.VolumeID}
			if v.MountPath != "" {
				item["mount_path"] = v.MountPath
			}
			if v.ReadOnly != nil {
				item["read_only"] = *v.ReadOnly
			}
			if v.Noexec != nil {
				item["noexec"] = *v.Noexec
			}
			if v.DeleteOnSandboxDelete != nil {
				item["delete_on_sandbox_delete"] = *v.DeleteOnSandboxDelete
			}
			volumes = append(volumes, item)
		}
		body["volumes"] = volumes
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
	if len(in.Env) > 0 {
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
	if in.EgressPolicy != nil {
		body["egress_policy"] = egressPolicyBody(*in.EgressPolicy)
	}
	if len(in.Metadata) > 0 {
		body["metadata"] = in.Metadata
	}
	// cl.CreateMicrovm sends a fresh Idempotency-Key per call (kept across the
	// transport's own retries), so a retried create is replayed, not repeated. The env
	// fallback's second create is a new call and so gets a new key.
	microvm, envDeferred, err := createMicrovmWithEnvFallback(ctx, cl, body)
	if err != nil {
		return MicrovmResult{}, err
	}
	if len(in.Env) > 0 {
		id, _ := microvm["id"].(string)
		if envDeferred || !microvmEnvApplied(ctx, cl, id, in.Env) {
			// An older Master refused the map (retried without env) or ignored it: set the
			// variables through the env endpoint, the pre-3381 sequence.
			if _, err := cl.SetMicrovmEnv(ctx, id, in.Env); err != nil {
				return MicrovmResult{}, err
			}
		}
	}
	return MicrovmResult{Microvm: microvm}, nil
}

// createMicrovmWithEnvFallback creates the MicroVM with env in the body. A Master older than
// the create-env fix validates env as a string, so an object map is refused with a 422 whose
// errors name only env (and creates no row). In exactly that case the create is retried ONCE
// without env and the caller sets the variables through PUT /microvm/vm/{id}/env. A 422 about
// any other field, or a failure of the retry, is returned unchanged.
//
// If create ever carries an Idempotency-Key, the retry MUST use a new key: the first request
// was refused and a replayed key could return that refusal again.
func createMicrovmWithEnvFallback(ctx context.Context, cl *client.Client, body map[string]any) (map[string]any, bool, error) {
	microvm, err := cl.CreateMicrovm(ctx, body)
	if err == nil {
		return microvm, false, nil
	}
	if _, hasEnv := body["env"]; !hasEnv || !isEnvOnlyValidationError(err) {
		return nil, false, err
	}
	retry := make(map[string]any, len(body))
	for k, v := range body {
		if k != "env" {
			retry[k] = v
		}
	}
	microvm, err = cl.CreateMicrovm(ctx, retry)
	if err != nil {
		return nil, false, err
	}
	return microvm, true, nil
}

// isEnvOnlyValidationError is true for a 422 whose field errors name env (or env.*) and
// nothing else.
func isEnvOnlyValidationError(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnprocessableEntity || len(apiErr.FieldErrors) == 0 {
		return false
	}
	for field := range apiErr.FieldErrors {
		if field != "env" && !strings.HasPrefix(field, "env.") {
			return false
		}
	}
	return true
}

// microvmEnvApplied reports whether the SHOW envelope's env_keys name every configured
// variable. A read failure counts as not applied so the PUT runs (it is idempotent).
func microvmEnvApplied(ctx context.Context, cl *client.Client, id string, env map[string]string) bool {
	details, err := cl.GetMicrovm(ctx, id)
	if err != nil {
		return false
	}
	have := map[string]bool{}
	if arr, ok := details["env_keys"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				have[s] = true
			}
		}
	}
	for name := range env {
		if !have[name] {
			return false
		}
	}
	return true
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
	switch {
	case in.NoLimit && in.Timeout != 0:
		return OKResult{}, fmt.Errorf("give timeout or no_limit, not both")
	case in.NoLimit:
		if err := cl.RemoveMicrovmTimeout(ctx, in.ID); err != nil {
			return OKResult{}, err
		}
		return okResult("deadline removed"), nil
	case in.Timeout < 1:
		return OKResult{}, fmt.Errorf("timeout must be at least 1 second, or set no_limit to remove the deadline")
	}
	if err := cl.SetMicrovmTimeout(ctx, in.ID, in.Timeout); err != nil {
		return OKResult{}, err
	}
	return okResult("timeout updated"), nil
}

func setMicrovmMetadata(ctx context.Context, cl *client.Client, in SetMicrovmMetadataInput) (MicrovmMetadataResult, error) {
	patch := make(map[string]any, len(in.Metadata))
	for k, v := range in.Metadata {
		if v == nil {
			patch[k] = nil
		} else {
			patch[k] = *v
		}
	}
	stored, err := cl.UpdateMicrovmMetadata(ctx, in.ID, patch)
	if err != nil {
		return MicrovmMetadataResult{}, err
	}
	return MicrovmMetadataResult{Metadata: stored}, nil
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
	Register(s, deps, Spec{Name: "user.microvm.get", Description: "Get a microVM by UUID, including its recent runs, image versions, domains and env key names (values are never returned). Read-only resume fields on the microvm: last_resume_mode is snapshot (memory kept) or fresh_boot (restarted from disk, in-memory state not kept), last_resume_reason is a short machine reason for a fresh boot, last_resumed_at is when; all null until the first resume. Each run also carries resume_mode, resume_reason and resumed_at."}, getMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.create", Description: "Create a microVM from a ready image, or from a sandbox disk snapshot (snapshot_id, see user.microvm.snapshot_create): pick a location (not needed with a snapshot), plan, SSH keys, network, lifetime and hooks, optionally an outbound network policy (egress_policy) and persistent data volumes (volumes, see user.volume.create with for sandbox) it starts with. A plan whose disk is smaller than the image needs (required_disk_gib) is refused with reason plan_disk_too_small; omit plan_id to get the smallest plan that fits."}, createMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.pause", Description: "Pause a running microVM (snapshots it on the node, not billed while paused)."}, pauseMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.resume", Description: "Resume a paused microVM."}, resumeMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.stop", Description: "Gracefully stop a microVM. Its hostname and DNS record are kept."}, stopMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.start", Description: "Boot a stopped microVM again."}, startMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.kill", Description: "Forcibly kill the microVM's current run immediately. The row survives; delete removes it. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, killMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.set_timeout", Description: "Overwrite the microVM's auto-pause/kill TTL (used by SDKs to extend a sandbox's life), or pass no_limit to remove the deadline entirely."}, setMicrovmTimeout)
	Register(s, deps, Spec{Name: "user.microvm.set_metadata", Description: "Merge-patch the microVM's customer metadata (string keys to string values; null removes a key). Returns the full stored map."}, setMicrovmMetadata)
	Register(s, deps, Spec{Name: "user.microvm.deploy", Description: "Redeploy the microVM onto a specific version of its current image (health-gated before ingress switches)."}, deployMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.rollback", Description: "Roll the microVM back to a previous version of its current image."}, rollbackMicrovm)
	Register(s, deps, Spec{Name: "user.microvm.set_env", Description: "Replace the microVM's environment variables wholesale. Returns only the merged key names, never values."}, setMicrovmEnv)
	Register(s, deps, Spec{Name: "user.microvm.domain_add", Description: "Attach a custom hostname to a microVM for HTTP ingress."}, addMicrovmDomain)
	Register(s, deps, Spec{Name: "user.microvm.domain_remove", Description: "Detach a custom hostname. Traffic to it stops immediately. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, removeMicrovmDomain)
	Register(s, deps, Spec{Name: "user.microvm.logs", Description: "Get recent console/agent log lines for a microVM."}, getMicrovmLogs)
	Register(s, deps, Spec{Name: "user.microvm.metrics", Description: "Get a current point-in-time CPU/memory/disk usage snapshot for a microVM."}, getMicrovmMetrics)
	Register(s, deps, Spec{Name: "user.microvm.delete", Description: "Kill (if running) and permanently delete a microVM, releasing its IPs, VPC interfaces and disks. A mapped static IP is released back to allocated, never freed to another account. DESTRUCTIVE: requires \"confirm\": true.", Destructive: true}, deleteMicrovm)
}
