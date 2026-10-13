package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// The 3.3.5 volume gate contract through the MCP tools: busy is 409 reason
// volume_busy, cooldown is 429 reason volume_cooldown with Retry-After (whole
// seconds), the account in-flight limit is 429 reason account_inflight_limit.
// The tools wait those out through the provider's volumeop package and never
// let the shared client replay a gated command; older masters' 422 text
// shapes keep working (volume_test.go).

type gateAnswer struct {
	status     int
	reason     string
	message    string
	retryAfter string
	record     bool // record the snapshot before answering (uncertain 5xx)
	foreign    bool // a same-name snapshot by someone else appears at this send
}

// gateMock serves one volume with snapshot and backup creation answering the
// scripted gate answers in order, then success.
type gateMock struct {
	mu        sync.Mutex
	answers   []gateAnswer
	snapshots int
	backups   int
	recorded  bool
	foreign   bool // a same-name snapshot by someone else exists
	gets      int
}

func (g *gateMock) next(w http.ResponseWriter) bool {
	if len(g.answers) == 0 {
		return false
	}
	a := g.answers[0]
	g.answers = g.answers[1:]
	if a.record {
		g.recorded = true
	}
	if a.foreign {
		g.foreign = true
	}
	if a.retryAfter != "" {
		w.Header().Set("Retry-After", a.retryAfter)
	}
	writeJSON(w, a.status, map[string]any{"success": false, "reason": a.reason, "message": a.message})
	return true
}

func (g *gateMock) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /storage/volume/{id}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.gets++
		snaps := []any{}
		if g.recorded {
			snaps = append(snaps, map[string]any{"id": "snap-1", "name": "nightly", "status": "available"})
		}
		if g.foreign {
			snaps = append(snaps, map[string]any{"id": "snap-foreign", "name": "nightly", "status": "available"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volume": map[string]any{
			"id": r.PathValue("id"), "status": "available", "active_operation": nil, "snapshots": snaps,
		}})
	})
	mux.HandleFunc("POST /storage/volume/{id}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.snapshots++
		if g.next(w) {
			return
		}
		g.recorded = true
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "queue": map[string]any{"id": "q-1"}})
	})
	mux.HandleFunc("POST /storage/volume/{id}/backup", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.backups++
		if g.next(w) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "queue": map[string]any{"id": "q-9", "operation": "backup"}})
	})
	return mux
}

func transientGateAnswers() []gateAnswer {
	return []gateAnswer{
		{status: http.StatusConflict, reason: "volume_busy", message: "Volume is busy with operation: backup. Please wait."},
		{status: http.StatusTooManyRequests, reason: "volume_cooldown", message: "Volume is in cooldown. Try again in 1 seconds.", retryAfter: "1"},
		{status: http.StatusTooManyRequests, reason: "account_inflight_limit", message: "Too many in-flight volume operations (3). Wait for some to finish."},
	}
}

func TestVolumeGate_SnapshotCreateWaitsOutTransientRefusals(t *testing.T) {
	g := &gateMock{answers: transientGateAnswers()}
	cs := connectSession(t, g.handler())

	start := time.Now()
	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
	var snap tools.SnapshotResult
	unmarshalResult(t, res, &snap)
	if snap.Snapshot["id"] != "snap-1" {
		t.Fatalf("snapshot_create = %v", snap.Snapshot)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("elapsed %v; the cooldown's Retry-After must be waited out", elapsed)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.snapshots != 4 {
		t.Fatalf("snapshot creates = %d; want 4 (three transient refusals, then accepted)", g.snapshots)
	}
}

func TestVolumeGate_BackupCreateWaitsOutTransientRefusals(t *testing.T) {
	g := &gateMock{answers: transientGateAnswers()}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.backup_create", map[string]any{"volume_id": "vol-1"})
	if res.IsError {
		t.Fatalf("backup_create: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "q-9") {
		t.Fatalf("backup_create = %s; want the queue handle", resultText(t, res))
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backups != 4 {
		t.Fatalf("backup creates = %d; want 4", g.backups)
	}
}

func TestVolumeGate_DefiniteRefusalCarriesReason(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusConflict, reason: "volume_backup_limit", message: "Backup limit reached (3/3). Delete an existing backup first."}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.backup_create", map[string]any{"volume_id": "vol-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "reason: volume_backup_limit") {
		t.Fatalf("want a tool error with reason volume_backup_limit; got %q", resultText(t, res))
	}
	if txt := resultText(t, res); strings.Contains(txt, "not repeated") || strings.Contains(txt, "no definite answer") {
		t.Fatalf("a definite refusal must not be reported as an unknown outcome: %q", txt)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backups != 1 {
		t.Fatalf("backup creates = %d; a definite refusal is never re-sent", g.backups)
	}
}

// A definite snapshot refusal is returned as it is: no uncertain-answer
// read-back (only the pre-send name check reads the volume) and no
// "no definite answer" wording.
func TestVolumeGate_SnapshotDefiniteRefusalHasNoReadBack(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusConflict, reason: "volume_snapshot_limit", message: "Snapshot limit reached (5/5)."}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
	txt := resultText(t, res)
	if !res.IsError || !strings.Contains(txt, "reason: volume_snapshot_limit") || strings.Contains(txt, "no definite answer") {
		t.Fatalf("want the definite refusal with its reason; got %q", txt)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.gets != 1 || g.snapshots != 1 {
		t.Fatalf("volume reads = %d, creates = %d; want 1 and 1 (name check, one send, no read-back)", g.gets, g.snapshots)
	}
}

// A snapshot of the same name already on the volume: refused before any send,
// so a snapshot the tool did not create is never returned.
func TestVolumeGate_SnapshotNameTakenIsRefusedBeforeSend(t *testing.T) {
	for _, answers := range [][]gateAnswer{
		nil, // the create would succeed
		{{status: http.StatusServiceUnavailable, message: "Service Unavailable"}}, // or be uncertain
	} {
		g := &gateMock{answers: answers, foreign: true}
		cs := connectSession(t, g.handler())

		res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
		if !res.IsError || !strings.Contains(resultText(t, res), "already exists on volume") {
			t.Fatalf("want a tool error naming the taken name; got %q", resultText(t, res))
		}
		g.mu.Lock()
		if g.snapshots != 0 {
			t.Fatalf("snapshot creates = %d; a taken name is refused before any send", g.snapshots)
		}
		g.mu.Unlock()
	}
}

// Another creator's same-name snapshot appears next to ours during an
// uncertain create: the tool reports the ambiguity and adopts neither.
func TestVolumeGate_UncertainSnapshotWithConcurrentSameNameIsAmbiguous(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusServiceUnavailable, message: "Service Unavailable", record: true, foreign: true}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
	if !res.IsError || !strings.Contains(resultText(t, res), "2 new snapshots named") {
		t.Fatalf("want a tool error naming the ambiguity; got %q", resultText(t, res))
	}
}

func TestVolumeGate_UncertainBackupIsNeverReplayed(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusServiceUnavailable, message: "Service Unavailable"}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.backup_create", map[string]any{"volume_id": "vol-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not repeated") {
		t.Fatalf("want a tool error saying the request was not repeated; got %q", resultText(t, res))
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.backups != 1 {
		t.Fatalf("backup creates = %d; a 503 must not be replayed (one more backup)", g.backups)
	}
}

