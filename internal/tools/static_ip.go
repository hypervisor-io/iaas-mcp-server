package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Static IP tools, mirroring the iaas_static_ip provider resource. Allocation
// is synchronous (the response carries the id). There is no individual SHOW
// route, so get lists and filters by id (the client handles this). Deallocation
// is the destructive delete.

func init() {
	toolRegistrars = append(toolRegistrars, registerStaticIPTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

// AllocateStaticIPInput reserves a static IP. ip_id and hypervisor_group_id are
// both required by the controller's AllocateRequest.
type AllocateStaticIPInput struct {
	IPID              string `json:"ip_id" jsonschema:"UUID of the pool IP to reserve"`
	HypervisorGroupID string `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (location) to allocate in"`
}

type GetStaticIPInput struct {
	ID string `json:"id" jsonschema:"UUID of the static IP"`
}

type ListStaticIPsInput struct{}

// DeallocateStaticIPInput releases a static IP. Confirm-gated (destructive).
type DeallocateStaticIPInput struct {
	ID string `json:"id" jsonschema:"UUID of the static IP to deallocate"`
	Confirmation
}

type StaticIPResult struct {
	StaticIP map[string]any `json:"static_ip"`
}

type StaticIPListResult struct {
	StaticIPs []map[string]any `json:"static_ips"`
	Count     int              `json:"count"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func allocateStaticIP(ctx context.Context, cl *client.Client, in AllocateStaticIPInput) (StaticIPResult, error) {
	body := map[string]any{
		"ip_id":               in.IPID,
		"hypervisor_group_id": in.HypervisorGroupID,
	}
	obj, err := cl.AllocateStaticIP(ctx, body)
	if err != nil {
		return StaticIPResult{}, err
	}
	return StaticIPResult{StaticIP: obj}, nil
}

func getStaticIP(ctx context.Context, cl *client.Client, in GetStaticIPInput) (StaticIPResult, error) {
	obj, err := cl.GetStaticIP(ctx, in.ID)
	if err != nil {
		return StaticIPResult{}, err
	}
	return StaticIPResult{StaticIP: obj}, nil
}

func listStaticIPs(ctx context.Context, cl *client.Client, _ ListStaticIPsInput) (StaticIPListResult, error) {
	items, err := cl.ListStaticIPs(ctx)
	if err != nil {
		return StaticIPListResult{}, err
	}
	return StaticIPListResult{StaticIPs: items, Count: len(items)}, nil
}

func deallocateStaticIP(ctx context.Context, cl *client.Client, in DeallocateStaticIPInput) (DeleteResult, error) {
	if err := cl.DeleteStaticIP(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerStaticIPTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name:        "user.static_ip.allocate",
		Description: "Allocate (reserve) a static IP in a hypervisor group.",
	}, allocateStaticIP)

	Register(s, deps, Spec{
		Name:        "user.static_ip.list",
		Description: "List all static IPs owned by the caller.",
	}, listStaticIPs)

	Register(s, deps, Spec{
		Name:        "user.static_ip.get",
		Description: "Get a static IP by UUID.",
	}, getStaticIP)

	Register(s, deps, Spec{
		Name:        "user.static_ip.deallocate",
		Description: "Deallocate (release) a static IP. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deallocateStaticIP)
}
