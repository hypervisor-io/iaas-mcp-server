package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Kubernetes worker/cluster action tools. The mutating routes carry
// idempotency.user, so these inputs accept an optional idempotency_key threaded
// through the framework seam. Scale/labels/delete-worker enqueue a task and
// return its task_id; the agent can poll the cluster for convergence.

func init() {
	toolRegistrars = append(toolRegistrars, registerParityK8sTools)
}

type K8sClusterIdemInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Idempotent
}

type K8sPoolCancelPendingInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	PoolID    string `json:"pool_id" jsonschema:"UUID of the node pool"`
	RefID     string `json:"ref_id" jsonschema:"UUID of the pending VM ref to cancel"`
	Idempotent
}

type K8sPoolReassignInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	PoolID    string `json:"pool_id" jsonschema:"UUID of the node pool to promote to default"`
	Reason    string `json:"reason,omitempty" jsonschema:"optional reason"`
	Idempotent
}

type K8sDeleteWorkerInput struct {
	ClusterID string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	NodeName  string `json:"node_name" jsonschema:"name of the worker node to delete"`
	Confirmation
	Idempotent
}

type K8sWorkersAutoscalingInput struct {
	ClusterID                string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	WorkerMinSize            int    `json:"worker_min_size" jsonschema:"minimum worker count (1-100)"`
	WorkerMaxSize            int    `json:"worker_max_size" jsonschema:"maximum worker count (>= min)"`
	WorkerAutoscalingEnabled bool   `json:"worker_autoscaling_enabled" jsonschema:"enable worker autoscaling"`
	Idempotent
}

type K8sWorkersLabelsInput struct {
	ClusterID string           `json:"cluster_id" jsonschema:"UUID of the cluster"`
	Labels    map[string]any   `json:"labels,omitempty" jsonschema:"node labels (key -> value)"`
	Taints    []map[string]any `json:"taints,omitempty" jsonschema:"node taints ({key,value,effect})"`
	Idempotent
}

type K8sWorkersScaleInput struct {
	ClusterID    string `json:"cluster_id" jsonschema:"UUID of the cluster"`
	DesiredCount int    `json:"desired_count" jsonschema:"desired worker count (>= 0)"`
	Reason       string `json:"reason,omitempty" jsonschema:"optional reason"`
	Idempotent
}

func acknowledgeK8sError(ctx context.Context, cl *client.Client, in K8sClusterIdemInput) (ObjectResult, error) {
	return objectResult(cl.AcknowledgeK8sClusterError(ctx, in.ClusterID, IdempotencyKeyFromContext(ctx)))
}

func acknowledgeK8sKubeconfig(ctx context.Context, cl *client.Client, in K8sClusterIdemInput) (OKResult, error) {
	if _, err := cl.AcknowledgeK8sKubeconfig(ctx, in.ClusterID, IdempotencyKeyFromContext(ctx)); err != nil {
		return OKResult{}, err
	}
	return okResult("kubeconfig reissue acknowledged"), nil
}

func cancelK8sPoolPending(ctx context.Context, cl *client.Client, in K8sPoolCancelPendingInput) (ObjectResult, error) {
	return objectResult(cl.CancelK8sPoolPending(ctx, in.ClusterID, in.PoolID, map[string]any{"ref_id": in.RefID}, IdempotencyKeyFromContext(ctx)))
}

func reassignK8sPool(ctx context.Context, cl *client.Client, in K8sPoolReassignInput) (ObjectResult, error) {
	body := map[string]any{}
	if in.Reason != "" {
		body["reason"] = in.Reason
	}
	return objectResult(cl.ReassignK8sPool(ctx, in.ClusterID, in.PoolID, body, IdempotencyKeyFromContext(ctx)))
}

func deleteK8sWorker(ctx context.Context, cl *client.Client, in K8sDeleteWorkerInput) (ObjectResult, error) {
	return objectResult(cl.DeleteK8sWorkerNode(ctx, in.ClusterID, in.NodeName, IdempotencyKeyFromContext(ctx)))
}

func k8sWorkersAutoscaling(ctx context.Context, cl *client.Client, in K8sWorkersAutoscalingInput) (KubernetesClusterResult, error) {
	body := map[string]any{
		"worker_min_size":            in.WorkerMinSize,
		"worker_max_size":            in.WorkerMaxSize,
		"worker_autoscaling_enabled": in.WorkerAutoscalingEnabled,
	}
	obj, err := cl.ToggleK8sWorkersAutoscaling(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx))
	if err != nil {
		return KubernetesClusterResult{}, err
	}
	return KubernetesClusterResult{Cluster: obj}, nil
}

func k8sWorkersLabels(ctx context.Context, cl *client.Client, in K8sWorkersLabelsInput) (ObjectResult, error) {
	body := map[string]any{}
	if in.Labels != nil {
		body["labels"] = in.Labels
	}
	if in.Taints != nil {
		body["taints"] = in.Taints
	}
	return objectResult(cl.UpdateK8sWorkersLabels(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx)))
}

func k8sWorkersScale(ctx context.Context, cl *client.Client, in K8sWorkersScaleInput) (ObjectResult, error) {
	body := map[string]any{"desired_count": in.DesiredCount}
	if in.Reason != "" {
		body["reason"] = in.Reason
	}
	return objectResult(cl.ScaleK8sWorkers(ctx, in.ClusterID, body, IdempotencyKeyFromContext(ctx)))
}

func registerParityK8sTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.acknowledge_error", Description: "Acknowledge (dismiss) a cluster's last error."}, acknowledgeK8sError)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.kubeconfig_acknowledge", Description: "Acknowledge that a cluster's kubeconfig was reissued."}, acknowledgeK8sKubeconfig)
	Register(s, deps, Spec{Name: "user.kubernetes_node_pool.cancel_pending", Description: "Cancel a node pool's pending VM deletion."}, cancelK8sPoolPending)
	Register(s, deps, Spec{Name: "user.kubernetes_node_pool.reassign", Description: "Promote a node pool to the cluster's default worker pool."}, reassignK8sPool)
	Register(s, deps, Spec{
		Name:        "user.kubernetes_cluster.delete_worker",
		Description: "Cordon, drain, and delete a specific worker node. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteK8sWorker)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.workers_autoscaling", Description: "Configure worker autoscaling (min/max/enabled) on the default pool."}, k8sWorkersAutoscaling)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.workers_labels", Description: "Set node labels and taints on the cluster's workers."}, k8sWorkersLabels)
	Register(s, deps, Spec{Name: "user.kubernetes_cluster.workers_scale", Description: "Scale the cluster's default worker pool to a desired count."}, k8sWorkersScale)
}