func TestVolumeGate_UncertainSnapshotRecordedIsAdopted(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusServiceUnavailable, message: "Service Unavailable", record: true}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
	var snap tools.SnapshotResult
	unmarshalResult(t, res, &snap)
	if snap.Snapshot["id"] != "snap-1" {
		t.Fatalf("snapshot_create = %v", snap.Snapshot)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.snapshots != 1 {
		t.Fatalf("snapshot creates = %d; an uncertain answer is never replayed", g.snapshots)
	}
}

func TestVolumeGate_UncertainSnapshotNotRecordedIsError(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusServiceUnavailable, message: "Service Unavailable"}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
	if !res.IsError || !strings.Contains(resultText(t, res), "no definite answer") {
		t.Fatalf("want a tool error naming the unknown outcome; got %q", resultText(t, res))
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.snapshots != 1 {
		t.Fatalf("snapshot creates = %d; want 1", g.snapshots)
	}
}

// Attach through volumeop against the typed shapes: 409 volume_busy while the
// volume shows no marker, then 429 volume_cooldown with Retry-After.
func TestVolumeGate_AttachWaitsOutTypedBusyAndCooldown(t *testing.T) {
	st := &volumeMockState{}
	h := volumeMockWith(st)
	var mu sync.Mutex
	script := []gateAnswer{
		{status: http.StatusConflict, reason: "volume_busy", message: "Please wait."},
		{status: http.StatusTooManyRequests, reason: "volume_cooldown", message: "Volume is in cooldown. Try again in 1 seconds.", retryAfter: "1"},
	}
	attaches := 0
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attach") {
			mu.Lock()
			attaches++
			if len(script) > 0 {
				a := script[0]
				script = script[1:]
				mu.Unlock()
				if a.retryAfter != "" {
					w.Header().Set("Retry-After", a.retryAfter)
				}
				writeJSON(w, a.status, map[string]any{"success": false, "reason": a.reason, "message": a.message})
				return
			}
			mu.Unlock()
		}
		h.ServeHTTP(w, r)
	})
	cs := connectSession(t, wrapped)

	start := time.Now()
	res := callTool(t, cs, "user.volume.attach", map[string]any{"id": "vol-1", "instance_id": "inst-1"})
	var attached tools.VolumeResult
	unmarshalResult(t, res, &attached)
	if attached.Volume["instance_id"] != "inst-1" {
		t.Fatalf("attach = %v", attached.Volume)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("elapsed %v; Retry-After must be waited out", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if attaches != 3 {
		t.Fatalf("attach requests = %d; want 3 (409, 429, accepted)", attaches)
	}
}

// An empty name is refused before anything is read or sent (the platform
// would name the snapshot itself and the tool could never find it).
func TestVolumeGate_SnapshotEmptyNameIsRefusedBeforeSend(t *testing.T) {
	g := &gateMock{}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": ""})
	if !res.IsError || !strings.Contains(resultText(t, res), "snapshot name is required") {
		t.Fatalf("want a tool error asking for a name; got %q", resultText(t, res))
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.snapshots != 0 || g.gets != 0 {
		t.Fatalf("creates = %d, reads = %d; an empty name is refused before anything", g.snapshots, g.gets)
	}
}

// A same-name snapshot made by someone else during an admission wait, then a
// definite success: ours is returned, never the foreign one, and no ambiguity
// is reported (the pre-send set is refreshed before the second send).
func TestVolumeGate_ForeignSameNameDuringAdmissionWaitIsNotOurs(t *testing.T) {
	g := &gateMock{answers: []gateAnswer{{status: http.StatusConflict, reason: "volume_busy", message: "Please wait.", foreign: true}}}
	cs := connectSession(t, g.handler())

	res := callTool(t, cs, "user.volume.snapshot_create", map[string]any{"volume_id": "vol-1", "name": "nightly"})
	var snap tools.SnapshotResult
	unmarshalResult(t, res, &snap)
	if snap.Snapshot["id"] != "snap-1" {
		t.Fatalf("snapshot_create = %v; want ours (snap-1), never snap-foreign", snap.Snapshot)
	}
}
