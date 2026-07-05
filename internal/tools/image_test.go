package tools_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func imageMock() http.Handler {
	mux := http.NewServeMux()
	var mu sync.Mutex
	polls := 0

	mux.HandleFunc("POST /images", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "image": map[string]any{"id": "img-1", "status": "creating"},
		})
	})
	mux.HandleFunc("GET /images", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polls++
		status := "available"
		if polls < 2 {
			status = "creating"
		}
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "img-1", "name": "golden", "status": status}},
		})
	})
	mux.HandleFunc("DELETE /image/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	return mux
}

func TestImage_CreateConvergesToAvailable(t *testing.T) {
	cs := connectSession(t, imageMock())
	res := callTool(t, cs, "user.image.create", map[string]any{"instance_id": "inst-1", "name": "golden"})
	var created tools.ImageResult
	unmarshalResult(t, res, &created)
	if created.Image["id"] != "img-1" {
		t.Fatalf("create id = %v, want img-1", created.Image["id"])
	}
	if created.Image["status"] != "available" {
		t.Errorf("create status = %v, want available (converged)", created.Image["status"])
	}
}

func TestImage_ListGetDelete(t *testing.T) {
	cs := connectSession(t, imageMock())

	res := callTool(t, cs, "user.image.list", map[string]any{})
	var list tools.ImageListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.image.get", map[string]any{"id": "img-1"})
	var got tools.ImageResult
	unmarshalResult(t, res, &got)
	if got.Image["id"] != "img-1" {
		t.Errorf("get id = %v, want img-1", got.Image["id"])
	}

	res = callTool(t, cs, "user.image.get", map[string]any{"id": "nope"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.image.delete", map[string]any{"id": "img-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.image.delete", map[string]any{"id": "img-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}
