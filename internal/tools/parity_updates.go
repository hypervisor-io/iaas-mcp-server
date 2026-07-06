package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Metadata-update tools and vpc delete, closing coverage gaps for provider
// parity. These reuse existing client update/delete methods and the family
// result types. Grouped here (each with its own registrar) to keep the change
// additive; the tool names still follow user.<family>.<action>.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityUpdateTools)
}

// ── inputs ───────────────────────────────────────────────────────────────────

type UpdateInstanceInput struct {
	ID          string  `json:"id" jsonschema:"UUID of the instance"`
	DisplayName *string `json:"display_name,omitempty" jsonschema:"new display name"`
	Hostname    *string `json:"hostname,omitempty" jsonschema:"new hostname"`
	Notes       *string `json:"notes,omitempty" jsonschema:"new notes"`
	Boot        *string `json:"boot,omitempty" jsonschema:"boot device order"`
}

type UpdateIPSetInput struct {
	ID          string  `json:"id" jsonschema:"UUID of the IP set"`
	Name        *string `json:"name,omitempty" jsonschema:"new name"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
	IPVersion   *string `json:"ip_version,omitempty" jsonschema:"ipv4 or ipv6"`
}

type UpdateSecurityGroupInput struct {
	ID          string  `json:"id" jsonschema:"UUID of the security group"`
	Name        string  `json:"name" jsonschema:"security group name (required by the API even on update)"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
}

type UpdateKubernetesClusterInput struct {
	ID          string  `json:"id" jsonschema:"UUID of the cluster"`
	Name        *string `json:"name,omitempty" jsonschema:"new name"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
	ProjectID   *string `json:"project_id,omitempty" jsonschema:"new project UUID"`
	Idempotent
}

type UpdateNatGatewayInput struct {
	VPCID      string  `json:"vpc_id" jsonschema:"UUID of the VPC"`
	ID         string  `json:"id" jsonschema:"UUID of the NAT gateway"`
	Name       *string `json:"name,omitempty" jsonschema:"new name"`
	NatEnabled *bool   `json:"nat_enabled,omitempty" jsonschema:"enable or disable NAT"`
}

type UpdateLBBackendInput struct {
	LoadBalancerID string  `json:"load_balancer_id" jsonschema:"UUID of the load balancer"`
	BackendID      string  `json:"backend_id" jsonschema:"UUID of the backend"`
	Name           *string `json:"name,omitempty" jsonschema:"new name"`
	Algorithm      *string `json:"algorithm,omitempty" jsonschema:"roundrobin, leastconn, or source"`
	Mode           *string `json:"mode,omitempty" jsonschema:"http or tcp"`
}

type DeleteVPCInput struct {
	ID string `json:"id" jsonschema:"UUID of the VPC to delete"`
	Confirmation
}

// ── handlers ─────────────────────────────────────────────────────────────────

func updateInstance(ctx context.Context, cl *client.Client, in UpdateInstanceInput) (InstanceResult, error) {
	fields := map[string]any{}
	if in.DisplayName != nil {
		fields["display_name"] = *in.DisplayName
	}
	if in.Hostname != nil {
		fields["hostname"] = *in.Hostname
	}
	if in.Notes != nil {
		fields["notes"] = *in.Notes
	}
	if in.Boot != nil {
		fields["boot"] = *in.Boot
	}
	obj, err := cl.UpdateInstance(ctx, in.ID, fields)
	if err != nil {
		return InstanceResult{}, err
	}
	return InstanceResult{Instance: obj}, nil
}

func updateIPSet(ctx context.Context, cl *client.Client, in UpdateIPSetInput) (IPSetResult, error) {
	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if in.IPVersion != nil {
		fields["ip_version"] = *in.IPVersion
	}
	if _, err := cl.UpdateIPSet(ctx, in.ID, fields); err != nil {
		return IPSetResult{}, err
	}
	// Update returns a bare envelope; read the fresh object back.
	obj, err := cl.GetIPSet(ctx, in.ID)
	if err != nil {
		return IPSetResult{}, err
	}
	return IPSetResult{IPSet: obj}, nil
}

func updateSecurityGroup(ctx context.Context, cl *client.Client, in UpdateSecurityGroupInput) (SecurityGroupResult, error) {
	fields := map[string]any{"name": in.Name}
	if in.Description != nil {
		fields["description"] = *in.Description
	}
	if _, err := cl.UpdateSecurityGroup(ctx, in.ID, fields); err != nil {
		return SecurityGroupResult{}, err
	}
	// Update returns a bare envelope; read the full object (with rules and
	// attached instances) back.
	env, err := cl.GetSecurityGroupEnvelope(ctx, in.ID)
	if err != nil {
		return SecurityGroupResult{}, err
	}
	sg, _ := env["security_group"].(map[string]any)
	return SecurityGroupResult{SecurityGroup: sg, AttachedInstances: asObjectList(env["attached_instances"])}, nil
}

func updateKubernetesCluster(ctx context.Context, cl *client.Client, in UpdateKubernetesClusterInput) (KubernetesClusterResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Description != nil {
		body["description"] = *in.Description
	}
	if in.ProjectID != nil {
		body["project_id"] = *in.ProjectID
	}
	obj, err := cl.UpdateKubernetesCluster(ctx, in.ID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return KubernetesClusterResult{}, err
	}
	return KubernetesClusterResult{Cluster: obj}, nil
}

func updateNatGateway(ctx context.Context, cl *client.Client, in UpdateNatGatewayInput) (NatGatewayResult, error) {
	fields := map[string]any{}
	if in.Name != nil {
		fields["name"] = *in.Name
	}
	if in.NatEnabled != nil {
		fields["nat_enabled"] = *in.NatEnabled
	}
	obj, err := cl.UpdateNatGateway(ctx, in.VPCID, in.ID, fields)
	if err != nil {
		return NatGatewayResult{}, err
	}
	return NatGatewayResult{Gateway: obj}, nil
}

func updateLBBackend(ctx context.Context, cl *client.Client, in UpdateLBBackendInput) (LBBackendResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Algorithm != nil {
		body["algorithm"] = *in.Algorithm
	}
	if in.Mode != nil {
		body["mode"] = *in.Mode
	}
	obj, err := cl.UpdateLBBackend(ctx, in.LoadBalancerID, in.BackendID, body)
	if err != nil {
		return LBBackendResult{}, err
	}
	return LBBackendResult{Backend: obj}, nil
}

func deleteVPC(ctx context.Context, cl *client.Client, in DeleteVPCInput) (DeleteResult, error) {
	if err := cl.DeleteVPC(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerParityUpdateTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.instance.update", Description: "Update an instance's metadata (display name, hostname, notes, boot order)."}, updateInstance)
	Register(s, deps, Spec{Name: "user.ip_set.update", Description: "Update an IP set's name, description, or ip_version."}, updateIPSet)
	Register(s, deps, Spec{Name: "user.security_group.update", Description: "Update a security group's name or description."}, updateSecurityGroup)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.update", Description: "Update a Kubernetes cluster's name, description, or project."}, updateKubernetesCluster)
	Register(s, deps, Spec{Name: "user.nat_gateway.update", Description: "Update a NAT gateway's name or NAT-enabled flag."}, updateNatGateway)
	Register(s, deps, Spec{Name: "user.load_balancer.backend_update", Description: "Update a load balancer backend's name, algorithm, or mode."}, updateLBBackend)
	Register(s, deps, Spec{
		Name:        "user.vpc.delete",
		Description: "Delete a VPC. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVPC)
}
