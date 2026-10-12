package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// volumeMockState is a small stateful mock so the async create waiter (status
// creating -> available), the CONFIRMED volume mutations and the snapshot
// resolve-by-name + delete-to-404 flows converge deterministically.
//
// Mutations follow the Master confirmed-operation contract: attach, detach,
// resize and delete only RECORD the operation (active_operation names it); the
// simulated node completes it on the next SHOW. A node failure clears
// active_operation and keeps the volume as it was; a refusal is a 422 with a
// typed reason and changes nothing.
type volumeMockState struct {
	mu        sync.Mutex
	volPolls  int
	snapshots map[string]map[string]any // id -> snapshot object

	instanceID any
	planID     string
	size       int
	active     string
	desired    map[string]any
	deleted    bool
	nodeFails  bool
	refuse     *[2]string // {reason, message}, answered once with 422
	mutations  int
	// fail500Once records the first mutation, then answers it 500 (an
	// uncertain outcome the client must never replay).
	fail500Once bool
}

func (st *volumeMockState) volume(id, status string) map[string]any {
	snaps := make([]any, 0, len(st.snapshots))
	for _, s := range st.snapshots {
		snaps = append(snaps, s)
	}
	var active any
	if st.active != "" {
		active = st.active
	}
	volStatus := status
	if st.instanceID != nil {
		volStatus = "attached"
	}
	return map[string]any{
		"id": id, "name": "data", "status": volStatus, "snapshots": snaps,
		"instance_id": st.instanceID, "volume_plan_id": st.planID, "size": st.size,
		"active_operation": active,
	}
}

func volumeMock() http.Handler { return volumeMockWith(&volumeMockState{}) }

func volumeMockWith(st *volumeMockState) http.Handler {
	st.snapshots = map[string]map[string]any{}
	if st.planID == "" {
		st.planID, st.size = "vp-1", 50
	}
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
		st.mu.Lock()
		defer st.mu.Unlock()
		if id == "missing" || st.deleted {
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "not_found", "message": "Volume not found."})
			return
		}
		st.volPolls++
		status := "available"
		if st.volPolls < 2 {
			status = "creating"
		}
		// The node completes a recorded operation before this read.
		if st.active != "" {
			if !st.nodeFails {
				switch st.active {
				case "attach", "detach":
					st.instanceID = st.desired["instance_id"]
				case "resize":
					st.planID, st.size = st.desired["volume_plan_id"].(string), 100
				case "delete":
					st.deleted, st.active = true, ""
					writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "not_found", "message": "Volume not found."})
					return
				}
			}
			st.active = ""
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volume": st.volume(id, status)})
	})
	record := func(action string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			st.mu.Lock()
			defer st.mu.Unlock()
			st.mutations++
			if st.refuse != nil {
				ref := *st.refuse
				st.refuse = nil
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"success": false, "reason": ref[0], "message": ref[1]})
				return
			}
			switch action {
			case "attach":
				st.desired = map[string]any{"instance_id": body["instance_id"]}
			case "detach":
				st.desired = map[string]any{"instance_id": nil}
			case "resize":
				st.desired = map[string]any{"volume_plan_id": body["volume_plan_id"]}
			}
			st.active = action
			if st.fail500Once {
				st.fail500Once = false
				writeJSON(w, http.StatusInternalServerError, map[string]any{"message": "Server Error"})
				return
			}
			if action == "delete" {
				writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Volume deletion initiated."})
				return
			}
			// The accepted answer still shows the OLD state plus the marker.
			out := map[string]any{"success": true, "volume": st.volume(r.PathValue("id"), "available")}
			if action == "resize" {
				out["is_downgrade"] = false
			}
			writeJSON(w, http.StatusOK, out)
		}
	}
	mux.HandleFunc("POST /storage/volume/{id}/attach", record("attach"))
	mux.HandleFunc("POST /storage/volume/{id}/detach", record("detach"))
	mux.HandleFunc("PATCH /storage/volume/{id}/resize", record("resize"))
	mux.HandleFunc("DELETE /storage/volume/{id}", record("delete"))

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
		"name": "data", "volume_plan_id": "vp-1", "location_id": "hg-1",
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

	// Each mutation waits for the node: the accepted answer still carries the
	// old state, so a tool that returned it would fail these checks.
	res = callTool(t, cs, "user.volume.attach", map[string]any{"id": "vol-1", "instance_id": "inst-1"})
	var attached tools.VolumeResult
	unmarshalResult(t, res, &attached)
	if attached.Volume["instance_id"] != "inst-1" || attached.Volume["active_operation"] != nil {
		t.Errorf("attach = %v, want confirmed inst-1 with no active operation", attached.Volume)
	}

	res = callTool(t, cs, "user.volume.resize", map[string]any{"id": "vol-1", "volume_plan_id": "vp-2"})
	var resized tools.VolumeResult
	unmarshalResult(t, res, &resized)
	if resized.Volume["volume_plan_id"] != "vp-2" || resized.Volume["size"] != float64(100) {
		t.Errorf("resize = %v, want confirmed vp-2 / 100", resized.Volume)
	}

	res = callTool(t, cs, "user.volume.detach", map[string]any{"id": "vol-1"})
	var detached tools.VolumeResult
	unmarshalResult(t, res, &detached)
	if detached.Volume["instance_id"] != nil || detached.Volume["status"] != "available" {
		t.Errorf("detach = %v, want confirmed detached", detached.Volume)
	}
}

