package tools_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// adminProxmoxMockState records what actually reached the backend, so the
// confirm-gate case on admin.instance.migrate can assert zero HTTP calls when
// confirm is omitted.
type adminProxmoxMockState struct {
	mu           sync.Mutex
	migrateCalls int
	lastQuery    url.Values
	lastBody     map[string]any
}

// adminProxmoxMock serves the admin API paths under /v1/... that
// admin_proxmox.go's client methods hit (client/proxmox.go's Admin*
// methods): node-issues (paginator list), hypervisor-group backup-jobs
// (envelope-wrapped under "jobs"), and instance proxmox-migrate(/precheck).
func adminProxmoxMock(st *adminProxmoxMockState) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/proxmox/node-issues", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.lastQuery = r.URL.Query()
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{
				map[string]any{"id": "issue-1", "status": "open", "type": "webssh_proxy_install"},
			},
		})
	})
	mux.HandleFunc("POST /v1/proxmox/node-issues/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "retry queued"})
	})
	mux.HandleFunc("POST /v1/proxmox/node-issues/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "resolved"})
	})

	mux.HandleFunc("GET /v1/hypervisor-group/{groupId}/backup-jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"jobs": []any{
				map[string]any{"id": "job-1", "schedule": "0 3 * * *"},
			},
		})
	})
	mux.HandleFunc("POST /v1/hypervisor-group/{groupId}/backup-jobs", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.lastBody = body
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"id": "job-new", "schedule": body["schedule"]})
	})
	mux.HandleFunc("PUT /v1/hypervisor-group/{groupId}/backup-jobs/{jobId}", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.lastBody = body
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("jobId"), "schedule": body["schedule"]})
	})
	mux.HandleFunc("DELETE /v1/hypervisor-group/{groupId}/backup-jobs/{jobId}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})

	mux.HandleFunc("GET /v1/instance/{id}/proxmox-migrate/precheck", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.lastQuery = r.URL.Query()
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"allowed": true, "local_disks": []any{}})
	})
	mux.HandleFunc("POST /v1/instance/{id}/proxmox-migrate", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.migrateCalls++
		st.lastBody = body
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "migration started"})
	})

	return mux
}

func TestAdminProxmox_NodeIssues(t *testing.T) {
	st := &adminProxmoxMockState{}
	cs := connectSession(t, adminProxmoxMock(st))

	res := callTool(t, cs, "admin.proxmox.node_issue.list", map[string]any{"status": "open"})
	var list tools.AdminListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Items[0]["id"] != "issue-1" {
		t.Fatalf("node_issue.list = %+v, want one issue-1", list)
	}
	st.mu.Lock()
	gotStatus := st.lastQuery.Get("status")
	st.mu.Unlock()
	if gotStatus != "open" {
		t.Errorf("node_issue.list reached mux with status=%q, want open", gotStatus)
	}

	res = callTool(t, cs, "admin.proxmox.node_issue.retry", map[string]any{"issue_id": "issue-1"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Fatalf("node_issue.retry = %+v, want ok", ok)
	}

	res = callTool(t, cs, "admin.proxmox.node_issue.resolve", map[string]any{"issue_id": "issue-1"})
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Fatalf("node_issue.resolve = %+v, want ok", ok)
	}
}

