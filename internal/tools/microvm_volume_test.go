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

// sandboxVolumeMock is the sandbox volume API as the platform answers it, with a recorder of every request.
type sandboxVolumeMock struct {
	mu      sync.Mutex
	calls   []string
	bodies  []map[string]any
	headers []string
}

func (m *sandboxVolumeMock) record(r *http.Request) map[string]any {
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

func sandboxVolObj(id, status string, extra map[string]any) map[string]any {
	o := map[string]any{"id": id, "name": "data", "for": "sandbox", "size_gib": 20, "status": status, "location_id": "loc-1"}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

func (m *sandboxVolumeMock) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /microvm/vm/{id}/volumes", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		if r.PathValue("id") == "foreign" {
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "reason": "sandbox_not_found", "message": "Sandbox not found."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volumes": []any{sandboxVolObj("v1", "attached", map[string]any{"slot": 0, "mount_path": "/mnt/data"}), sandboxVolObj("v2", "attached", map[string]any{"slot": 1})}})
	})
	mux.HandleFunc("POST /microvm/vm/{id}/volumes", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		if r.PathValue("id") == "running" {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "reason": "sandbox_state_invalid", "message": "stop it first"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volume": sandboxVolObj(body["volume_id"].(string), "attached", map[string]any{"mount_path": body["mount_path"]})})
	})
	mux.HandleFunc("DELETE /microvm/vm/{id}/volume/{vol}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "volume": sandboxVolObj(r.PathValue("vol"), "available", nil)})
	})
	mux.HandleFunc("POST /storage/volumes", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		if body["for"] == "sandbox" {
			writeJSON(w, http.StatusCreated, map[string]any{"success": true, "volume": sandboxVolObj("vol-new", "available", map[string]any{"name": body["name"]})})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "volume": map[string]any{"id": "kvm-1", "status": "available"}})
	})
	mux.HandleFunc("GET /storage/volume/{id}", func(w http.ResponseWriter, r *http.Request) {
		m.record(r)
		writeJSON(w, http.StatusOK, map[string]any{"volume": map[string]any{"id": r.PathValue("id"), "status": "available"}})
	})
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, r *http.Request) {
		body := m.record(r)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{"id": "vm-new", "name": body["name"], "state": "creating"}})
	})
	return mux
}

func TestMicrovmVolume_ListAndAttachAndDetachUseTheDocumentedRoutesAndBodies(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())

	res := callTool(t, cs, "user.microvm.volume_list", map[string]any{"sandbox_id": "vm-1"})
	if res.IsError {
		t.Fatalf("list: %s", resultText(t, res))
	}
	var list tools.MicrovmVolumeListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 || list.Volumes[0]["mount_path"] != "/mnt/data" {
		t.Fatalf("list = %+v", list)
	}

	res = callTool(t, cs, "user.microvm.volume_attach", map[string]any{"sandbox_id": "vm-1", "volume_id": "v9", "mount_path": "/data", "read_only": true, "noexec": false, "delete_on_sandbox_delete": true})
	if res.IsError {
		t.Fatalf("attach: %s", resultText(t, res))
	}
	var got tools.MicrovmVolumeResult
	unmarshalResult(t, res, &got)
	if got.Volume["id"] != "v9" || got.Volume["status"] != "attached" {
		t.Fatalf("attach = %+v", got)
	}
	attachIdx := 1
	if mock.calls[attachIdx] != "POST /microvm/vm/vm-1/volumes" || mock.headers[attachIdx] == "" {
		t.Fatalf("attach request = %s key=%q", mock.calls[attachIdx], mock.headers[attachIdx])
	}
	b := mock.bodies[attachIdx]
	if b["volume_id"] != "v9" || b["mount_path"] != "/data" || b["read_only"] != true || b["noexec"] != false || b["delete_on_sandbox_delete"] != true {
		t.Fatalf("body = %v", b)
	}

	res = callTool(t, cs, "user.microvm.volume_detach", map[string]any{"sandbox_id": "vm-1", "volume_id": "v9", "confirm": true})
	if res.IsError || mock.calls[2] != "DELETE /microvm/vm/vm-1/volume/v9" {
		t.Fatalf("detach = %v calls=%v", resultText(t, res), mock.calls)
	}
}

