package tools_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// snapshotMock is the snapshot API as the platform answers it: 202 creating, then ready after n polls, the typed
// refusals, and a recorder of every request.
type snapshotMock struct {
	mu      sync.Mutex
	calls   []string
	bodies  []map[string]any
	headers []string
	polls   map[string]int
}

func (m *snapshotMock) record(r *http.Request) map[string]any {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, r.Method+" "+r.URL.RequestURI())
	m.bodies = append(m.bodies, body)
	m.headers = append(m.headers, r.Header.Get("Idempotency-Key"))
	return body
}

func snapObj(id, status string, extra map[string]any) map[string]any {
	o := map[string]any{"id": id, "name": "before", "kind": "disk", "status": status, "disk_gib": 10, "price_gib_hour": 0.5}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

func (m *snapshotMock) handler() http.Handler {
	m.polls = map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vm/{id}/snapshots", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		switch r.PathValue("id") {
		case "foreign":
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "sandbox_not_found", "message": "Sandbox not found."})
		case "pause":
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "snapshot_requires_pause", "message": "pausing first avoids it", "pause_estimate_ms": 14000, "pause_limit_ms": 10000})
		case "slow":
			writeJSON(w, http.StatusAccepted, map[string]any{"success": true, "snapshot": snapObj("snap-slow", "creating", nil)})
		case "fails":
			writeJSON(w, http.StatusAccepted, map[string]any{"success": true, "snapshot": snapObj("snap-fails", "creating", nil)})
		default:
			writeJSON(w, http.StatusAccepted, map[string]any{"success": true, "snapshot": snapObj("snap-1", "creating", map[string]any{"name": body["name"]})})
		}
	})
	mux.HandleFunc("GET /microvm/snapshot/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		id := r.PathValue("id")
		m.mu.Lock()
		m.polls[id]++
		n := m.polls[id]
		m.mu.Unlock()
		switch id {
		case "missing":
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "snapshot_not_found", "message": "Snapshot not found."})
		case "snap-slow":
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": snapObj(id, "creating", nil)})
		case "snap-fails":
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": snapObj(id, "failed", map[string]any{"failure_reason": "snapshot_requires_pause", "failure_detail": map[string]any{"message": "Pausing or stopping the sandbox first avoids it.", "pause_estimate_ms": 14000, "pause_limit_ms": 10000}})})
		default:
			if n < 3 {
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": snapObj(id, "creating", nil)})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": snapObj(id, "ready", map[string]any{"capture_method": "paused_copy", "paused_ms": 2300, "consistency": "crash"})})
		}
	})
	mux.HandleFunc("GET /microvm/snapshots", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshots": map[string]any{
			"current_page": 1, "last_page": 1, "data": []any{snapObj("snap-1", "ready", nil), snapObj("snap-2", "ready", nil)},
		}})
	})
	mux.HandleFunc("DELETE /microvm/snapshot/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if r.PathValue("id") == "busy" {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "snapshot_in_use", "message": "in use"})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"success": true, "snapshot": map[string]any{"id": r.PathValue("id"), "status": "deleting"}})
	})
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{"id": "vm-new", "name": body["name"], "state": "creating", "source_snapshot_id": body["snapshot_id"]}})
	})
	return mux
}

func TestMicrovmSnapshot_CreateSendsTheDocumentedBodyWaitsForReadyAndReportsTheCapture(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.snapshot_create", map[string]any{
		"sandbox_id": "vm-1", "name": "before", "description": "pre-migration", "consistency": "quiesced",
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	var got tools.MicrovmSnapshotResult
	unmarshalResult(t, res, &got)
	if !got.Ready || got.Snapshot["status"] != "ready" || got.Snapshot["capture_method"] != "paused_copy" || got.Snapshot["paused_ms"] != float64(2300) {
		t.Fatalf("result = %+v", got)
	}
	if mock.calls[0] != "POST /microvm/vm/vm-1/snapshots" || mock.headers[0] == "" {
		t.Fatalf("first request = %s key=%q", mock.calls[0], mock.headers[0])
	}
	b := mock.bodies[0]
	if b["name"] != "before" || b["kind"] != "disk" || b["description"] != "pre-migration" || b["consistency"] != "quiesced" {
		t.Fatalf("body = %v", b)
	}
	if len(mock.calls) < 4 {
		t.Fatalf("expected polling until ready, calls = %v", mock.calls)
	}
}

func TestMicrovmSnapshot_CreateOmitsUnsetOptionalFields(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	callTool(t, cs, "user.microvm.snapshot_create", map[string]any{"sandbox_id": "vm-1", "name": "n"})
	b := mock.bodies[0]
	for _, k := range []string{"description", "consistency"} {
		if _, ok := b[k]; ok {
			t.Errorf("body must not carry %s: %v", k, b)
		}
	}
}

func TestMicrovmSnapshot_CreateReturnsNotReadyWhenTheWaitRunsOutAndAnErrorWhenItFails(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.snapshot_create", map[string]any{"sandbox_id": "slow", "name": "n", "wait_seconds": 1})
	if res.IsError {
		t.Fatalf("a wait that runs out is not an error: %s", resultText(t, res))
	}
	var got tools.MicrovmSnapshotResult
	unmarshalResult(t, res, &got)
	if got.Ready || got.Snapshot["status"] != "creating" || got.Snapshot["id"] != "snap-slow" {
		t.Fatalf("result = %+v", got)
	}

	res = callTool(t, cs, "user.microvm.snapshot_create", map[string]any{"sandbox_id": "fails", "name": "n"})
	if !res.IsError || !strings.Contains(resultText(t, res), "snapshot_requires_pause") || !strings.Contains(resultText(t, res), "estimated pause 14000 ms, limit 10000 ms") {
		t.Fatalf("a failed snapshot must be an error naming the reason; got %q", resultText(t, res))
	}
}

