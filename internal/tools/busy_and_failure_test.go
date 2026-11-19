package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"

	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// connectProductionClientSession is connectSession with the client the server
// really builds (iaasauth.NewClient: the short busy-refusal budget).
func connectProductionClientSession(t *testing.T, handler http.Handler) *mcp.ClientSession {
	t.Helper()
	mock := httptest.NewServer(handler)
	t.Cleanup(mock.Close)
	deps := tools.Deps{TokenSource: func(_ context.Context) (*client.Client, error) {
		return iaasauth.NewClient(mock.URL, "test-token", 20*time.Second, false), nil
	}}
	server := mcp.NewServer(&mcp.Implementation{Name: "iaas-mcp-server", Version: "test"}, nil)
	tools.RegisterAll(server, deps)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func busyHandler(reason string, busyFor int32, calls *int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(calls, 1) <= busyFor {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": reason,
				"message": "Another request is reserving addresses from the same pool. Repeat the request."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
}

// One busy answer is waited out (Retry-After honoured) and the tool succeeds.
func TestBusyRefusal_OneRetrySucceeds(t *testing.T) {
	var calls int32
	cs := connectProductionClientSession(t, busyHandler("concurrent_update_retry", 1, &calls))
	start := time.Now()
	res := callTool(t, cs, "user.vpc.delete", map[string]any{"id": "vpc-1", "confirm": true})
	if res.IsError {
		t.Fatalf("error: %s", resultText(t, res))
	}
	if atomic.LoadInt32(&calls) != 2 || time.Since(start) < 900*time.Millisecond {
		t.Errorf("calls=%d elapsed=%v; want 2 calls after about one Retry-After", calls, time.Since(start))
	}
}

// A persistent busy answer ends after the short budget (3 tries) with the
// server's message and a hint that repeating the call is safe.
func TestBusyRefusal_ShortBudgetThenHint(t *testing.T) {
	for _, reason := range []string{"address_reservation_busy", "concurrent_update_retry"} {
		var calls int32
		cs := connectProductionClientSession(t, busyHandler(reason, 1000, &calls))
		start := time.Now()
		res := callTool(t, cs, "user.vpc.delete", map[string]any{"id": "vpc-1", "confirm": true})
		if !res.IsError {
			t.Fatalf("%s: want an error result", reason)
		}
		txt := resultText(t, res)
		for _, want := range []string{"busy (" + reason + ")", "safe to repeat the same call", "Another request is reserving addresses", "reason: " + reason} {
			if !strings.Contains(txt, want) {
				t.Errorf("%s: error %q lacks %q", reason, txt, want)
			}
		}
		if got := atomic.LoadInt32(&calls); got != 3 {
			t.Errorf("%s: calls = %d, want 3", reason, got)
		}
		if time.Since(start) > 10*time.Second {
			t.Errorf("%s: took %v, the budget is about 6 s", reason, time.Since(start))
		}
	}
}

// A real conflict is returned at once, without the busy hint.
func TestBusyRefusal_OtherConflictNotRetried(t *testing.T) {
	var calls int32
	cs := connectProductionClientSession(t, busyHandler("plan_in_use", 1000, &calls))
	res := callTool(t, cs, "user.vpc.delete", map[string]any{"id": "vpc-1", "confirm": true})
	if !res.IsError || atomic.LoadInt32(&calls) != 1 || strings.Contains(resultText(t, res), "safe to repeat") {
		t.Fatalf("calls=%d res=%q", calls, resultText(t, res))
	}
}

// 500 database_error: the fixed sentence plus a plain hint, bounded retries.
func TestDatabaseError500_IsSurfaced(t *testing.T) {
	var calls int32
	cs := connectProductionClientSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "reason": "database_error",
			"message": "A database error occurred. Try again later."})
	}))
	res := callTool(t, cs, "user.vpc.delete", map[string]any{"id": "vpc-1", "confirm": true})
	txt := resultText(t, res)
	if !res.IsError || !strings.Contains(txt, "A database error occurred. Try again later.") || !strings.Contains(txt, "database_error") || !strings.Contains(txt, "try again later") {
		t.Fatalf("result = %q", txt)
	}
	if got := atomic.LoadInt32(&calls); got > 4 {
		t.Errorf("calls = %d; the client's 5xx retry is bounded at 4", got)
	}
}