func TestMicrovmVolume_AttachOmitsUnsetOptionalFieldsAndRefusalsCarryTheirReason(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	callTool(t, cs, "user.microvm.volume_attach", map[string]any{"sandbox_id": "vm-1", "volume_id": "v1"})
	for _, k := range []string{"mount_path", "read_only", "noexec", "delete_on_sandbox_delete"} {
		if _, ok := mock.bodies[0][k]; ok {
			t.Errorf("body must not carry %s: %v", k, mock.bodies[0])
		}
	}
	res := callTool(t, cs, "user.microvm.volume_attach", map[string]any{"sandbox_id": "running", "volume_id": "v1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "sandbox_state_invalid") {
		t.Fatalf("a running sandbox must be refused with its reason: %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm.volume_list", map[string]any{"sandbox_id": "foreign"})
	if !res.IsError || !strings.Contains(resultText(t, res), "sandbox_not_found") {
		t.Fatalf("foreign sandbox: %q", resultText(t, res))
	}
}

func TestMicrovmVolume_DetachNeedsConfirmation(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.volume_detach", map[string]any{"sandbox_id": "vm-1", "volume_id": "v1"})
	if !res.IsError {
		t.Fatal("detach is confirm-gated")
	}
	for _, c := range mock.calls {
		if strings.HasPrefix(c, "DELETE") {
			t.Fatalf("an unconfirmed detach reached the API: %v", mock.calls)
		}
	}
}

func TestVolumeCreate_ForSandboxSendsTheSandboxBodyAndNeedsNoPlan(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.volume.create", map[string]any{"name": "scratch", "for": "sandbox", "size_gib": 20, "location_id": "loc-1", "delete_on_sandbox_delete": true})
	if res.IsError {
		t.Fatalf("create: %s", resultText(t, res))
	}
	b := mock.bodies[0]
	if mock.calls[0] != "POST /storage/volumes" || b["for"] != "sandbox" || b["size_gib"] != float64(20) || b["location_id"] != "loc-1" || b["delete_on_sandbox_delete"] != true {
		t.Fatalf("request = %s body = %v", mock.calls[0], b)
	}
	if _, ok := b["volume_plan_id"]; ok {
		t.Errorf("a sandbox volume has no plan: %v", b)
	}
	if len(mock.calls) != 1 {
		t.Errorf("the volume is available when the call returns; no polling expected: %v", mock.calls)
	}
}

func TestVolumeCreate_RefusesIncompleteInputBeforeTheNetwork(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	if res := callTool(t, cs, "user.volume.create", map[string]any{"name": "x", "for": "sandbox", "location_id": "loc-1"}); !res.IsError || !strings.Contains(resultText(t, res), "size_gib") {
		t.Fatalf("sandbox volume without size: %q", resultText(t, res))
	}
	if res := callTool(t, cs, "user.volume.create", map[string]any{"name": "x", "location_id": "loc-1"}); !res.IsError || !strings.Contains(resultText(t, res), "volume_plan_id") {
		t.Fatalf("KVM volume without plan: %q", resultText(t, res))
	}
	if len(mock.calls) != 0 {
		t.Fatalf("nothing should have been sent: %v", mock.calls)
	}
}

func TestMicrovmCreate_CarriesVolumesInOrderAndOmitsUnsetFlags(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"name": "vsb", "location_id": "loc-1", "image_id": "img-1",
		"volumes": []any{
			map[string]any{"volume_id": "v1", "mount_path": "/data", "read_only": true},
			map[string]any{"volume_id": "v2", "delete_on_sandbox_delete": true},
		},
	})
	if res.IsError {
		t.Fatalf("create: %s", resultText(t, res))
	}
	vols, _ := mock.bodies[0]["volumes"].([]any)
	if len(vols) != 2 {
		t.Fatalf("volumes = %v", mock.bodies[0]["volumes"])
	}
	first, second := vols[0].(map[string]any), vols[1].(map[string]any)
	if first["volume_id"] != "v1" || first["mount_path"] != "/data" || first["read_only"] != true {
		t.Fatalf("first = %v", first)
	}
	if _, ok := first["noexec"]; ok {
		t.Errorf("unset flag sent: %v", first)
	}
	if _, ok := second["mount_path"]; ok || second["delete_on_sandbox_delete"] != true {
		t.Fatalf("second = %v", second)
	}
	callTool(t, cs, "user.microvm.create", map[string]any{"name": "plain", "location_id": "loc-1", "image_id": "img-1"})
	if _, ok := mock.bodies[len(mock.bodies)-1]["volumes"]; ok {
		t.Error("no volumes configured: no volumes key")
	}
}

func TestMicrovmVolume_AttachSendsExplicitFalseFlagsAsFalse(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	callTool(t, cs, "user.microvm.volume_attach", map[string]any{"sandbox_id": "vm-1", "volume_id": "v1", "read_only": false, "noexec": true, "delete_on_sandbox_delete": false})
	b := mock.bodies[0]
	if b["read_only"] != false || b["noexec"] != true || b["delete_on_sandbox_delete"] != false {
		t.Fatalf("an explicit false must stay false on the wire: %v", b)
	}
}

func TestVolumeCreate_ForSandboxSendsAnExplicitFalseDeleteFlagAsFalse(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	callTool(t, cs, "user.volume.create", map[string]any{"name": "x", "for": "sandbox", "size_gib": 5, "location_id": "loc-1", "delete_on_sandbox_delete": false})
	if mock.bodies[0]["delete_on_sandbox_delete"] != false {
		t.Fatalf("body = %v", mock.bodies[0])
	}
}

func TestMicrovmCreate_VolumeFlagsKeepExplicitFalse(t *testing.T) {
	mock := &sandboxVolumeMock{}
	cs := connectSession(t, mock.handler())
	callTool(t, cs, "user.microvm.create", map[string]any{
		"name": "vsb", "location_id": "loc-1", "image_id": "img-1",
		"volumes": []any{map[string]any{"volume_id": "v1", "read_only": false, "noexec": false, "delete_on_sandbox_delete": false}},
	})
	vols, _ := mock.bodies[0]["volumes"].([]any)
	v := vols[0].(map[string]any)
	if v["read_only"] != false || v["noexec"] != false || v["delete_on_sandbox_delete"] != false {
		t.Fatalf("volume = %v", v)
	}
}
