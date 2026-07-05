package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Notification channel tools, mirroring the iaas_notification_channel resource.
// Full CRUD, all synchronous. The per-type config (slack/discord webhook_url,
// telegram bot_token+chat_id, webhook url) is passed as a free-form object.
//
// The server exposes a POST /notification-channel/{id}/test action, but there
// is no client method for it (it is an operational, non-declarative action), so
// it is intentionally not exposed here.

func init() {
	toolRegistrars = append(toolRegistrars, registerNotificationChannelTools)
}

type CreateNotificationChannelInput struct {
	Name    string         `json:"name" jsonschema:"channel name"`
	Type    string         `json:"type" jsonschema:"slack, discord, telegram, or webhook"`
	Enabled *bool          `json:"enabled,omitempty" jsonschema:"whether the channel is enabled"`
	Config  map[string]any `json:"config,omitempty" jsonschema:"per-type config (e.g. webhook_url, or bot_token+chat_id)"`
}

type GetNotificationChannelInput struct {
	ID string `json:"id" jsonschema:"UUID of the notification channel"`
}

type ListNotificationChannelsInput struct{}

type UpdateNotificationChannelInput struct {
	ID      string         `json:"id" jsonschema:"UUID of the notification channel"`
	Name    *string        `json:"name,omitempty" jsonschema:"new name"`
	Enabled *bool          `json:"enabled,omitempty" jsonschema:"enable or disable"`
	Config  map[string]any `json:"config,omitempty" jsonschema:"replacement per-type config"`
}

type DeleteNotificationChannelInput struct {
	ID string `json:"id" jsonschema:"UUID of the notification channel to delete"`
	Confirmation
}

type NotificationChannelResult struct {
	Channel map[string]any `json:"channel"`
}

type NotificationChannelListResult struct {
	Channels []map[string]any `json:"channels"`
	Count    int              `json:"count"`
}

func createNotificationChannel(ctx context.Context, cl *client.Client, in CreateNotificationChannelInput) (NotificationChannelResult, error) {
	body := map[string]any{"name": in.Name, "type": in.Type}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.Config != nil {
		body["config"] = in.Config
	}
	obj, err := cl.CreateNotificationChannel(ctx, body)
	if err != nil {
		return NotificationChannelResult{}, err
	}
	return NotificationChannelResult{Channel: obj}, nil
}

func getNotificationChannel(ctx context.Context, cl *client.Client, in GetNotificationChannelInput) (NotificationChannelResult, error) {
	obj, err := cl.GetNotificationChannel(ctx, in.ID)
	if err != nil {
		return NotificationChannelResult{}, err
	}
	return NotificationChannelResult{Channel: obj}, nil
}

func listNotificationChannels(ctx context.Context, cl *client.Client, _ ListNotificationChannelsInput) (NotificationChannelListResult, error) {
	items, err := cl.ListNotificationChannels(ctx)
	if err != nil {
		return NotificationChannelListResult{}, err
	}
	return NotificationChannelListResult{Channels: items, Count: len(items)}, nil
}

func updateNotificationChannel(ctx context.Context, cl *client.Client, in UpdateNotificationChannelInput) (NotificationChannelResult, error) {
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if in.Config != nil {
		body["config"] = in.Config
	}
	obj, err := cl.UpdateNotificationChannel(ctx, in.ID, body)
	if err != nil {
		return NotificationChannelResult{}, err
	}
	return NotificationChannelResult{Channel: obj}, nil
}

func deleteNotificationChannel(ctx context.Context, cl *client.Client, in DeleteNotificationChannelInput) (DeleteResult, error) {
	if err := cl.DeleteNotificationChannel(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

func registerNotificationChannelTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{Name: "user.notification_channel.create", Description: "Create a notification channel (slack, discord, telegram, or webhook)."}, createNotificationChannel)
	Register(s, deps, Spec{Name: "user.notification_channel.list", Description: "List all notification channels owned by the caller."}, listNotificationChannels)
	Register(s, deps, Spec{Name: "user.notification_channel.get", Description: "Get a notification channel by UUID."}, getNotificationChannel)
	Register(s, deps, Spec{Name: "user.notification_channel.update", Description: "Update a notification channel's name, enabled state, or config."}, updateNotificationChannel)
	Register(s, deps, Spec{
		Name:        "user.notification_channel.delete",
		Description: "Delete a notification channel. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteNotificationChannel)
}