func TestVolume_NodeFailureIsToolError(t *testing.T) {
	st := &volumeMockState{nodeFails: true}
	cs := connectSession(t, volumeMockWith(st))

	res := callTool(t, cs, "user.volume.resize", map[string]any{"id": "vol-1", "volume_plan_id": "vp-2"})
	if !res.IsError {
		t.Fatalf("a node failure must be a tool error; got %s", resultText(t, res))
	}
	if txt := resultText(t, res); !strings.Contains(txt, "failed on the node") || !strings.Contains(txt, "kept its previous state") {
		t.Fatalf("error text = %q", txt)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.planID != "vp-1" {
		t.Fatalf("plan = %s; the mock must keep the old plan on failure", st.planID)
	}
}

func TestVolume_RefusalCarriesReason(t *testing.T) {
	st := &volumeMockState{refuse: &[2]string{"volume_operation_refused", "The node refused the volume operation."}}
	cs := connectSession(t, volumeMockWith(st))

	res := callTool(t, cs, "user.volume.attach", map[string]any{"id": "vol-1", "instance_id": "inst-1"})
	if !res.IsError {
		t.Fatalf("a refusal must be a tool error; got %s", resultText(t, res))
	}
	txt := resultText(t, res)
	if !strings.Contains(txt, "validation failed") || !strings.Contains(txt, "reason: volume_operation_refused") {
		t.Fatalf("error text = %q; want the 422 category and the reason code", txt)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.mutations != 1 {
		t.Fatalf("mutations = %d; a refusal is never re-sent", st.mutations)
	}
}

func TestVolume_Uncertain500IsFollowedNeverReplayed(t *testing.T) {
	st := &volumeMockState{fail500Once: true}
	cs := connectSession(t, volumeMockWith(st))

	res := callTool(t, cs, "user.volume.attach", map[string]any{"id": "vol-1", "instance_id": "inst-1"})
	var attached tools.VolumeResult
	unmarshalResult(t, res, &attached)
	if attached.Volume["instance_id"] != "inst-1" {
		t.Fatalf("attach = %v, want the recorded attach followed to completion", attached.Volume)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.mutations != 1 {
		t.Fatalf("mutations = %d; an uncertain 500 must never be replayed", st.mutations)
	}
}

func TestVolume_DeleteNodeFailureIsToolError(t *testing.T) {
	st := &volumeMockState{nodeFails: true}
	cs := connectSession(t, volumeMockWith(st))

	res := callTool(t, cs, "user.volume.delete", map[string]any{"id": "vol-1", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "failed on the node") {
		t.Fatalf("a delete the node failed must be a tool error, never deleted:true; got %q", resultText(t, res))
	}
}

func TestVolume_DeleteAttachedRefusedWithReason(t *testing.T) {
	st := &volumeMockState{refuse: &[2]string{"volume_attached", "Detach the volume before deleting it."}}
	cs := connectSession(t, volumeMockWith(st))

	res := callTool(t, cs, "user.volume.delete", map[string]any{"id": "vol-1", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "reason: volume_attached") {
		t.Fatalf("delete of an attached volume must surface volume_attached; got %q", resultText(t, res))
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
	// The delete waited for the node: the volume now reads 404.
	res = callTool(t, cs, "user.volume.get", map[string]any{"id": "vol-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Fatalf("after a confirmed delete the volume must read 404; got %q", resultText(t, res))
	}
}