func TestAdminProxmox_BackupJobs(t *testing.T) {
	st := &adminProxmoxMockState{}
	cs := connectSession(t, adminProxmoxMock(st))

	res := callTool(t, cs, "admin.backup_job.list", map[string]any{"group_id": "grp-1"})
	var list tools.AdminListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Items[0]["id"] != "job-1" {
		t.Fatalf("backup_job.list = %+v, want one job-1", list)
	}

	res = callTool(t, cs, "admin.backup_job.create", map[string]any{
		"group_id": "grp-1",
		"data": map[string]any{
			"target_type": "all",
			"storage":     "local",
			"schedule":    "0 3 * * *",
			"mode":        "snapshot",
			"keep-daily":  float64(7),
		},
	})
	var item tools.AdminItemResult
	unmarshalResult(t, res, &item)
	if item.Item["schedule"] != "0 3 * * *" {
		t.Fatalf("backup_job.create result = %+v, want schedule 0 3 * * *", item.Item)
	}
	st.mu.Lock()
	gotBody := st.lastBody
	st.mu.Unlock()
	if gotBody["storage"] != "local" || gotBody["keep-daily"] != float64(7) {
		t.Errorf("backup_job.create body = %v, want the full data object round-tripped", gotBody)
	}

	res = callTool(t, cs, "admin.backup_job.update", map[string]any{
		"group_id": "grp-1", "job_id": "job-1",
		"data": map[string]any{"schedule": "0 4 * * *"},
	})
	unmarshalResult(t, res, &item)
	if item.Item["schedule"] != "0 4 * * *" {
		t.Fatalf("backup_job.update result = %+v, want schedule 0 4 * * *", item.Item)
	}

	// delete without confirm refuses.
	res = callTool(t, cs, "admin.backup_job.delete", map[string]any{"group_id": "grp-1", "job_id": "job-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("backup_job.delete without confirm should refuse; got %q", resultText(t, res))
	}
	// delete with confirm succeeds.
	res = callTool(t, cs, "admin.backup_job.delete", map[string]any{"group_id": "grp-1", "job_id": "job-1", "confirm": true})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Fatalf("backup_job.delete with confirm = %+v, want ok", ok)
	}
}

func TestAdminProxmox_MigratePrecheck(t *testing.T) {
	st := &adminProxmoxMockState{}
	cs := connectSession(t, adminProxmoxMock(st))

	res := callTool(t, cs, "admin.instance.migrate_precheck", map[string]any{"instance_id": "inst-1", "target_node": "pve-02"})
	var item tools.AdminItemResult
	unmarshalResult(t, res, &item)
	if item.Item["allowed"] != true {
		t.Fatalf("migrate_precheck result = %+v, want allowed true", item.Item)
	}
	st.mu.Lock()
	gotTarget := st.lastQuery.Get("target_node")
	st.mu.Unlock()
	if gotTarget != "pve-02" {
		t.Errorf("migrate_precheck reached mux with target_node=%q, want pve-02", gotTarget)
	}
}

func TestAdminProxmox_MigrateConfirmGate(t *testing.T) {
	st := &adminProxmoxMockState{}
	cs := connectSession(t, adminProxmoxMock(st))

	// Without confirm: refused before any HTTP call.
	res := callTool(t, cs, "admin.instance.migrate", map[string]any{
		"instance_id": "inst-1", "target_node": "pve-02",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("migrate without confirm should refuse; got %q", resultText(t, res))
	}
	st.mu.Lock()
	calls := st.migrateCalls
	st.mu.Unlock()
	if calls != 0 {
		t.Fatalf("migrate without confirm reached the backend %d times, want 0", calls)
	}

	// With confirm: options post through.
	res = callTool(t, cs, "admin.instance.migrate", map[string]any{
		"instance_id": "inst-1", "target_node": "pve-02",
		"online": true, "bwlimit": float64(1024), "targetstorage": "local-lvm",
		"migration_network": "10.10.0.0/24", "confirm": true,
	})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Fatalf("migrate with confirm = %+v, want ok", ok)
	}
	st.mu.Lock()
	calls = st.migrateCalls
	gotBody := st.lastBody
	st.mu.Unlock()
	if calls != 1 {
		t.Fatalf("migrate with confirm hit the backend %d times, want 1", calls)
	}
	if gotBody["target_node"] != "pve-02" || gotBody["online"] != true || gotBody["bwlimit"] != float64(1024) ||
		gotBody["targetstorage"] != "local-lvm" || gotBody["migration_network"] != "10.10.0.0/24" {
		t.Errorf("migrate body = %v, want all options posted through", gotBody)
	}
}
