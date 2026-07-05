package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Load balancer tools, mirroring the iaas_load_balancer resource and its child
// resources (frontend, backend, target, certificate, routing_rule). The LB
// create is async (poll status to "active"); delete converges to 404. Children
// are synchronous (the server syncs config internally) and have no SHOW route
// (the client resolves them by scanning the LB). There is no LB update route
// and no sync action, so neither is exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerLoadBalancerTools)
}

// ── load balancer inputs / outputs ──────────────────────────────────────────

type CreateLoadBalancerInput struct {
	Name              string `json:"name" jsonschema:"load balancer name"`
	LBPlanID          string `json:"lb_plan_id" jsonschema:"UUID of the load balancer plan"`
	VPCID             string `json:"vpc_id,omitempty" jsonschema:"optional VPC UUID"`
	VPCSubnetID       string `json:"vpc_subnet_id,omitempty" jsonschema:"VPC subnet UUID (required with vpc_id)"`
	HypervisorGroupID string `json:"hypervisor_group_id,omitempty" jsonschema:"hypervisor group UUID (required without vpc_id)"`
}

type GetLoadBalancerInput struct {
	ID string `json:"id" jsonschema:"UUID of the load balancer"`
}

type ListLoadBalancersInput struct{}

type DeleteLoadBalancerInput struct {
	ID string `json:"id" jsonschema:"UUID of the load balancer to delete"`
	Confirmation
}

type LoadBalancerResult struct {
	LoadBalancer map[string]any `json:"load_balancer"`
}

type LoadBalancerListResult struct {
	LoadBalancers []map[string]any `json:"load_balancers"`
	Count         int              `json:"count"`
}

// ── child inputs / outputs ──────────────────────────────────────────────────

type CreateLBFrontendInput struct {
	LoadBalancerID   string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	Name             string `json:"name" jsonschema:"frontend name"`
	Port             int    `json:"port" jsonschema:"listen port"`
	Protocol         string `json:"protocol,omitempty" jsonschema:"http, https, tcp, or udp"`
	Mode             string `json:"mode,omitempty" jsonschema:"http or tcp"`
	SSLCertificateID string `json:"ssl_certificate_id,omitempty" jsonschema:"UUID of an LB certificate"`
	DefaultBackendID string `json:"default_backend_id,omitempty" jsonschema:"UUID of the default backend"`
	Enabled          *bool  `json:"enabled,omitempty" jsonschema:"whether the frontend is enabled"`
}

