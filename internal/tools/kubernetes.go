package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Kubernetes tools, mirroring the iaas_kubernetes_cluster resource and its
// children (node_pool, ssl_certificate, security_group_rule) plus the
// kubeconfig and autoscaler-manifest data sources. Cluster create is async
// (poll STATE to "running", fail "error"); delete converges to 404. The k8s
// create/update/delete/upgrade routes carry the idempotency.user middleware, so
// these inputs accept an optional idempotency_key (threaded via the framework
// seam) and pass it to the client, which falls back to a generated UUID.
//
// acknowledge_error and the cluster-level retry route have no client method, so
// they are not exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerKubernetesTools)
}

// ── cluster inputs / outputs ────────────────────────────────────────────────

type CreateKubernetesClusterInput struct {
	Name                 string `json:"name" jsonschema:"cluster name"`
	Slug                 string `json:"slug" jsonschema:"cluster slug"`
	HypervisorGroupID    string `json:"hypervisor_group_id" jsonschema:"UUID of the hypervisor group (region)"`
	VPCID                string `json:"vpc_id" jsonschema:"UUID of the VPC"`
	CPVPCSubnetID        string `json:"cp_vpc_subnet_id" jsonschema:"UUID of the control-plane subnet (must be private)"`
	WorkerVPCSubnetID    string `json:"worker_vpc_subnet_id" jsonschema:"UUID of the worker subnet"`
	KubernetesVersionID  string `json:"kubernetes_version_id" jsonschema:"UUID of the Kubernetes version"`
	ControlNodeCount     int    `json:"control_node_count" jsonschema:"number of control-plane nodes (1 or 3)"`
	EndpointMode         string `json:"endpoint_mode" jsonschema:"private or public_and_private"`
	CPInstancePlanID     string `json:"cp_instance_plan_id" jsonschema:"UUID of the control-plane node plan"`
	CPLBPlanID           string `json:"cp_lb_plan_id" jsonschema:"UUID of the control-plane load balancer plan"`
	WorkerInstancePlanID string `json:"worker_instance_plan_id" jsonschema:"UUID of the worker node plan"`
	Description          string `json:"description,omitempty" jsonschema:"optional description"`
	ProjectID            string `json:"project_id,omitempty" jsonschema:"optional project UUID"`
	PodCIDR              string `json:"pod_cidr,omitempty" jsonschema:"optional pod CIDR"`
	ServiceCIDR          string `json:"service_cidr,omitempty" jsonschema:"optional service CIDR"`
	LBHAEnabled          *bool  `json:"lb_ha_enabled,omitempty" jsonschema:"optional control-plane LB high availability"`
	WorkerCount          *int   `json:"worker_count,omitempty" jsonschema:"optional initial worker count"`
	Idempotent
}

type GetKubernetesClusterInput struct {
	ID string `json:"id" jsonschema:"UUID of the cluster"`
}

type ListKubernetesClustersInput struct{}

type DeleteKubernetesClusterInput struct {
	ID string `json:"id" jsonschema:"UUID of the cluster to delete"`
	Confirmation
	Idempotent
}

type UpgradeK8sControlPlaneInput struct {
	ClusterID        string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	TargetVersionID  string `json:"target_version_id" jsonschema:"UUID of the target Kubernetes version"`
	DrainGracePeriod *int   `json:"drain_grace_period,omitempty" jsonschema:"optional drain grace period (seconds)"`
	Idempotent
}

type UpgradeK8sWorkersInput struct {
	ClusterID        string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	TargetVersionID  string `json:"target_version_id" jsonschema:"UUID of the target Kubernetes version"`
	MaxSurge         *int   `json:"max_surge,omitempty" jsonschema:"optional max surge"`
	DrainGracePeriod *int   `json:"drain_grace_period,omitempty" jsonschema:"optional drain grace period (seconds)"`
	Idempotent
}

type K8sClusterActionInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Idempotent
}

type KubernetesClusterResult struct {
	Cluster map[string]any `json:"cluster"`
}

type KubernetesClusterListResult struct {
	Clusters []map[string]any `json:"clusters"`
	Count    int              `json:"count"`
}

type K8sUpgradeResult struct {
	Upgrade map[string]any `json:"upgrade"`
}

// ── node pool inputs / outputs ──────────────────────────────────────────────

