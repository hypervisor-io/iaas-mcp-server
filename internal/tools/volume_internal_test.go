package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/hypervisor-io/terraform-provider-iaas/volumeop"
)

// TestAttachVolume_TimeoutReportsPendingNotCancelled proves the bounded wait:
// an operation the node never confirms ends the tool call with a PendingError
// naming the still-active operation (the platform keeps it; the agent follows
// it with user.volume.get), and the request is sent exactly once.
func TestAttachVolume_TimeoutReportsPendingNotCancelled(t *testing.T) {
	t.Setenv("IAAS_MCP_POLL_INTERVAL", "1ms")
	prev := volumeOpTimeout
	volumeOpTimeout = 150 * time.Millisecond
	t.Cleanup(func() { volumeOpTimeout = prev })

	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vol := map[string]any{"id": "vol-1", "status": "available", "instance_id": nil, "active_operation": nil}
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		if posts.Load() > 0 {
			vol["active_operation"] = "attach"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "volume": vol})
	}))
	t.Cleanup(srv.Close)
	cl := client.New(srv.URL, "test-token", 2*time.Second, false)

	start := time.Now()
	_, err := attachVolume(context.Background(), cl, AttachVolumeInput{ID: "vol-1", InstanceID: "inst-1"})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the wait took %v; it must be bounded by volumeOpTimeout", elapsed)
	}
	var pending *volumeop.PendingError
	if !errors.As(err, &pending) {
		t.Fatalf("err = %v (%T); want *volumeop.PendingError", err, err)
	}
	if pending.ActiveOperation != "attach" || !strings.Contains(err.Error(), "not cancelled") {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err must wrap context.DeadlineExceeded: %v", err)
	}
	if n := posts.Load(); n != 1 {
		t.Fatalf("attach requests = %d; want 1", n)
	}
}