type UpdateLBFrontendInput struct {
	LoadBalancerID   string  `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	FrontendID       string  `json:"frontend_id" jsonschema:"UUID of the frontend"`
	Name             *string `json:"name,omitempty"`
	Port             *int    `json:"port,omitempty"`
	Protocol         *string `json:"protocol,omitempty"`
	SSLCertificateID *string `json:"ssl_certificate_id,omitempty"`
	DefaultBackendID *string `json:"default_backend_id,omitempty"`
	Enabled          *bool   `json:"enabled,omitempty"`
}

type LBChildRef struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	ChildID        string `json:"child_id" jsonschema:"UUID of the child resource"`
}

type CreateLBBackendInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	Name           string `json:"name" jsonschema:"backend name"`
	Algorithm      string `json:"algorithm,omitempty" jsonschema:"roundrobin, leastconn, or source"`
	Mode           string `json:"mode,omitempty" jsonschema:"http or tcp"`
}

type CreateLBTargetInput struct {
	LoadBalancerID   string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	BackendID        string `json:"backend_id" jsonschema:"UUID of the parent backend"`
	TargetIP         string `json:"target_ip" jsonschema:"target IP address"`
	TargetPort       int    `json:"target_port" jsonschema:"target port"`
	TargetInstanceID string `json:"target_instance_id,omitempty" jsonschema:"optional instance UUID"`
	Weight           *int   `json:"weight,omitempty" jsonschema:"target weight"`
	Enabled          *bool  `json:"enabled,omitempty" jsonschema:"whether the target is enabled"`
}

type UpdateLBTargetInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	BackendID      string `json:"backend_id" jsonschema:"UUID of the parent backend"`
	TargetID       string `json:"target_id" jsonschema:"UUID of the target"`
	Weight         *int   `json:"weight,omitempty" jsonschema:"new weight"`
	Enabled        *bool  `json:"enabled,omitempty" jsonschema:"enable or disable"`
}

type LBTargetRef struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	BackendID      string `json:"backend_id" jsonschema:"UUID of the parent backend"`
	TargetID       string `json:"target_id" jsonschema:"UUID of the target"`
}

type CreateLBCertificateInput struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	Name           string `json:"name" jsonschema:"certificate name"`
	Certificate    string `json:"certificate" jsonschema:"PEM certificate"`
	PrivateKey     string `json:"private_key" jsonschema:"PEM private key"`
	Chain          string `json:"chain,omitempty" jsonschema:"optional PEM chain"`
}

type CreateLBRoutingRuleInput struct {
	LoadBalancerID  string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	FrontendID      string `json:"frontend_id" jsonschema:"UUID of the parent frontend"`
	LBBackendID     string `json:"lb_backend_id" jsonschema:"UUID of the target backend"`
	MatchValue      string `json:"match_value" jsonschema:"value to match"`
	MatchType       string `json:"match_type,omitempty" jsonschema:"path_prefix, path_exact, host, header, sni, path_beg, or hdr_host"`
	MatchHost       string `json:"match_host,omitempty" jsonschema:"host to match"`
	MatchHeaderName string `json:"match_header_name,omitempty" jsonschema:"header name to match"`
	Priority        *int   `json:"priority,omitempty" jsonschema:"rule priority"`
	Enabled         *bool  `json:"enabled,omitempty" jsonschema:"whether the rule is enabled"`
}

type UpdateLBRoutingRuleInput struct {
	LoadBalancerID string  `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	FrontendID     string  `json:"frontend_id" jsonschema:"UUID of the parent frontend"`
	RuleID         string  `json:"rule_id" jsonschema:"UUID of the rule"`
	LBBackendID    *string `json:"lb_backend_id,omitempty"`
	MatchValue     *string `json:"match_value,omitempty"`
	Priority       *int    `json:"priority,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

type LBRuleRef struct {
	LoadBalancerID string `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	FrontendID     string `json:"frontend_id" jsonschema:"UUID of the parent frontend"`
	RuleID         string `json:"rule_id" jsonschema:"UUID of the rule"`
}

type LBFrontendResult struct {
	Frontend map[string]any `json:"frontend"`
}
type LBBackendResult struct {
	Backend map[string]any `json:"backend"`
}
type LBTargetResult struct {
	Target map[string]any `json:"target"`
}
type LBCertificateResult struct {
	Certificate map[string]any `json:"certificate"`
}
type LBRoutingRuleResult struct {
	Rule map[string]any `json:"rule"`
}

// ── load balancer handlers ──────────────────────────────────────────────────

func createLoadBalancer(ctx context.Context, cl *client.Client, in CreateLoadBalancerInput) (LoadBalancerResult, error) {
	body := map[string]any{"name": in.Name, "lb_plan_id": in.LBPlanID}
	if in.VPCID != "" {
		body["vpc_id"] = in.VPCID
	}
	if in.VPCSubnetID != "" {
		body["vpc_subnet_id"] = in.VPCSubnetID
	}
	if in.HypervisorGroupID != "" {
		body["hypervisor_group_id"] = in.HypervisorGroupID
	}
	created, err := cl.CreateLoadBalancer(ctx, body)
	if err != nil {
		return LoadBalancerResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return LoadBalancerResult{}, fmt.Errorf("create response did not include a load balancer id")
	}
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetLoadBalancer(ctx, id) },
		"status", []string{"active"}, []string{"error"}, defaultCreateTimeout)
	if err != nil {
		return LoadBalancerResult{}, fmt.Errorf("load balancer %s did not become active: %w", id, err)
	}
	obj, err := cl.GetLoadBalancer(ctx, id)
	if err != nil {
		return LoadBalancerResult{}, err
	}
	return LoadBalancerResult{LoadBalancer: obj}, nil
}

func getLoadBalancer(ctx context.Context, cl *client.Client, in GetLoadBalancerInput) (LoadBalancerResult, error) {
	obj, err := cl.GetLoadBalancer(ctx, in.ID)
	if err != nil {
		return LoadBalancerResult{}, err
	}
	return LoadBalancerResult{LoadBalancer: obj}, nil
}

