package tools_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func notificationChannelMock() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notification-channels", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "channel": map[string]any{"id": "ch-1", "name": "ops", "type": "slack"},
		})
	})
	mux.HandleFunc("GET /notification-channels", func(w http.ResponseWriter, r *http.Request) {
		// List envelope is nested under "channels".data (client handles it).
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"channels": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{map[string]any{"id": "ch-1", "name": "ops", "type": "slack"}},
			},
		})
	})
	mux.HandleFunc("GET /notification-channel/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Channel not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "channel": map[string]any{"id": id, "name": "ops"}})
	})
	mux.HandleFunc("PATCH /notification-channel/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "channel": map[string]any{"id": id, "name": "renamed"}})
	})
	mux.HandleFunc("DELETE /notification-channel/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestNotificationChannel_CRUD(t *testing.T) {
	cs := connectSession(t, notificationChannelMock())

	res := callTool(t, cs, "user.notification_channel.create", map[string]any{
		"name": "ops", "type": "slack", "config": map[string]any{"webhook_url": "https://hooks.example.com/x"},
	})
	var created tools.NotificationChannelResult
	unmarshalResult(t, res, &created)
	if created.Channel["id"] != "ch-1" {
		t.Fatalf("create id = %v, want ch-1", created.Channel["id"])
	}

	res = callTool(t, cs, "user.notification_channel.list", map[string]any{})
	var list tools.NotificationChannelListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.notification_channel.get", map[string]any{"id": "ch-1"})
	var got tools.NotificationChannelResult
	unmarshalResult(t, res, &got)
	if got.Channel["id"] != "ch-1" {
		t.Errorf("get id = %v, want ch-1", got.Channel["id"])
	}
}

func TestNotificationChannel_DeleteConfirmAndErrors(t *testing.T) {
	cs := connectSession(t, notificationChannelMock())
	res := callTool(t, cs, "user.notification_channel.delete", map[string]any{"id": "ch-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.notification_channel.delete", map[string]any{"id": "ch-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
	res = callTool(t, cs, "user.notification_channel.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
}
