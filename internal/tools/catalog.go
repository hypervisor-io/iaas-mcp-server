package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Catalog tools: read-only lookups that back resource inputs (locations, plans,
// images, ISOs, and the Kubernetes catalog searches). These mirror the
// provider's data sources. All are read-only, so none are confirm-gated and
// none mutate anything.

func init() {
	toolRegistrars = append(toolRegistrars, registerCatalogTools)
}

// CatalogListResult is the shared output for every catalog lookup: the raw list
// plus its count. Items are returned verbatim so the agent can match on any
// field (id, name, slug, specs).
type CatalogListResult struct {
	Items []map[string]any `json:"items"`
	Count int              `json:"count"`
}

func catalogResult(items []map[string]any, err error) (CatalogListResult, error) {
	if err != nil {
		return CatalogListResult{}, err
	}
	return CatalogListResult{Items: items, Count: len(items)}, nil
}

type EmptyInput struct{}

type QueryInput struct {
	Query string `json:"query,omitempty" jsonschema:"optional substring filter"`
}

type PlanGroupsInput struct {
	LocationID string `json:"location_id" jsonschema:"UUID of the location"`
}

type PlansInput struct {
	LocationID  string `json:"location_id" jsonschema:"UUID of the location"`
	PlanGroupID string `json:"plan_group_id" jsonschema:"UUID of the plan group"`
}

type ImagesInput struct {
	Query             string `json:"query,omitempty" jsonschema:"optional substring filter"`
	HypervisorGroupID string `json:"hypervisor_group_id,omitempty" jsonschema:"optional hypervisor group UUID to scope images"`
}

type K8sVpcsInput struct {
	HypervisorGroupID string `json:"hypervisor_group_id,omitempty" jsonschema:"optional hypervisor group UUID"`
	Query             string `json:"query,omitempty" jsonschema:"optional substring filter"`
}

type K8sSubnetsInput struct {
	VPCID string `json:"vpc_id" jsonschema:"UUID of the VPC (required)"`
	Type  string `json:"type,omitempty" jsonschema:"optional subnet type: private or public"`
	Query string `json:"query,omitempty" jsonschema:"optional substring filter"`
}

func registerCatalogTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.catalog.locations", Description: "List Cloud Service locations (regions)."},
		func(ctx context.Context, cl *client.Client, _ EmptyInput) (CatalogListResult, error) {
			return catalogResult(cl.ListLocations(ctx))
		})
	Register(s, deps, Spec{Name: "user.catalog.plan_groups", Description: "List instance plan groups in a location."},
		func(ctx context.Context, cl *client.Client, in PlanGroupsInput) (CatalogListResult, error) {
			return catalogResult(cl.ListPlanGroups(ctx, in.LocationID))
		})
	Register(s, deps, Spec{Name: "user.catalog.plans", Description: "List instance plans in a location's plan group."},
		func(ctx context.Context, cl *client.Client, in PlansInput) (CatalogListResult, error) {
			return catalogResult(cl.ListPlans(ctx, in.LocationID, in.PlanGroupID))
		})
	Register(s, deps, Spec{Name: "user.catalog.images", Description: "Search OS images (optionally scoped to a hypervisor group)."},
		func(ctx context.Context, cl *client.Client, in ImagesInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchImages(ctx, in.Query, in.HypervisorGroupID))
		})
	Register(s, deps, Spec{Name: "user.catalog.isos", Description: "List available ISOs."},
		func(ctx context.Context, cl *client.Client, in QueryInput) (CatalogListResult, error) {
			return catalogResult(cl.ListISOs(ctx, in.Query))
		})

	// Kubernetes catalog searches.
	Register(s, deps, Spec{Name: "user.catalog.k8s_versions", Description: "Search available Kubernetes versions."},
		func(ctx context.Context, cl *client.Client, in QueryInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sVersions(ctx, in.Query))
		})
	Register(s, deps, Spec{Name: "user.catalog.k8s_regions", Description: "Search Kubernetes-enabled regions."},
		func(ctx context.Context, cl *client.Client, in QueryInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sRegions(ctx, in.Query))
		})
	Register(s, deps, Spec{Name: "user.catalog.k8s_worker_plans", Description: "Search Kubernetes worker node plans."},
		func(ctx context.Context, cl *client.Client, in QueryInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sWorkerPlans(ctx, in.Query))
		})
	Register(s, deps, Spec{Name: "user.catalog.k8s_control_plane_plans", Description: "Search Kubernetes control-plane node plans."},
		func(ctx context.Context, cl *client.Client, in QueryInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sControlPlanePlans(ctx, in.Query))
		})
	Register(s, deps, Spec{Name: "user.catalog.k8s_load_balancer_plans", Description: "Search Kubernetes load balancer plans."},
		func(ctx context.Context, cl *client.Client, in QueryInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sLoadBalancerPlans(ctx, in.Query))
		})
	Register(s, deps, Spec{Name: "user.catalog.k8s_vpcs", Description: "Search VPCs available for Kubernetes clusters."},
		func(ctx context.Context, cl *client.Client, in K8sVpcsInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sVpcs(ctx, in.HypervisorGroupID, in.Query))
		})
	Register(s, deps, Spec{Name: "user.catalog.k8s_subnets", Description: "Search subnets of a VPC for Kubernetes clusters."},
		func(ctx context.Context, cl *client.Client, in K8sSubnetsInput) (CatalogListResult, error) {
			return catalogResult(cl.SearchK8sSubnets(ctx, in.VPCID, in.Type, in.Query))
		})
}