type CreateK8sNodePoolInput struct {
	ClusterID          string           `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Name               string           `json:"name" jsonschema:"node pool name"`
	InstancePlanID     string           `json:"instance_plan_id" jsonschema:"UUID of the node instance plan"`
	MinSize            int              `json:"min_size" jsonschema:"minimum node count"`
	MaxSize            int              `json:"max_size" jsonschema:"maximum node count"`
	TargetCount        int              `json:"target_count" jsonschema:"target node count"`
	Weight             int              `json:"weight" jsonschema:"scheduling weight"`
	AutoscalingEnabled bool             `json:"autoscaling_enabled" jsonschema:"whether autoscaling is enabled"`
	Labels             map[string]any   `json:"labels,omitempty" jsonschema:"optional node labels"`
	Taints             []map[string]any `json:"taints,omitempty" jsonschema:"optional node taints ({key,value,effect})"`
	Idempotent
}

type GetK8sNodePoolInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	PoolID    string `json:"pool_id" jsonschema:"UUID of the node pool"`
}

type ListK8sNodePoolsInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
}

type UpdateK8sNodePoolInput struct {
	ClusterID          string           `json:"cluster_id" jsonschema:"UUID of the cluster"`
	PoolID             string           `json:"pool_id" jsonschema:"UUID of the node pool"`
	MinSize            *int             `json:"min_size,omitempty"`
	MaxSize            *int             `json:"max_size,omitempty"`
	TargetCount        *int             `json:"target_count,omitempty"`
	Weight             *int             `json:"weight,omitempty"`
	AutoscalingEnabled *bool            `json:"autoscaling_enabled,omitempty"`
	Labels             map[string]any   `json:"labels,omitempty"`
	Taints             []map[string]any `json:"taints,omitempty"`
	Idempotent
}

type DeleteK8sNodePoolInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	PoolID    string `json:"pool_id" jsonschema:"UUID of the node pool to delete"`
	Confirmation
	Idempotent
}

type K8sNodePoolResult struct {
	Pool map[string]any `json:"pool"`
}

type K8sNodePoolListResult struct {
	Pools []map[string]any `json:"pools"`
	Count int              `json:"count"`
}

// ── ssl cert inputs / outputs ───────────────────────────────────────────────

type CreateK8sSslCertInput struct {
	ClusterID   string   `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Source      string   `json:"source" jsonschema:"letsencrypt or custom"`
	Domain      string   `json:"domain" jsonschema:"primary domain"`
	Name        string   `json:"name,omitempty" jsonschema:"optional name"`
	Certificate string   `json:"certificate,omitempty" jsonschema:"PEM certificate (required for custom)"`
	PrivateKey  string   `json:"private_key,omitempty" jsonschema:"PEM private key (required for custom)"`
	Chain       string   `json:"chain,omitempty" jsonschema:"optional PEM chain"`
	SanDomains  []string `json:"san_domains,omitempty" jsonschema:"optional SAN domains"`
	Idempotent
}

type GetK8sSslCertInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	CertID    string `json:"cert_id" jsonschema:"UUID of the certificate"`
}

type ListK8sSslCertsInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
}

type DeleteK8sSslCertInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	CertID    string `json:"cert_id" jsonschema:"UUID of the certificate to delete"`
	Confirmation
	Idempotent
}

type K8sSslCertResult struct {
	Certificate map[string]any `json:"certificate"`
}

type K8sSslCertListResult struct {
	Certificates []map[string]any `json:"certificates"`
	Count        int              `json:"count"`
}

// ── security group rule inputs / outputs ────────────────────────────────────

type CreateK8sSgRuleInput struct {
	ClusterID     string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Scope         string `json:"scope" jsonschema:"lb, cp, or worker"`
	Direction     string `json:"direction" jsonschema:"ingress or egress"`
	Protocol      string `json:"protocol" jsonschema:"tcp, udp, icmp, icmpv6, all, or any"`
	IPVersion     string `json:"ip_version" jsonschema:"ipv4 or ipv6"`
	PortRangeMin  *int   `json:"port_range_min,omitempty"`
	PortRangeMax  *int   `json:"port_range_max,omitempty"`
	Cidr          string `json:"cidr,omitempty" jsonschema:"source/dest CIDR (mutually exclusive with remote_group_id/ip_set_id)"`
	RemoteGroupID string `json:"remote_group_id,omitempty"`
	IPSetID       string `json:"ip_set_id,omitempty"`
	Description   string `json:"description,omitempty"`
	Idempotent
}