func listLoadBalancers(ctx context.Context, cl *client.Client, _ ListLoadBalancersInput) (LoadBalancerListResult, error) {
	items, err := cl.ListLoadBalancers(ctx)
	if err != nil {
		return LoadBalancerListResult{}, err
	}
	return LoadBalancerListResult{LoadBalancers: items, Count: len(items)}, nil
}

func deleteLoadBalancer(ctx context.Context, cl *client.Client, in DeleteLoadBalancerInput) (DeleteResult, error) {
	if err := cl.DeleteLoadBalancer(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	err := waitForGone(ctx, func() (map[string]any, error) { return cl.GetLoadBalancer(ctx, in.ID) }, defaultDeleteTimeout)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("load balancer %s was not removed: %w", in.ID, err)
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// ── frontend handlers ───────────────────────────────────────────────────────

func createLBFrontend(ctx context.Context, cl *client.Client, in CreateLBFrontendInput) (LBFrontendResult, error) {
	body := map[string]any{"name": in.Name, "port": in.Port}
	if in.Protocol != "" {
		body["protocol"] = in.Protocol
	}
	if in.Mode != "" {
		body["mode"] = in.Mode
	}
	if in.SSLCertificateID != "" {
		body["ssl_certificate_id"] = in.SSLCertificateID
	}
	if in.DefaultBackendID != "" {
		body["default_backend_id"] = in.DefaultBackendID
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.CreateLBFrontend(ctx, in.LoadBalancerID, body)
	if err != nil {
		return LBFrontendResult{}, err
	}
	return LBFrontendResult{Frontend: obj}, nil
}

func getLBFrontend(ctx context.Context, cl *client.Client, in LBChildRef) (LBFrontendResult, error) {
	obj, err := cl.GetLBFrontend(ctx, in.LoadBalancerID, in.ChildID)
	if err != nil {
		return LBFrontendResult{}, err
	}
	return LBFrontendResult{Frontend: obj}, nil
}

func updateLBFrontend(ctx context.Context, cl *client.Client, in UpdateLBFrontendInput) (LBFrontendResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Port != nil {
		body["port"] = *in.Port
	}
	if in.Protocol != nil {
		body["protocol"] = *in.Protocol
	}
	if in.SSLCertificateID != nil {
		body["ssl_certificate_id"] = *in.SSLCertificateID
	}
	if in.DefaultBackendID != nil {
		body["default_backend_id"] = *in.DefaultBackendID
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.UpdateLBFrontend(ctx, in.LoadBalancerID, in.FrontendID, body)
	if err != nil {
		return LBFrontendResult{}, err
	}
	return LBFrontendResult{Frontend: obj}, nil
}

func deleteLBFrontend(ctx context.Context, cl *client.Client, in LBChildRef) (DeleteResult, error) {
	if err := cl.DeleteLBFrontend(ctx, in.LoadBalancerID, in.ChildID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ChildID, Deleted: true}, nil
}

// ── backend handlers ────────────────────────────────────────────────────────

func createLBBackend(ctx context.Context, cl *client.Client, in CreateLBBackendInput) (LBBackendResult, error) {
	body := map[string]any{"name": in.Name}
	if in.Algorithm != "" {
		body["algorithm"] = in.Algorithm
	}
	if in.Mode != "" {
		body["mode"] = in.Mode
	}
	obj, err := cl.CreateLBBackend(ctx, in.LoadBalancerID, body)
	if err != nil {
		return LBBackendResult{}, err
	}
	return LBBackendResult{Backend: obj}, nil
}

func getLBBackend(ctx context.Context, cl *client.Client, in LBChildRef) (LBBackendResult, error) {
	obj, err := cl.GetLBBackend(ctx, in.LoadBalancerID, in.ChildID)
	if err != nil {
		return LBBackendResult{}, err
	}
	return LBBackendResult{Backend: obj}, nil
}

func deleteLBBackend(ctx context.Context, cl *client.Client, in LBChildRef) (DeleteResult, error) {
	if err := cl.DeleteLBBackend(ctx, in.LoadBalancerID, in.ChildID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ChildID, Deleted: true}, nil
}

// ── target handlers ─────────────────────────────────────────────────────────

func createLBTarget(ctx context.Context, cl *client.Client, in CreateLBTargetInput) (LBTargetResult, error) {
	body := map[string]any{"target_ip": in.TargetIP, "target_port": in.TargetPort}
	if in.TargetInstanceID != "" {
		body["target_instance_id"] = in.TargetInstanceID
	}
	if in.Weight != nil {
		body["weight"] = *in.Weight
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.CreateLBTarget(ctx, in.LoadBalancerID, in.BackendID, body)
	if err != nil {
		return LBTargetResult{}, err
	}
	return LBTargetResult{Target: obj}, nil
}

func getLBTarget(ctx context.Context, cl *client.Client, in LBTargetRef) (LBTargetResult, error) {
	obj, err := cl.GetLBTarget(ctx, in.LoadBalancerID, in.BackendID, in.TargetID)
	if err != nil {
		return LBTargetResult{}, err
	}
	return LBTargetResult{Target: obj}, nil
}

func updateLBTarget(ctx context.Context, cl *client.Client, in UpdateLBTargetInput) (LBTargetResult, error) {
	body := map[string]any{}
	if in.Weight != nil {
		body["weight"] = *in.Weight
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.UpdateLBTarget(ctx, in.LoadBalancerID, in.BackendID, in.TargetID, body)
	if err != nil {
		return LBTargetResult{}, err
	}
	return LBTargetResult{Target: obj}, nil
}

func deleteLBTarget(ctx context.Context, cl *client.Client, in LBTargetRef) (DeleteResult, error) {
	if err := cl.DeleteLBTarget(ctx, in.LoadBalancerID, in.BackendID, in.TargetID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.TargetID, Deleted: true}, nil
}

// ── certificate handlers ────────────────────────────────────────────────────

func createLBCertificate(ctx context.Context, cl *client.Client, in CreateLBCertificateInput) (LBCertificateResult, error) {
	body := map[string]any{"name": in.Name, "certificate": in.Certificate, "private_key": in.PrivateKey}
	if in.Chain != "" {
		body["chain"] = in.Chain
	}
	obj, err := cl.CreateLBCertificate(ctx, in.LoadBalancerID, body)
	if err != nil {
		return LBCertificateResult{}, err
	}
	return LBCertificateResult{Certificate: obj}, nil
}

func getLBCertificate(ctx context.Context, cl *client.Client, in LBChildRef) (LBCertificateResult, error) {
	obj, err := cl.GetLBCertificate(ctx, in.LoadBalancerID, in.ChildID)
	if err != nil {
		return LBCertificateResult{}, err
	}
	return LBCertificateResult{Certificate: obj}, nil
}

func deleteLBCertificate(ctx context.Context, cl *client.Client, in LBChildRef) (DeleteResult, error) {
	if err := cl.DeleteLBCertificate(ctx, in.LoadBalancerID, in.ChildID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ChildID, Deleted: true}, nil
}

// ── routing rule handlers ───────────────────────────────────────────────────

func createLBRoutingRule(ctx context.Context, cl *client.Client, in CreateLBRoutingRuleInput) (LBRoutingRuleResult, error) {
	body := map[string]any{"lb_backend_id": in.LBBackendID, "match_value": in.MatchValue}
	if in.MatchType != "" {
		body["match_type"] = in.MatchType
	}
	if in.MatchHost != "" {
		body["match_host"] = in.MatchHost
	}
	if in.MatchHeaderName != "" {
		body["match_header_name"] = in.MatchHeaderName
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.CreateLBRoutingRule(ctx, in.LoadBalancerID, in.FrontendID, body)
	if err != nil {
		return LBRoutingRuleResult{}, err
	}
	return LBRoutingRuleResult{Rule: obj}, nil
}

func getLBRoutingRule(ctx context.Context, cl *client.Client, in LBRuleRef) (LBRoutingRuleResult, error) {
	obj, err := cl.GetLBRoutingRule(ctx, in.LoadBalancerID, in.FrontendID, in.RuleID)
	if err != nil {
		return LBRoutingRuleResult{}, err
	}
	return LBRoutingRuleResult{Rule: obj}, nil
}

func updateLBRoutingRule(ctx context.Context, cl *client.Client, in UpdateLBRoutingRuleInput) (LBRoutingRuleResult, error) {
	body := map[string]any{}
	if in.LBBackendID != nil {
		body["lb_backend_id"] = *in.LBBackendID
	}
	if in.MatchValue != nil {
		body["match_value"] = *in.MatchValue
	}
	if in.Priority != nil {
		body["priority"] = *in.Priority
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	obj, err := cl.UpdateLBRoutingRule(ctx, in.LoadBalancerID, in.FrontendID, in.RuleID, body)
	if err != nil {
		return LBRoutingRuleResult{}, err
	}
	return LBRoutingRuleResult{Rule: obj}, nil
}

func deleteLBRoutingRule(ctx context.Context, cl *client.Client, in LBRuleRef) (DeleteResult, error) {
	if err := cl.DeleteLBRoutingRule(ctx, in.LoadBalancerID, in.FrontendID, in.RuleID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.RuleID, Deleted: true}, nil
}

func registerLoadBalancerTools(s *mcp.Server, deps Deps) {
	// Load balancer.
	Register(s, deps, Spec{Name: "user.load_balancer.create", Description: "Create a load balancer and wait until it is active."}, createLoadBalancer)
	Register(s, deps, Spec{Name: "user.load_balancer.list", Description: "List all load balancers owned by the caller."}, listLoadBalancers)
	Register(s, deps, Spec{Name: "user.load_balancer.get", Description: "Get a load balancer by UUID (with frontends, backends, certificates)."}, getLoadBalancer)
	Register(s, deps, Spec{
		Name:        "user.load_balancer.delete",
		Description: "Delete a load balancer and wait until removed. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteLoadBalancer)

	// Frontends.
	Register(s, deps, Spec{Name: "user.load_balancer.frontend_create", Description: "Add a frontend (listener) to a load balancer."}, createLBFrontend)
	Register(s, deps, Spec{Name: "user.load_balancer.frontend_get", Description: "Get a load balancer frontend."}, getLBFrontend)
	Register(s, deps, Spec{Name: "user.load_balancer.frontend_update", Description: "Update a load balancer frontend."}, updateLBFrontend)
	Register(s, deps, Spec{Name: "user.load_balancer.frontend_delete", Description: "Delete a load balancer frontend."}, deleteLBFrontend)

	// Backends.
	Register(s, deps, Spec{Name: "user.load_balancer.backend_create", Description: "Add a backend pool to a load balancer."}, createLBBackend)
	Register(s, deps, Spec{Name: "user.load_balancer.backend_get", Description: "Get a load balancer backend."}, getLBBackend)
	Register(s, deps, Spec{Name: "user.load_balancer.backend_delete", Description: "Delete a load balancer backend."}, deleteLBBackend)

	// Targets.
	Register(s, deps, Spec{Name: "user.load_balancer.target_create", Description: "Add a target to a backend."}, createLBTarget)
	Register(s, deps, Spec{Name: "user.load_balancer.target_get", Description: "Get a backend target."}, getLBTarget)
	Register(s, deps, Spec{Name: "user.load_balancer.target_update", Description: "Update a backend target's weight or enabled state."}, updateLBTarget)
	Register(s, deps, Spec{Name: "user.load_balancer.target_delete", Description: "Delete a backend target."}, deleteLBTarget)

	// Certificates.
	Register(s, deps, Spec{Name: "user.load_balancer.certificate_create", Description: "Upload a TLS certificate to a load balancer."}, createLBCertificate)
	Register(s, deps, Spec{Name: "user.load_balancer.certificate_get", Description: "Get a load balancer certificate."}, getLBCertificate)
	Register(s, deps, Spec{Name: "user.load_balancer.certificate_delete", Description: "Delete a load balancer certificate."}, deleteLBCertificate)

	// Routing rules.
	Register(s, deps, Spec{Name: "user.load_balancer.routing_rule_create", Description: "Add a routing rule to a frontend."}, createLBRoutingRule)
	Register(s, deps, Spec{Name: "user.load_balancer.routing_rule_get", Description: "Get a frontend routing rule."}, getLBRoutingRule)
	Register(s, deps, Spec{Name: "user.load_balancer.routing_rule_update", Description: "Update a frontend routing rule."}, updateLBRoutingRule)
	Register(s, deps, Spec{Name: "user.load_balancer.routing_rule_delete", Description: "Delete a frontend routing rule."}, deleteLBRoutingRule)
}
