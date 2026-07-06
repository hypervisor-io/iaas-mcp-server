package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// IP set tools, mirroring the iaas_ip_set provider resource and IpSetController.
// An IP set is a named, ip_version-scoped collection of CIDR entries used by
// security-group rules. All writes are synchronous. Entries are child rows
// added one-by-one (preserving descriptions) or in bulk (CIDR strings only).

func init() {
	toolRegistrars = append(toolRegistrars, registerIPSetTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type CreateIPSetInput struct {
	Name        string `json:"name" jsonschema:"name of the IP set"`
	IPVersion   string `json:"ip_version" jsonschema:"ipv4 or ipv6"`
	Description string `json:"description,omitempty" jsonschema:"optional description"`
}

type GetIPSetInput struct {
	ID string `json:"id" jsonschema:"UUID of the IP set"`
}

type ListIPSetsInput struct{}

type DeleteIPSetInput struct {
	ID string `json:"id" jsonschema:"UUID of the IP set to delete"`
	Confirmation
}

type AddIPSetEntryInput struct {
	IPSetID     string `json:"ip_set_id" jsonschema:"UUID of the IP set"`
	Cidr        string `json:"cidr" jsonschema:"CIDR to add, e.g. 10.0.0.0/24"`
	Description string `json:"description,omitempty" jsonschema:"optional per-entry description"`
}

type BulkAddIPSetInput struct {
	IPSetID string   `json:"ip_set_id" jsonschema:"UUID of the IP set"`
	Cidrs   []string `json:"cidrs" jsonschema:"list of CIDR strings to add (descriptions are not supported in bulk)"`
}

type RemoveIPSetEntryInput struct {
	IPSetID string `json:"ip_set_id" jsonschema:"UUID of the IP set"`
	EntryID string `json:"entry_id" jsonschema:"UUID of the entry to remove"`
	Confirmation
}

type IPSetResult struct {
	IPSet map[string]any `json:"ip_set"`
}

type IPSetListResult struct {
	IPSets []map[string]any `json:"ip_sets"`
	Count  int              `json:"count"`
}

type EntryResult struct {
	Entry map[string]any `json:"entry"`
}

type BulkAddResult struct {
	Created []map[string]any `json:"created"`
	Errors  []map[string]any `json:"errors"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func createIPSet(ctx context.Context, cl *client.Client, in CreateIPSetInput) (IPSetResult, error) {
	body := map[string]any{"name": in.Name, "ip_version": in.IPVersion}
	if in.Description != "" {
		body["description"] = in.Description
	}
	obj, err := cl.CreateIPSet(ctx, body)
	if err != nil {
		return IPSetResult{}, err
	}
	return IPSetResult{IPSet: obj}, nil
}

func getIPSet(ctx context.Context, cl *client.Client, in GetIPSetInput) (IPSetResult, error) {
	obj, err := cl.GetIPSet(ctx, in.ID)
	if err != nil {
		return IPSetResult{}, err
	}
	return IPSetResult{IPSet: obj}, nil
}

func listIPSets(ctx context.Context, cl *client.Client, _ ListIPSetsInput) (IPSetListResult, error) {
	items, err := cl.ListIPSets(ctx)
	if err != nil {
		return IPSetListResult{}, err
	}
	return IPSetListResult{IPSets: items, Count: len(items)}, nil
}

func deleteIPSet(ctx context.Context, cl *client.Client, in DeleteIPSetInput) (DeleteResult, error) {
	if err := cl.DeleteIPSet(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func addIPSetEntry(ctx context.Context, cl *client.Client, in AddIPSetEntryInput) (EntryResult, error) {
	body := map[string]any{"cidr": in.Cidr}
	if in.Description != "" {
		body["description"] = in.Description
	}
	obj, err := cl.AddIPSetEntry(ctx, in.IPSetID, body)
	if err != nil {
		return EntryResult{}, err
	}
	return EntryResult{Entry: obj}, nil
}

func bulkAddIPSet(ctx context.Context, cl *client.Client, in BulkAddIPSetInput) (BulkAddResult, error) {
	env, err := cl.BulkAddIPSetEntries(ctx, in.IPSetID, in.Cidrs)
	if err != nil {
		return BulkAddResult{}, err
	}
	return BulkAddResult{
		Created: asObjectList(env["created"]),
		Errors:  asObjectList(env["errors"]),
	}, nil
}

func removeIPSetEntry(ctx context.Context, cl *client.Client, in RemoveIPSetEntryInput) (OKResult, error) {
	if err := cl.DeleteIPSetEntry(ctx, in.IPSetID, in.EntryID); err != nil {
		return OKResult{}, err
	}
	return okResult("entry removed"), nil
}

func registerIPSetTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.ip_set.create",
		Description: "Create an IP set (a named collection of CIDR entries for firewall rules).",
	}, createIPSet)

	Register(s, deps, Spec{
		Name:        "user.ip_set.list",
		Description: "List all IP sets visible to the caller.",
	}, listIPSets)

	Register(s, deps, Spec{
		Name:        "user.ip_set.get",
		Description: "Get an IP set by UUID, including its CIDR entries.",
	}, getIPSet)

	Register(s, deps, Spec{
		Name:        "user.ip_set.delete",
		Description: "Delete an IP set. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteIPSet)

	Register(s, deps, Spec{
		Name:        "user.ip_set.add_entry",
		Description: "Add a single CIDR entry (with optional description) to an IP set.",
	}, addIPSetEntry)

	Register(s, deps, Spec{
		Name:        "user.ip_set.bulk_add",
		Description: "Add many CIDR entries to an IP set at once (descriptions not supported).",
	}, bulkAddIPSet)

	Register(s, deps, Spec{
		Name:        "user.ip_set.remove_entry",
		Description: "Remove a single CIDR entry from an IP set by entry UUID. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, removeIPSetEntry)
}