type GetK8sSgRuleInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Scope     string `json:"scope" jsonschema:"lb, cp, or worker"`
	RuleID    string `json:"rule_id" jsonschema:"UUID of the rule"`
}

type ListK8sSgRulesInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Scope     string `json:"scope" jsonschema:"lb, cp, or worker"`
}

type DeleteK8sSgRuleInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Scope     string `json:"scope" jsonschema:"lb, cp, or worker"`
	RuleID    string `json:"rule_id" jsonschema:"UUID of the rule to delete"`
	Idempotent
}

type K8sSgRuleResult struct {
	Rule map[string]any `json:"rule"`
}

type K8sSgRuleListResult struct {
	Rules []map[string]any `json:"rules"`
	Count int              `json:"count"`
}

type YAMLResult struct {
	YAML string `json:"yaml"`
}

type K8sClusterYAMLInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
}

// ── cluster handlers ────────────────────────────────────────────────────────

func createKubernetesCluster(ctx context.Context, cl *client.Client, in CreateKubernetesClusterInput) (KubernetesClusterResult, error) {
	body := map[string]any{
		"name":                    in.Name,
		"slug":                    in.Slug,
		"hypervisor_group_id":     in.HypervisorGroupID,
		"vpc_id":                  in.VPCID,
		"cp_vpc_subnet_id":        in.CPVPCSubnetID,
		"worker_vpc_subnet_id":    in.WorkerVPCSubnetID,
		"kubernetes_version_id":   in.KubernetesVersionID,
		"control_node_count":      in.ControlNodeCount,
		"endpoint_mode":           in.EndpointMode,
		"cp_instance_plan_id":     in.CPInstancePlanID,
		"cp_lb_plan_id":           in.CPLBPlanID,
		"worker_instance_plan_id": in.WorkerInstancePlanID,
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.ProjectID != "" {
		body["project_id"] = in.ProjectID
	}
	if in.PodCIDR != "" {
		body["pod_cidr"] = in.PodCIDR
	}
	if in.ServiceCIDR != "" {
		body["service_cidr"] = in.ServiceCIDR
	}
	if in.LBHAEnabled != nil {
		body["lb_ha_enabled"] = *in.LBHAEnabled
	}
	if in.WorkerCount != nil {
		body["worker_count"] = *in.WorkerCount
	}

	created, err := cl.CreateKubernetesCluster(ctx, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return KubernetesClusterResult{}, err
	}
	id, _ := created["id"].(string)
	if id == "" {
		return KubernetesClusterResult{}, fmt.Errorf("create response did not include a cluster id")
	}
	// Async: converge on state "running" (fail "error").
	err = waitForStatus(ctx,
		func() (map[string]any, error) { return cl.GetKubernetesCluster(ctx, id) },
		"state", []string{"running"}, []string{"error"}, defaultCreateTimeout)
	if err != nil {
		return KubernetesClusterResult{}, fmt.Errorf("cluster %s did not become running: %w", id, err)
	}
	obj, err := cl.GetKubernetesCluster(ctx, id)
	if err != nil {
		return KubernetesClusterResult{}, err
	}
	return KubernetesClusterResult{Cluster: obj}, nil
}

func getKubernetesCluster(ctx context.Context, cl *client.Client, in GetKubernetesClusterInput) (KubernetesClusterResult, error) {
	obj, err := cl.GetKubernetesCluster(ctx, in.ID)
	if err != nil {
		return KubernetesClusterResult{}, err
	}
	return KubernetesClusterResult{Cluster: obj}, nil
}

func listKubernetesClusters(ctx context.Context, cl *client.Client, _ ListKubernetesClustersInput) (KubernetesClusterListResult, error) {
	items, err := cl.ListKubernetesClusters(ctx)
	if err != nil {
		return KubernetesClusterListResult{}, err
	}
	return KubernetesClusterListResult{Clusters: items, Count: len(items)}, nil
}

func deleteKubernetesCluster(ctx context.Context, cl *client.Client, in DeleteKubernetesClusterInput) (DeleteResult, error) {
	if err := cl.DeleteKubernetesCluster(ctx, in.ID, IdempotencyKeyFromContext(ctx)); err != nil {
		return DeleteResult{}, err
	}
	err := waitForGone(ctx, func() (map[string]any, error) { return cl.GetKubernetesCluster(ctx, in.ID) }, defaultDeleteTimeout)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("cluster %s was not removed: %w", in.ID, err)
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func upgradeK8sControlPlane(ctx context.Context, cl *client.Client, in UpgradeK8sControlPlaneInput) (K8sUpgradeResult, error) {
	body := map[string]any{"target_version_id": in.TargetVersionID}
	if in.DrainGracePeriod != nil {
		body["drain_grace_period"] = *in.DrainGracePeriod
	}
	obj, err := cl.UpgradeK8sClusterControlPlane(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sUpgradeResult{}, err
	}
	return K8sUpgradeResult{Upgrade: obj}, nil
}

func upgradeK8sWorkers(ctx context.Context, cl *client.Client, in UpgradeK8sWorkersInput) (K8sUpgradeResult, error) {
	body := map[string]any{"target_version_id": in.TargetVersionID}
	if in.MaxSurge != nil {
		body["max_surge"] = *in.MaxSurge
	}
	if in.DrainGracePeriod != nil {
		body["drain_grace_period"] = *in.DrainGracePeriod
	}
	obj, err := cl.UpgradeK8sClusterWorkers(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sUpgradeResult{}, err
	}
	return K8sUpgradeResult{Upgrade: obj}, nil
}

func upgradeK8sCCM(ctx context.Context, cl *client.Client, in K8sClusterActionInput) (OKResult, error) {
	if err := cl.UpgradeK8sClusterCCM(ctx, in.ClusterID, IdempotencyKeyFromContext(ctx)); err != nil {
		return OKResult{}, err
	}
	return okResult("CCM upgrade queued"), nil
}

func retryK8sUpgrade(ctx context.Context, cl *client.Client, in K8sClusterActionInput) (K8sUpgradeResult, error) {
	obj, err := cl.RetryK8sClusterUpgrade(ctx, in.ClusterID, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sUpgradeResult{}, err
	}
	return K8sUpgradeResult{Upgrade: obj}, nil
}

// ── node pool handlers ──────────────────────────────────────────────────────

func createK8sNodePool(ctx context.Context, cl *client.Client, in CreateK8sNodePoolInput) (K8sNodePoolResult, error) {
	body := map[string]any{
		"name":                in.Name,
		"instance_plan_id":    in.InstancePlanID,
		"min_size":            in.MinSize,
		"max_size":            in.MaxSize,
		"target_count":        in.TargetCount,
		"weight":              in.Weight,
		"autoscaling_enabled": in.AutoscalingEnabled,
	}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.Taints != nil {
		body["taints"] = in.Taints
	}
	obj, err := cl.CreateKubernetesNodePool(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sNodePoolResult{}, err
	}
	return K8sNodePoolResult{Pool: obj}, nil
}

func getK8sNodePool(ctx context.Context, cl *client.Client, in GetK8sNodePoolInput) (K8sNodePoolResult, error) {
	obj, err := cl.GetKubernetesNodePool(ctx, in.ClusterID, in.PoolID)
	if err != nil {
		return K8sNodePoolResult{}, err
	}
	return K8sNodePoolResult{Pool: obj}, nil
}

func listK8sNodePools(ctx context.Context, cl *client.Client, in ListK8sNodePoolsInput) (K8sNodePoolListResult, error) {
	items, err := cl.ListKubernetesNodePools(ctx, in.ClusterID)
	if err != nil {
		return K8sNodePoolListResult{}, err
	}
	return K8sNodePoolListResult{Pools: items, Count: len(items)}, nil
}

func updateK8sNodePool(ctx context.Context, cl *client.Client, in UpdateK8sNodePoolInput) (K8sNodePoolResult, error) {
	body := map[string]any{}
	if in.MinSize != nil {
		body["min_size"] = *in.MinSize
	}
	if in.MaxSize != nil {
		body["max_size"] = *in.MaxSize
	}
	if in.TargetCount != nil {
		body["target_count"] = *in.TargetCount
	}
	if in.Weight != nil {
		body["weight"] = *in.Weight
	}
	if in.AutoscalingEnabled != nil {
		body["autoscaling_enabled"] = *in.AutoscalingEnabled
	}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.Taints != nil {
		body["taints"] = in.Taints
	}
	obj, err := cl.UpdateKubernetesNodePool(ctx, in.ClusterID, in.PoolID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sNodePoolResult{}, err
	}
	return K8sNodePoolResult{Pool: obj}, nil
}

func deleteK8sNodePool(ctx context.Context, cl *client.Client, in DeleteK8sNodePoolInput) (DeleteResult, error) {
	if err := cl.DeleteKubernetesNodePool(ctx, in.ClusterID, in.PoolID, IdempotencyKeyFromContext(ctx)); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.PoolID, Deleted: true}, nil
}

// ── ssl cert handlers ───────────────────────────────────────────────────────

func createK8sSslCert(ctx context.Context, cl *client.Client, in CreateK8sSslCertInput) (K8sSslCertResult, error) {
	body := map[string]any{"source": in.Source, "domain": in.Domain}
	if in.Name != "" {
		body["name"] = in.Name
	}
	if in.Certificate != "" {
		body["certificate"] = in.Certificate
	}
	if in.PrivateKey != "" {
		body["private_key"] = in.PrivateKey
	}
	if in.Chain != "" {
		body["chain"] = in.Chain
	}
	if in.SanDomains != nil {
		body["san_domains"] = in.SanDomains
	}
	obj, err := cl.CreateKubernetesSslCert(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sSslCertResult{}, err
	}
	return K8sSslCertResult{Certificate: obj}, nil
}

func getK8sSslCert(ctx context.Context, cl *client.Client, in GetK8sSslCertInput) (K8sSslCertResult, error) {
	obj, err := cl.GetKubernetesSslCert(ctx, in.ClusterID, in.CertID)
	if err != nil {
		return K8sSslCertResult{}, err
	}
	return K8sSslCertResult{Certificate: obj}, nil
}

func listK8sSslCerts(ctx context.Context, cl *client.Client, in ListK8sSslCertsInput) (K8sSslCertListResult, error) {
	items, err := cl.ListKubernetesSslCerts(ctx, in.ClusterID)
	if err != nil {
		return K8sSslCertListResult{}, err
	}
	return K8sSslCertListResult{Certificates: items, Count: len(items)}, nil
}

func deleteK8sSslCert(ctx context.Context, cl *client.Client, in DeleteK8sSslCertInput) (DeleteResult, error) {
	if err := cl.DeleteKubernetesSslCert(ctx, in.ClusterID, in.CertID, IdempotencyKeyFromContext(ctx)); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.CertID, Deleted: true}, nil
}