// admin.system.email_log: the status filter is sent, validated, and the new
// row fields (status, error, null user_id, empty html_body) pass through.
func TestAdminEmailLog_StatusFilterAndNewFields(t *testing.T) {
	var gotStatus []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/system/email-log", func(w http.ResponseWriter, r *http.Request) {
		gotStatus = append(gotStatus, r.URL.Query().Get("status"))
		rows := []any{
			map[string]any{"id": "m1", "to": "a@example.com", "subject": "x", "html_body": "", "user_id": nil, "mailer": "smtp", "status": "failed", "error": "Connection refused"},
		}
		if r.URL.Query().Get("status") == "" {
			rows = append(rows, map[string]any{"id": "m2", "to": "b@example.com", "subject": "y", "html_body": "<p>hi</p>", "user_id": "u-1", "mailer": "smtp", "status": "sent", "error": nil})
		}
		writeJSON(w, http.StatusOK, map[string]any{"current_page": 1, "last_page": 1, "data": rows})
	})
	cs := connectSession(t, mux)

	var list tools.AdminListResult
	res := callTool(t, cs, "admin.system.email_log", map[string]any{"status": "failed"})
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Items[0]["status"] != "failed" || list.Items[0]["error"] != "Connection refused" || list.Items[0]["user_id"] != nil {
		t.Fatalf("failed rows = %+v", list)
	}
	res = callTool(t, cs, "admin.system.email_log", map[string]any{})
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("all rows = %+v", list)
	}
	if len(gotStatus) != 2 || gotStatus[0] != "failed" || gotStatus[1] != "" {
		t.Errorf("status sent = %q", gotStatus)
	}
	res = callTool(t, cs, "admin.system.email_log", map[string]any{"status": "bounced"})
	if !res.IsError || !strings.Contains(resultText(t, res), `status must be "failed" or "sent"`) || len(gotStatus) != 2 {
		t.Errorf("invalid status: %q (requests %d)", resultText(t, res), len(gotStatus))
	}
}

// The task read tools return failed tasks unchanged: message and
// failure_reason (including null) reach the caller, and the descriptions name
// the closed list.
func TestTaskTools_PassFailureReasonThrough(t *testing.T) {
	mux := http.NewServeMux()
	failed := map[string]any{"id": "t1", "status": "failed", "message": "The guest did not release the disk.", "failure_reason": "detach_not_confirmed"}
	old := map[string]any{"id": "t0", "status": "failed", "message": "x", "failure_reason": nil}
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"current_page": 1, "last_page": 1, "data": []any{failed, old}})
	})
	mux.HandleFunc("GET /v1/task/{id}", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, failed) })
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"current_page": 1, "last_page": 1, "data": []any{failed, old}})
	})
	cs := connectSession(t, mux)

	var list tools.AdminListResult
	unmarshalResult(t, callTool(t, cs, "admin.task.list", map[string]any{}), &list)
	if list.Items[0]["failure_reason"] != "detach_not_confirmed" || list.Items[1]["failure_reason"] != nil {
		t.Errorf("admin.task.list = %+v", list.Items)
	}
	var item tools.AdminItemResult
	unmarshalResult(t, callTool(t, cs, "admin.task.get", map[string]any{"id": "t1"}), &item)
	if item.Item["failure_reason"] != "detach_not_confirmed" || item.Item["message"] != "The guest did not release the disk." {
		t.Errorf("admin.task.get = %+v", item)
	}
	res := callTool(t, cs, "user.task.list", map[string]any{})
	if res.IsError || !strings.Contains(resultText(t, res), "detach_not_confirmed") {
		t.Errorf("user.task.list = %s", resultText(t, res))
	}

	listed, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tl := range listed.Tools {
		switch tl.Name {
		case "admin.task.list", "admin.task.get", "user.task.list":
			seen++
			if !strings.Contains(tl.Description, "failure_reason") || !strings.Contains(tl.Description, "detach_not_confirmed") {
				t.Errorf("%s description lacks failure_reason: %q", tl.Name, tl.Description)
			}
		}
	}
	if seen != 3 {
		t.Errorf("saw %d task tools", seen)
	}
}

// The managed database admission answers 409 database_busy WITHOUT Retry-After:
// a real conflict, sent once, with no busy hint.
func TestBusyRefusal_ManagedDatabaseAdmissionIsNotBusyRetry(t *testing.T) {
	var calls int32
	cs := connectProductionClientSession(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "database_busy",
			"message": "Another operation is running on this database. Wait until it has finished, then start the backup again."})
	}))
	res := callTool(t, cs, "user.managed_database.backup", map[string]any{"id": "db-1"})
	txt := resultText(t, res)
	if !res.IsError || atomic.LoadInt32(&calls) != 1 || strings.Contains(txt, "safe to repeat") || !strings.Contains(txt, "Another operation is running on this database") {
		t.Fatalf("calls=%d result=%q", calls, txt)
	}
}