func TestMicrovmSnapshot_RefusalsCarryTheirReason(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	for id, reason := range map[string]string{"foreign": "sandbox_not_found", "pause": "snapshot_requires_pause"} {
		res := callTool(t, cs, "user.microvm.snapshot_create", map[string]any{"sandbox_id": id, "name": "n"})
		if !res.IsError || !strings.Contains(resultText(t, res), reason) {
			t.Errorf("%s: want reason %s, got %q", id, reason, resultText(t, res))
		}
	}
}

func TestMicrovmSnapshot_ListAndGet(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.snapshot_list", map[string]any{"sandbox_id": "vm-1", "status": "ready"})
	var list tools.MicrovmSnapshotListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 || len(list.Snapshots) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if mock.calls[0] != "GET /microvm/snapshots?sandbox_id=vm-1&status=ready" {
		t.Fatalf("request = %s", mock.calls[0])
	}

	var one tools.MicrovmSnapshotResult
	unmarshalResult(t, callTool(t, cs, "user.microvm.snapshot_get", map[string]any{"id": "snap-slow"}), &one)
	if one.Ready || one.Snapshot["status"] != "creating" {
		t.Fatalf("get = %+v", one)
	}
	res = callTool(t, cs, "user.microvm.snapshot_get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "snapshot_not_found") {
		t.Fatalf("get missing = %q", resultText(t, res))
	}
}

func TestMicrovmSnapshot_DeleteIsConfirmGated(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.snapshot_delete", map[string]any{"id": "snap-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	if len(mock.calls) != 0 {
		t.Fatalf("an unconfirmed delete must not reach the API; calls = %v", mock.calls)
	}
	res = callTool(t, cs, "user.microvm.snapshot_delete", map[string]any{"id": "snap-1", "confirm": true})
	if res.IsError || mock.calls[0] != "DELETE /microvm/snapshot/snap-1" {
		t.Fatalf("confirmed delete: err=%v calls=%v", res.IsError, mock.calls)
	}
	res = callTool(t, cs, "user.microvm.snapshot_delete", map[string]any{"id": "busy", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "snapshot_in_use") {
		t.Fatalf("busy delete = %q", resultText(t, res))
	}
}

func TestMicrovmCreate_ForkFromASnapshotNeedsNoImageOrLocation(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.create", map[string]any{"name": "fork-1", "snapshot_id": "snap-1", "inherit_env": true, "plan_id": "plan-1"})
	if res.IsError {
		t.Fatalf("fork failed: %s", resultText(t, res))
	}
	b := mock.bodies[0]
	if b["snapshot_id"] != "snap-1" || b["inherit_env"] != true || b["name"] != "fork-1" || b["plan_id"] != "plan-1" {
		t.Fatalf("body = %v", b)
	}
	for _, k := range []string{"image_id", "location_id"} {
		if _, ok := b[k]; ok {
			t.Errorf("a fork must not send %s: %v", k, b)
		}
	}
	// The image path still sends both and no snapshot fields.
	callTool(t, cs, "user.microvm.create", map[string]any{"name": "plain", "image_id": "img-1", "location_id": "loc-1"})
	b = mock.bodies[len(mock.bodies)-1]
	if b["image_id"] != "img-1" || b["location_id"] != "loc-1" {
		t.Fatalf("body = %v", b)
	}
	if _, ok := b["snapshot_id"]; ok {
		t.Fatalf("body = %v", b)
	}
}

// fast_start is sent only when the caller sets it, and a 429 fast_start_unavailable reaches the
// caller as a tool error that names the reason and the cause (nothing is created, so the same call can be repeated).
func TestMicrovmCreate_FastStartIsSentOnlyWhenSet(t *testing.T) {
	mock := &snapshotMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.create", map[string]any{"name": "fast-1", "image_id": "img-1", "location_id": "loc-1", "fast_start": "require"})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	if b := mock.bodies[len(mock.bodies)-1]; b["fast_start"] != "require" {
		t.Fatalf("body = %v", b)
	}
	callTool(t, cs, "user.microvm.create", map[string]any{"name": "plain-2", "image_id": "img-1", "location_id": "loc-1"})
	if b := mock.bodies[len(mock.bodies)-1]; b["fast_start"] != nil {
		t.Fatalf("an unset fast_start must not be sent: %v", b)
	}
}

func TestMicrovmCreate_FastStartUnavailableSurfacesReasonAndMessage(t *testing.T) {
	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"success": false, "reason": "fast_start_unavailable", "cause": "pool_empty", "retry_after_seconds": 1,
			"message": "No instantly startable sandbox is ready right now. Try again in a moment, or start without fast_start=require.",
		})
	})
	cs := connectSession(t, mux)

	res := callTool(t, cs, "user.microvm.create", map[string]any{"name": "fast-3", "image_id": "img-1", "location_id": "loc-1", "fast_start": "require"})

	if !res.IsError {
		t.Fatal("a 429 must be a tool error")
	}
	text := resultText(t, res)
	for _, want := range []string{"429", "fast_start_unavailable", "No instantly startable sandbox"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q lacks %q", text, want)
		}
	}
	if calls < 1 {
		t.Fatal("the create was never sent")
	}
}
