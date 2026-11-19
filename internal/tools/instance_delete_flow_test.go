package tools_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// Fake panel for the instance delete flow: DELETE /cloud-service/instances/{id}
// and GET /instance/{id} follow the 3.3.8.8 contract (200 accepted, 409
// delete_in_progress on a repeat, delete_state deleting or failed on the
// instance, 404 once gone).
type deleteFlowPanel struct {
	deletes, shows atomic.Int32
	onDelete       func(n int32, w http.ResponseWriter)
	show           func(n int32) (int, map[string]any)
}

func (p *deleteFlowPanel) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /cloud-service/instances/{id}", func(w http.ResponseWriter, r *http.Request) {
		p.onDelete(p.deletes.Add(1), w)
	})
	mux.HandleFunc("GET /instance/{id}", func(w http.ResponseWriter, r *http.Request) {
		code, body := p.show(p.shows.Add(1))
		writeJSON(w, code, body)
	})
	return mux
}

func deleteAccepted(_ int32, w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Instance deletion queued"})
}

func TestInstanceDeleteFlow_AcceptedThenConfirmed(t *testing.T) {
	p := &deleteFlowPanel{onDelete: deleteAccepted, show: func(n int32) (int, map[string]any) {
		if n < 3 {
			return 200, map[string]any{"id": "inst-1", "delete_state": "deleting"}
		}
		return 404, map[string]any{"message": "Instance not found"}
	}}
	cs := connectSession(t, p.handler())
	res := callTool(t, cs, "user.instance.delete", map[string]any{"id": "inst-1", "confirm": true})
	var out tools.DeleteResult
	unmarshalResult(t, res, &out)
	if !out.Deleted || p.deletes.Load() != 1 || p.shows.Load() < 3 {
		t.Fatalf("out=%+v deletes=%d shows=%d", out, p.deletes.Load(), p.shows.Load())
	}
}

func TestInstanceDeleteFlow_409DeleteInProgressIsAccepted(t *testing.T) {
	p := &deleteFlowPanel{onDelete: func(_ int32, w http.ResponseWriter) {
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "delete_in_progress", "message": "This instance is already being deleted."})
	}, show: func(n int32) (int, map[string]any) {
		if n < 2 {
			return 200, map[string]any{"id": "inst-1", "delete_state": "deleting"}
		}
		return 404, map[string]any{}
	}}
	cs := connectSession(t, p.handler())
	res := callTool(t, cs, "user.instance.delete", map[string]any{"id": "inst-1", "confirm": true})
	var out tools.DeleteResult
	unmarshalResult(t, res, &out)
	if !out.Deleted || p.deletes.Load() != 1 {
		t.Fatalf("out=%+v deletes=%d", out, p.deletes.Load())
	}
}

func TestInstanceDeleteFlow_FailedStopsAtOnceWithDeleteError(t *testing.T) {
	p := &deleteFlowPanel{onDelete: deleteAccepted, show: func(n int32) (int, map[string]any) {
		return 200, map[string]any{"id": "inst-1", "status": 1, "delete_state": "failed",
			"delete_error": "The host did not confirm the delete after 5 attempts.", "delete_reason": "node_unconfirmed", "delete_attempts": 5}
	}}
	cs := connectSession(t, p.handler())
	res := callTool(t, cs, "user.instance.delete", map[string]any{"id": "inst-1", "confirm": true})
	if !res.IsError {
		t.Fatal("a failed delete must be an error result: the instance still exists")
	}
	msg := resultText(t, res)
	for _, want := range []string{"inst-1", "did not confirm the delete after 5 attempts", "node_unconfirmed", "again"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if strings.Contains(msg, "was not removed") || p.shows.Load() != 1 {
		t.Errorf("must stop on the first read without the generic wording: shows=%d msg=%q", p.shows.Load(), msg)
	}
}

func TestInstanceDeleteFlow_PublicIPInFlightGetsAHint(t *testing.T) {
	p := &deleteFlowPanel{onDelete: func(_ int32, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "120")
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "public_ip_operation_in_flight", "message": "A public IP change is in flight."})
	}, show: func(int32) (int, map[string]any) { return 200, map[string]any{} }}
	cs := connectSession(t, p.handler())
	res := callTool(t, cs, "user.instance.delete", map[string]any{"id": "inst-1", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "repeat the same call after 2m0s") {
		t.Fatalf("expected the hint, got error=%v %q", res.IsError, resultText(t, res))
	}
	if p.deletes.Load() != 1 || p.shows.Load() != 0 {
		t.Errorf("deletes=%d shows=%d; refused once, never polled", p.deletes.Load(), p.shows.Load())
	}
}

func TestInstanceGet_PassesDeleteStateThrough(t *testing.T) {
	p := &deleteFlowPanel{show: func(int32) (int, map[string]any) {
		return 200, map[string]any{"id": "inst-1", "delete_state": "failed", "delete_error": "x", "delete_reason": "r", "delete_attempts": 5}
	}}
	cs := connectSession(t, p.handler())
	res := callTool(t, cs, "user.instance.get", map[string]any{"id": "inst-1"})
	var out tools.InstanceResult
	unmarshalResult(t, res, &out)
	if out.Instance["delete_state"] != "failed" || out.Instance["delete_error"] != "x" || out.Instance["delete_reason"] != "r" {
		t.Fatalf("instance = %v", out.Instance)
	}
}
