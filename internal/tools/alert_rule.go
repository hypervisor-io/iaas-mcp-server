package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Alert rule tools, mirroring the iaas_alert_rule resource. Full CRUD, all
// synchronous. channel_ids replaces the full attached-channel set when sent.
//
// The server exposes POST /alert-rule/{id}/acknowledge and a history GET, but
// neither has a client method (operational actions), so they are not exposed.

func init() {
	toolRegistrars = append(toolRegistrars, registerAlertRuleTools)
}

type CreateAlertRuleInput struct {
	Name             string   `json:"name" jsonschema:"alert rule name"`
	ResourceType     string   `json:"resource_type" jsonschema:"instance, managed_database, load_balancer, or vpn_gateway"`
	Metric           string   `json:"metric" jsonschema:"metric to evaluate (e.g. cpu, memory)"`
	Operator         string   `json:"operator" jsonschema:"gt, lt, gte, lte, or eq"`
	Threshold        float64  `json:"threshold" jsonschema:"threshold value"`
	ResourceID       string   `json:"resource_id,omitempty" jsonschema:"optional specific resource UUID (omit to match all of the type)"`
	Duration         *int     `json:"duration,omitempty" jsonschema:"optional sustained duration in seconds"`
	ReminderInterval *int     `json:"reminder_interval,omitempty" jsonschema:"optional reminder interval in seconds"`
	ChannelIDs       []string `json:"channel_ids,omitempty" jsonschema:"UUIDs of notification channels to attach"`
}

type GetAlertRuleInput struct {
	ID string `json:"id" jsonschema:"UUID of the alert rule"`
}

type ListAlertRulesInput struct{}

type UpdateAlertRuleInput struct {
	ID               string   `json:"id" jsonschema:"UUID of the alert rule"`
	Name             *string  `json:"name,omitempty" jsonschema:"new name"`
	Metric           *string  `json:"metric,omitempty" jsonschema:"new metric"`
	Operator         *string  `json:"operator,omitempty" jsonschema:"new operator"`
	Threshold        *float64 `json:"threshold,omitempty" jsonschema:"new threshold"`
	Enabled          *bool    `json:"enabled,omitempty" jsonschema:"enable or disable the rule"`
	Duration         *int     `json:"duration,omitempty" jsonschema:"new sustained duration in seconds"`
	ReminderInterval *int     `json:"reminder_interval,omitempty" jsonschema:"new reminder interval in seconds"`
	ChannelIDs       []string `json:"channel_ids,omitempty" jsonschema:"replacement set of notification channel UUIDs"`
}

type DeleteAlertRuleInput struct {
	ID string `json:"id" jsonschema:"UUID of the alert rule to delete"`
	Confirmation
}

type AlertRuleResult struct {
	AlertRule map[string]any `json:"alert_rule"`
}

type AlertRuleListResult struct {
	AlertRules []map[string]any `json:"alert_rules"`
	Count      int              `json:"count"`
}

func createAlertRule(ctx context.Context, cl *client.Client, in CreateAlertRuleInput) (AlertRuleResult, error) {
	body := map[string]any{
		"name":          in.Name,
		"resource_type": in.ResourceType,
		"metric":        in.Metric,
		"operator":      in.Operator,
		"threshold":     in.Threshold,
	}
	if in.ResourceID != "" {
		body["resource_id"] = in.ResourceID
	}
	if in.Duration != nil {
		body["duration"] = *in.Duration
	}
	if in.ReminderInterval != nil {
		body["reminder_interval"] = *in.ReminderInterval
	}
	if in.ChannelIDs != nil {
		body["channel_ids"] = in.ChannelIDs
	}
	obj, err := cl.CreateAlertRule(ctx, body)
	if err != nil {
		return AlertRuleResult{}, err
	}
	return AlertRuleResult{AlertRule: obj}, nil
}

func getAlertRule(ctx context.Context, cl *client.Client, in GetAlertRuleInput) (AlertRuleResult, error) {
	obj, err := cl.GetAlertRule(ctx, in.ID)
	if err != nil {
		return AlertRuleResult{}, err
	}
	return AlertRuleResult{AlertRule: obj}, nil
}

func listAlertRules(ctx context.Context, cl *client.Client, _ ListAlertRulesInput) (AlertRuleListResult, error) {
	items, err := cl.ListAlertRules(ctx)
	if err != nil {
		return AlertRuleListResult{}, err
	}
	return AlertRuleListResult{AlertRules: items, Count: len(items)}, nil
}

func updateAlertRule(ctx context.Context, cl *client.Client, in UpdateAlertRuleInput) (AlertRuleResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Metric != nil {
		body["metric"] = *in.Metric
	}
	if in.Operator != nil {
		body["operator"] = *in.Operator
	}
	if in.Threshold != nil {
		body["threshold"] = *in.Threshold
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.Duration != nil {
		body["duration"] = *in.Duration
	}
	if in.ReminderInterval != nil {
		body["reminder_interval"] = *in.ReminderInterval
	}
	if in.ChannelIDs != nil {
		body["channel_ids"] = in.ChannelIDs
	}
	obj, err := cl.UpdateAlertRule(ctx, in.ID, body)
	if err != nil {
		return AlertRuleResult{}, err
	}
	return AlertRuleResult{AlertRule: obj}, nil
}

func deleteAlertRule(ctx context.Context, cl *client.Client, in DeleteAlertRuleInput) (DeleteResult, error) {
	if err := cl.DeleteAlertRule(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerAlertRuleTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.alert_rule.create", Description: "Create a metric alert rule."}, createAlertRule)
	Register(s, deps, Spec{Name: "user.alert_rule.list", Description: "List all alert rules owned by the caller."}, listAlertRules)
	Register(s, deps, Spec{Name: "user.alert_rule.get", Description: "Get an alert rule by UUID."}, getAlertRule)
	Register(s, deps, Spec{Name: "user.alert_rule.update", Description: "Update an alert rule's fields or attached channels."}, updateAlertRule)
	Register(s, deps, Spec{
		Name:        "user.alert_rule.delete",
		Description: "Delete an alert rule. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteAlertRule)
}
