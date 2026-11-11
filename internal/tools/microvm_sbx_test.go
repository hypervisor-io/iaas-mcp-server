package tools_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// SBX-SMALL: metadata on create/list, the set_metadata merge patch, no_limit on
// set_timeout, and an Idempotency-Key on sandbox create and kit build.

type sbxRecorder struct {
	mu    sync.Mutex
	hdrs  map[string]string
	query map[string][]string
	body  map[string]any
}

func (r *sbxRecorder) mux() http.Handler {
	mux := http.NewServeMux()
	read := func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.body = nil
		_ = json.NewDecoder(req.Body).Decode(&r.body)
		r.query = req.URL.Query()
		r.hdrs[req.Method+" "+req.URL.Path] = req.Header.Get("Idempotency-Key")
	}
	mux.HandleFunc("POST /microvm/vms", func(w http.ResponseWriter, req *http.Request) {
		read(w, req)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "microvm": map[string]any{"id": "vm-1", "metadata": r.body["metadata"]}})
	})
	mux.HandleFunc("GET /microvm/vms", func(w http.ResponseWriter, req *http.Request) {
		read(w, req)
		writeJSON(w, http.StatusOK, map[string]any{"microvms": map[string]any{"current_page": 1, "last_page": 1, "data": []any{}}})
	})
	mux.HandleFunc("PATCH /microvm/vm/{id}/metadata", func(w http.ResponseWriter, req *http.Request) {
		read(w, req)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "metadata": map[string]any{"team": "pay"}})
	})
	mux.HandleFunc("POST /microvm/vm/{id}/timeout", func(w http.ResponseWriter, req *http.Request) {
		read(w, req)
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})
	mux.HandleFunc("POST /microvm/images", func(w http.ResponseWriter, req *http.Request) {
		read(w, req)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "image": map[string]any{"id": "img-1"}})
	})
	mux.HandleFunc("POST /microvm/image/{id}/build", func(w http.ResponseWriter, req *http.Request) {
		read(w, req)
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "version": map[string]any{"id": "ver-1"}})
	})
	return mux
}

func TestMicrovm_CreateSendsMetadataAndAnIdempotencyKey(t *testing.T) {
	rec := &sbxRecorder{hdrs: map[string]string{}}
	cs := connectSession(t, rec.mux())

	res := callTool(t, cs, "user.microvm.create", map[string]any{
		"location_id": "loc-1", "image_id": "img-1", "name": "worker",
		"metadata": map[string]string{"team": "pay"},
	})
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	meta, _ := rec.body["metadata"].(map[string]any)
	if meta["team"] != "pay" {
		t.Errorf("metadata sent = %v", rec.body["metadata"])
	}
	if rec.hdrs["POST /microvm/vms"] == "" {
		t.Error("create must send an Idempotency-Key")
	}
}

func TestMicrovm_ListFiltersByMetadata(t *testing.T) {
	rec := &sbxRecorder{hdrs: map[string]string{}}
	cs := connectSession(t, rec.mux())

	res := callTool(t, cs, "user.microvm.list", map[string]any{"metadata": map[string]string{"team": "pay", "env": "prod"}})
	if res.IsError {
		t.Fatalf("list failed: %s", resultText(t, res))
	}
	if rec.query["metadata[team]"][0] != "pay" || rec.query["metadata[env]"][0] != "prod" {
		t.Errorf("query = %v", rec.query)
	}
}

func TestMicrovm_SetMetadataSendsNullForRemoval(t *testing.T) {
	rec := &sbxRecorder{hdrs: map[string]string{}}
	cs := connectSession(t, rec.mux())

	res := callTool(t, cs, "user.microvm.set_metadata", map[string]any{"id": "vm-1", "metadata": map[string]any{"team": "pay", "old": nil}})
	if res.IsError {
		t.Fatalf("set_metadata failed: %s", resultText(t, res))
	}
	meta, _ := rec.body["metadata"].(map[string]any)
	if meta["team"] != "pay" {
		t.Errorf("patch = %v", rec.body)
	}
	if v, present := meta["old"]; !present || v != nil {
		t.Errorf("a removal must travel as an explicit null: %v", rec.body)
	}
	var out tools.MicrovmMetadataResult
	unmarshalResult(t, res, &out)
	if out.Metadata["team"] != "pay" {
		t.Errorf("result = %v", out.Metadata)
	}
}

func TestMicrovm_SetTimeoutNoLimitAndItsGuards(t *testing.T) {
	rec := &sbxRecorder{hdrs: map[string]string{}}
	cs := connectSession(t, rec.mux())

	res := callTool(t, cs, "user.microvm.set_timeout", map[string]any{"id": "vm-1", "no_limit": true})
	if res.IsError {
		t.Fatalf("no_limit failed: %s", resultText(t, res))
	}
	if rec.body["no_limit"] != true {
		t.Errorf("body = %v", rec.body)
	}
	if _, has := rec.body["timeout"]; has {
		t.Error("no_limit must not send timeout")
	}

	if res := callTool(t, cs, "user.microvm.set_timeout", map[string]any{"id": "vm-1", "no_limit": true, "timeout": 60}); !res.IsError {
		t.Error("timeout together with no_limit must be refused")
	}
	if res := callTool(t, cs, "user.microvm.set_timeout", map[string]any{"id": "vm-1"}); !res.IsError {
		t.Error("neither timeout nor no_limit must be refused")
	}
	if res := callTool(t, cs, "user.microvm.set_timeout", map[string]any{"id": "vm-1", "timeout": 3600}); res.IsError {
		t.Errorf("finite timeout failed: %s", resultText(t, res))
	}
}

func TestMicrovm_ImageBuildSendsAnIdempotencyKey(t *testing.T) {
	rec := &sbxRecorder{hdrs: map[string]string{}}
	cs := connectSession(t, rec.mux())

	res := callTool(t, cs, "user.microvm.image.build", map[string]any{"id": "img-1", "location_id": "loc-1"})
	if res.IsError {
		t.Fatalf("build failed: %s", resultText(t, res))
	}
	if rec.hdrs["POST /microvm/image/img-1/build"] == "" {
		t.Error("a kit build must send an Idempotency-Key")
	}
}

func TestMicrovm_ImageCreateSendsAnIdempotencyKey(t *testing.T) {
	rec := &sbxRecorder{hdrs: map[string]string{}}
	cs := connectSession(t, rec.mux())

	res := callTool(t, cs, "user.microvm.image.create", map[string]any{
		"name": "kit", "source_kind": "oci", "source_image": "ghcr.io/acme/demo:1", "location_id": "loc-1", "base_image_id": "base-1",
	})
	if res.IsError {
		t.Fatalf("image create failed: %s", resultText(t, res))
	}
	if rec.hdrs["POST /microvm/images"] == "" {
		t.Error("a kit create must send an Idempotency-Key")
	}
}