// ── security group rule handlers ────────────────────────────────────────────

func createK8sSgRule(ctx context.Context, cl *client.Client, in CreateK8sSgRuleInput) (K8sSgRuleResult, error) {
	body := map[string]any{"direction": in.Direction, "protocol": in.Protocol, "ip_version": in.IPVersion}
	if in.PortRangeMin != nil {
		body["port_range_min"] = *in.PortRangeMin
	}
	if in.PortRangeMax != nil {
		body["port_range_max"] = *in.PortRangeMax
	}
	if in.Cidr != "" {
		body["cidr"] = in.Cidr
	}
	if in.RemoteGroupID != "" {
		body["remote_group_id"] = in.RemoteGroupID
	}
	if in.IPSetID != "" {
		body["ip_set_id"] = in.IPSetID
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	obj, err := cl.CreateKubernetesClusterSgRule(ctx, in.ClusterID, in.Scope, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return K8sSgRuleResult{}, err
	}
	return K8sSgRuleResult{Rule: obj}, nil
}

func getK8sSgRule(ctx context.Context, cl *client.Client, in GetK8sSgRuleInput) (K8sSgRuleResult, error) {
	obj, err := cl.GetKubernetesClusterSgRule(ctx, in.ClusterID, in.Scope, in.RuleID)
	if err != nil {
		return K8sSgRuleResult{}, err
	}
	return K8sSgRuleResult{Rule: obj}, nil
}

func listK8sSgRules(ctx context.Context, cl *client.Client, in ListK8sSgRulesInput) (K8sSgRuleListResult, error) {
	items, err := cl.ListKubernetesClusterSgRules(ctx, in.ClusterID, in.Scope)
	if err != nil {
		return K8sSgRuleListResult{}, err
	}
	return K8sSgRuleListResult{Rules: items, Count: len(items)}, nil
}

func deleteK8sSgRule(ctx context.Context, cl *client.Client, in DeleteK8sSgRuleInput) (DeleteResult, error) {
	if err := cl.DeleteKubernetesClusterSgRule(ctx, in.ClusterID, in.Scope, in.RuleID, IdempotencyKeyFromContext(ctx)); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.RuleID, Deleted: true}, nil
}

