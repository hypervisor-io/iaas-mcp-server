package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// instanceProxmoxMockState is a small stateful mock recording how many times
// the snapshot-delete endpoint was actually hit, so the confirm-gate case can
// assert zero HTTP calls reached the backend.
type instanceProxmoxMockState struct {
	mu           sync.Mutex
	deleteCalls  int
	lastSnapshot map[string]any // last decoded request body for the endpoint under test
}

func instanceProxmoxMock(st *instanceProxmoxMockState) http.Handler {
	mux := http.NewServeMux()

	// GET /instance/{id}/snapshots -> {"snapshots": [...]}, the real envelope
	// key ListInstanceSnapshots unwraps.
	mux.HandleFunc("GET /instance/{id}/snapshots", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"snapshots": []any{
				map[string]any{"name": "current", "current": true},
				map[string]any{"name": "before-upgrade", "description": "pre-upgrade snapshot"},
			},
		})
	})

	// POST /instance/{id}/snapshot -> create. Body carries "snapname"/"vmstate",
	// not "name" (client translates it).
	mux.HandleFunc("POST /instance/{id}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		st.lastSnapshot = body
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "snapshot queued"})
	})

	// POST /instance/{id}/snapshot/rollback
	mux.HandleFunc("POST /instance/{id}/snapshot/rollback", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "rollback started"})
	})

	// DELETE /instance/{id}/snapshot (body-carrying delete)
	mux.HandleFunc("DELETE /instance/{id}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.deleteCalls++
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "snapshot deleted"})
	})

	// POST /instance/{id}/tags
	mux.HandleFunc("POST /instance/{id}/tags", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "tags updated"})
	})

	// GET /instance/{id}/guest-ips -> {"success":true,"data":[...]}, the generic
	// envelope doList unwraps. The Master's GuestAgentService normalizes the raw
	// QEMU guest-agent response into this flatter per-interface shape before
	// returning it to API consumers.
	mux.HandleFunc("GET /instance/{id}/guest-ips", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"data": []any{
				map[string]any{"nic": "eth0", "mac": "aa:bb", "ip": "203.0.113.9", "type": "ipv4"},
			},
		})
	})

	// GET /instance/{id}/backup/{backupId}/files -> {"success":true,"files":[...]},
	// returned bare (no unwrap) so the caller reads "files" themselves.
	mux.HandleFunc("GET /instance/{id}/backup/{backupId}/files", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"files": []any{
				map[string]any{"filepath": "/etc/hostname", "type": "f"},
			},
		})
	})

	return mux
}

func TestInstanceProxmox_SnapshotLifecycle(t *testing.T) {
	st := &instanceProxmoxMockState{}
	cs := connectSession(t, instanceProxmoxMock(st))

	// list
	res := callTool(t, cs, "user.instance.snapshot.list", map[string]any{"instance_id": "inst-1"})
	var list tools.SnapshotListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 || len(list.Snapshots) != 2 {
		t.Fatalf("snapshot list = %+v, want count 2", list)
	}
	if list.Snapshots[1]["name"] != "before-upgrade" {
		t.Errorf("snapshots[1].name = %v, want before-upgrade", list.Snapshots[1]["name"])
	}

	// create
	res = callTool(t, cs, "user.instance.snapshot.create", map[string]any{
		"instance_id": "inst-1", "name": "before-upgrade", "vmstate": true,
	})
	var created tools.OKResult
	unmarshalResult(t, res, &created)
	if !created.OK {
		t.Fatalf("snapshot create = %+v, want ok", created)
	}
	st.mu.Lock()
	gotBody := st.lastSnapshot
	st.mu.Unlock()
	if gotBody["snapname"] != "before-upgrade" || gotBody["vmstate"] != true {
		t.Errorf("snapshot create body = %v, want snapname=before-upgrade vmstate=true", gotBody)
	}

	// rollback without confirm refuses
	res = callTool(t, cs, "user.instance.snapshot.rollback", map[string]any{
		"instance_id": "inst-1", "name": "before-upgrade",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("rollback without confirm should refuse; got %q", resultText(t, res))
	}
	// rollback with confirm succeeds
	res = callTool(t, cs, "user.instance.snapshot.rollback", map[string]any{
		"instance_id": "inst-1", "name": "before-upgrade", "confirm": true,
	})
	var rolled tools.OKResult
	unmarshalResult(t, res, &rolled)
	if !rolled.OK {
		t.Fatalf("snapshot rollback = %+v, want ok", rolled)
	}

	// delete without confirm refuses AND makes zero HTTP calls
	res = callTool(t, cs, "user.instance.snapshot.delete", map[string]any{
		"instance_id": "inst-1", "name": "before-upgrade",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "refusing destructive operation") {
		t.Fatalf("delete without confirm should refuse with the destructive-op message; got %q", resultText(t, res))
	}
	st.mu.Lock()
	calls := st.deleteCalls
	st.mu.Unlock()
	if calls != 0 {
		t.Fatalf("delete without confirm reached the backend %d times, want 0", calls)
	}

	// delete with confirm succeeds and hits the backend once
	res = callTool(t, cs, "user.instance.snapshot.delete", map[string]any{
		"instance_id": "inst-1", "name": "before-upgrade", "confirm": true,
	})
	var deleted tools.OKResult
	unmarshalResult(t, res, &deleted)
	if !deleted.OK {
		t.Fatalf("snapshot delete = %+v, want ok", deleted)
	}
	st.mu.Lock()
	calls = st.deleteCalls
	st.mu.Unlock()
	if calls != 1 {
		t.Fatalf("delete with confirm hit the backend %d times, want 1", calls)
	}
}

func TestInstanceProxmox_TagsSet(t *testing.T) {
	cs := connectSession(t, instanceProxmoxMock(&instanceProxmoxMockState{}))

	res := callTool(t, cs, "user.instance.tags.set", map[string]any{
		"instance_id": "inst-1", "tags": "prod,web",
	})
	var out tools.OKResult
	unmarshalResult(t, res, &out)
	if !out.OK {
		t.Fatalf("tags.set = %+v, want ok", out)
	}
}

func TestInstanceProxmox_GuestIPs(t *testing.T) {
	cs := connectSession(t, instanceProxmoxMock(&instanceProxmoxMockState{}))

	res := callTool(t, cs, "user.instance.guest_ips", map[string]any{"instance_id": "inst-1"})
	var out tools.GuestIPsResult
	unmarshalResult(t, res, &out)
	if out.Count != 1 || len(out.Interfaces) != 1 {
		t.Fatalf("guest_ips = %+v, want count 1", out)
	}
	if out.Interfaces[0]["nic"] != "eth0" || out.Interfaces[0]["ip"] != "203.0.113.9" {
		t.Errorf("interfaces[0] = %v, want nic=eth0 ip=203.0.113.9", out.Interfaces[0])
	}
}

func TestInstanceProxmox_BackupFiles(t *testing.T) {
	cs := connectSession(t, instanceProxmoxMock(&instanceProxmoxMockState{}))

	res := callTool(t, cs, "user.instance.backup.files", map[string]any{
		"instance_id": "inst-1", "backup_id": "backup-1", "filepath": "/etc",
	})
	var out tools.BackupFilesResult
	unmarshalResult(t, res, &out)
	files, ok := out.Listing["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("backup.files listing = %+v, want one file entry", out.Listing)
	}
}
