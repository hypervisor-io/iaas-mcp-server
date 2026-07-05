package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// volumeMockState is a small stateful mock so the async create waiter (status
// creating -> available) and the snapshot resolve-by-name + delete-to-404 flows
// converge deterministically.
type volumeMockState struct {
	mu        sync.Mutex
	volPolls  int
	snapshots map[string]map[string]any // id -> snapshot object
}

func volumeMock() http.Handler {
	st := &volumeMockState{snapshots: map[string]map[string]any{}}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /storage/volumes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "volume": map[string]any{"id": "vol-1", "status": "creating"},
		})
	})
	mux.HandleFunc("GET /storage/volumes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "vol-1", "name": "data", "status": "available"}},
		})
	})
	mux.HandleFunc("GET /storage/volume/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Volume not found"})
			return
		}
		st.mu.Lock()
		st.volPolls++
		status := "available"
		if st.volPolls < 2 {
			status = "creating"
		}
		snaps := make([]any, 0, len(st.snapshots))
		for _, s := range st.snapshots {
			snaps = append(snaps, s)
		}
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"volume":  map[string]any{"id": id, "name": "data", "status": status, "snapshots": snaps},
		})
	})
	mux.HandleFunc("POST /storage/volume/{id}/attach", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volume": map[string]any{"id": r.PathValue("id"), "instance_id": "inst-1"}})
	})
	mux.HandleFunc("POST /storage/volume/{id}/detach", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volume": map[string]any{"id": r.PathValue("id"), "instance_id": nil}})
	})
	mux.HandleFunc("PATCH /storage/volume/{id}/resize", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "is_downgrade": false, "volume": map[string]any{"id": r.PathValue("id"), "size": 200}})
	})
	mux.HandleFunc("DELETE /storage/volume/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})

	// Snapshot create: enqueue and record it so FindVolumeSnapshotByName resolves it.
	mux.HandleFunc("POST /storage/volume/{id}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.snapshots["snap-1"] = map[string]any{"id": "snap-1", "name": "backup", "status": "available"}
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "queue": map[string]any{"id": "q-1"}})
	})
	mux.HandleFunc("DELETE /storage/volume/{id}/snapshot/{snapshotId}", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		delete(st.snapshots, r.PathValue("snapshotId"))
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "queue": map[string]any{"id": "q-2"}})
	})
	return mux
}

func TestVolume_CreateConvergesAndActions(t *testing.T) {
	cs := connectSession(t, volumeMock())

	res := callTool(t, cs, "user.volume.create", map[string]any{
		"name": "data", "volume_plan_id": "vp-1", "hypervisor_group_id": "hg-1",
	})
	var created tools.VolumeResult
	unmarshalResult(t, res, &created)
	if created.Volume["id"] != "vol-1" || created.Volume["status"] != "available" {
		t.Fatalf("create = %v, want vol-1/available", created.Volume)
	}

	res = callTool(t, cs, "user.volume.list", map[string]any{})
	var list tools.VolumeListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.volume.attach", map[string]any{"id": "vol-1", "instance_id": "inst-1"})
	var attached tools.VolumeResult
	unmarshalResult(t, res, &attached)
	if attached.Volume["instance_id"] != "inst-1" {
		t.Errorf("attach instance_id = %v, want inst-1", attached.Volume["instance_id"])
	}

	res = callTool(t, cs, "user.volume.resize", map[string]any{"id": "vol-1", "volume_plan_id": "vp-2"})
	var resized tools.VolumeResult
	unmarshalResult(t, res, &resized)
	if resized.Volume["id"] != "vol-1" {
		t.Errorf("resize returned %v", resized.Volume)
	}
}

func TestVolume_SnapshotLifecycle(t *testing.T) {
	cs := connectSession(t, volumeMock())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "backup"})
	var snap tools.SnapshotResult
	unmarshalResult(t, res, &snap)
	if snap.Snapshot["id"] != "snap-1" || snap.Snapshot["status"] != "available" {
		t.Fatalf("snapshot_create = %v, want snap-1/available", snap.Snapshot)
	}

	// Delete without confirm refuses.
	res = callTool(t, cs, "user.volume.snapshot_delete", map[string]any{"volume_id": "vol-1", "snapshot_id": "snap-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("snapshot_delete without confirm should refuse; got %q", resultText(t, res))
	}
	// With confirm, converges to 404.
	res = callTool(t, cs, "user.volume.snapshot_delete", map[string]any{"volume_id": "vol-1", "snapshot_id": "snap-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed snapshot delete did not converge")
	}
}

func TestVolume_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, volumeMock())
	res := callTool(t, cs, "user.volume.delete", map[string]any{"id": "vol-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.volume.delete", map[string]any{"id": "vol-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