// ── kubeconfig / autoscaler manifest ────────────────────────────────────────

func getKubeconfig(ctx context.Context, cl *client.Client, in K8sClusterYAMLInput) (YAMLResult, error) {
	yaml, err := cl.GetKubeconfig(ctx, in.ClusterID)
	if err != nil {
		return YAMLResult{}, err
	}
	return YAMLResult{YAML: yaml}, nil
}

func getAutoscalerManifest(ctx context.Context, cl *client.Client, in K8sClusterYAMLInput) (YAMLResult, error) {
	yaml, err := cl.GetAutoscalerManifest(ctx, in.ClusterID)
	if err != nil {
		return YAMLResult{}, err
	}
	return YAMLResult{YAML: yaml}, nil
}

func registerKubernetesTools(s *mcp.Server, deps Deps) {
	// Cluster.
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.create", Description: "Create a Kubernetes cluster and wait until it is running."}, createKubernetesCluster)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.list", Description: "List all Kubernetes clusters owned by the caller."}, listKubernetesClusters)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.get", Description: "Get a Kubernetes cluster by UUID."}, getKubernetesCluster)
	Register(s, deps, Spec{
		Name:        "user.kubernetes_cluster.delete",
		Description: "Delete a Kubernetes cluster and wait until removed. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteKubernetesCluster)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.upgrade_control_plane", Description: "Upgrade a cluster's control plane to a target version."}, upgradeK8sControlPlane)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.upgrade_workers", Description: "Upgrade a cluster's worker nodes to a target version."}, upgradeK8sWorkers)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.upgrade_ccm", Description: "Upgrade a cluster's cloud controller manager."}, upgradeK8sCCM)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.retry_upgrade", Description: "Retry a failed cluster upgrade."}, retryK8sUpgrade)

	// Node pools.
	Register(s, deps, Spec{Name: "user.kubernetes_node_pool.create", Description: "Add a node pool to a cluster."}, createK8sNodePool)
	Register(s, deps, Spec{Name: "user.kubernetes_node_pool.list", Description: "List a cluster's node pools."}, listK8sNodePools)
	Register(s, deps, Spec{Name: "user.kubernetes_node_pool.get", Description: "Get a cluster node pool by UUID."}, getK8sNodePool)
	Register(s, deps, Spec{Name: "user.kubernetes_node_pool.update", Description: "Update a node pool's sizing, weight, labels, or taints."}, updateK8sNodePool)
	Register(s, deps, Spec{
		Name:        "user.kubernetes_node_pool.delete",
		Description: "Delete a node pool. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteK8sNodePool)

	// SSL certificates.
	Register(s, deps, Spec{Name: "user.kubernetes_ssl_certificate.create", Description: "Add an SSL certificate to a cluster."}, createK8sSslCert)
	Register(s, deps, Spec{Name: "user.kubernetes_ssl_certificate.list", Description: "List a cluster's SSL certificates."}, listK8sSslCerts)
	Register(s, deps, Spec{Name: "user.kubernetes_ssl_certificate.get", Description: "Get a cluster SSL certificate by UUID."}, getK8sSslCert)
	Register(s, deps, Spec{
		Name:        "user.kubernetes_ssl_certificate.delete",
		Description: "Delete a cluster SSL certificate. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteK8sSslCert)

	// Security group rules (scope: lb, cp, worker).
	Register(s, deps, Spec{Name: "user.kubernetes_security_group_rule.create", Description: "Add a security group rule to a cluster scope (lb, cp, or worker)."}, createK8sSgRule)
	Register(s, deps, Spec{Name: "user.kubernetes_security_group_rule.list", Description: "List a cluster scope's security group rules."}, listK8sSgRules)
	Register(s, deps, Spec{Name: "user.kubernetes_security_group_rule.get", Description: "Get a cluster security group rule by UUID."}, getK8sSgRule)
	Register(s, deps, Spec{Name: "user.kubernetes_security_group_rule.delete", Description: "Delete a cluster security group rule."}, deleteK8sSgRule)

	// Data sources.
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.kubeconfig", Description: "Download a cluster's kubeconfig (YAML, sensitive)."}, getKubeconfig)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.autoscaler_manifest", Description: "Download a cluster's autoscaler manifest (YAML, sensitive)."}, getAutoscalerManifest)
}
