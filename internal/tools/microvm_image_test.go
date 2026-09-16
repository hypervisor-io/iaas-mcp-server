package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func microvmImageMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /microvm/images", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["base_image_id"] == nil || body["base_image_id"] == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Choose a base image.",
				"errors":  map[string]any{"base_image_id": []string{"Choose a base image."}},
			})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"image": map[string]any{
				"id":            "img-1",
				"name":          body["name"],
				"source_kind":   body["source_kind"],
				"status":        "building",
				"base_image_id": body["base_image_id"],
				"location_id":   body["location_id"],
			},
		})
	})

	mux.HandleFunc("GET /microvm/images", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"images": map[string]any{
				"current_page": 1, "last_page": 1,
				"data": []any{map[string]any{
					"id": "img-1", "name": "debian-13", "source_kind": "base", "status": "ready",
					// C5: os/features pass through verbatim; the tool layer
					// must not filter them.
					"os":       map[string]any{"id": "debian-13", "family": "debian", "name": "Debian", "version": "13"},
					"features": map[string]any{"sshd": true, "envd": false, "vcagent": true},
				}},
			},
		})
	})

	mux.HandleFunc("GET /microvm/image/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Image not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"image": map[string]any{
				"id": id, "name": "my-worker-image", "status": "ready",
				"os":       map[string]any{"id": "debian-13", "family": "debian"},
				"features": map[string]any{"sshd": true},
			},
			"versions":       []any{map[string]any{"id": "ver-1", "version": float64(1)}},
			"microvms_count": float64(2),
		})
	})

	mux.HandleFunc("POST /microvm/image/{id}/build", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"version": map[string]any{"id": "ver-2", "version": float64(2), "status": "building", "location_id": body["location_id"]},
		})
	})

	mux.HandleFunc("DELETE /microvm/image/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "in-use" {
			writeJSON(w, http.StatusConflict, map[string]any{"message": "image_in_use"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Image deleted."})
	})

	return mux
}

func TestMicrovmImage_CreateRequiresBaseImageID(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.create", map[string]any{
		"name":        "my-worker-image",
		"source_kind": "dockerfile",
		"dockerfile":  "FROM base",
		"location_id": "loc-1",
	})
	// base_image_id is a required field on the tool's JSON schema (C6), so
	// the MCP SDK rejects the call before it ever reaches the mock API.
	if !res.IsError || !strings.Contains(resultText(t, res), "base_image_id") {
		t.Fatalf("create without base_image_id: want a schema error naming base_image_id, got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.image.create", map[string]any{
		"name":          "my-worker-image",
		"source_kind":   "dockerfile",
		"dockerfile":    "FROM base",
		"location_id":   "loc-1",
		"base_image_id": "base-1",
	})
	if res.IsError {
		t.Fatalf("create with base_image_id failed: %s", resultText(t, res))
	}
	var created tools.MicrovmImageResult
	unmarshalResult(t, res, &created)
	if created.Image["base_image_id"] != "base-1" {
		t.Errorf("base_image_id = %v, want base-1", created.Image["base_image_id"])
	}
}

func TestMicrovmImage_ListGetSurfaceOsAndFeatures(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.list", map[string]any{})
	var list tools.MicrovmImageListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Fatalf("list count = %d, want 1", list.Count)
	}
	os, _ := list.Images[0]["os"].(map[string]any)
	if os["family"] != "debian" {
		t.Errorf("list image os.family = %v, want debian", os["family"])
	}
	features, _ := list.Images[0]["features"].(map[string]any)
	if features["sshd"] != true {
		t.Errorf("list image features.sshd = %v, want true", features["sshd"])
	}

	res = callTool(t, cs, "user.microvm.image.get", map[string]any{"id": "img-1"})
	var show tools.MicrovmImageShowResult
	unmarshalResult(t, res, &show)
	if show.MicrovmsCount != 2 {
		t.Errorf("microvms_count = %d, want 2", show.MicrovmsCount)
	}
	os, _ = show.Image["os"].(map[string]any)
	if os["id"] != "debian-13" {
		t.Errorf("get image os.id = %v, want debian-13", os["id"])
	}

	res = callTool(t, cs, "user.microvm.image.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get missing: want not found, got %q", resultText(t, res))
	}
}

func TestMicrovmImage_BuildAndDeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.build", map[string]any{"id": "img-1", "location_id": "loc-1"})
	var version tools.MicrovmImageVersionResult
	unmarshalResult(t, res, &version)
	if version.Version["id"] != "ver-2" {
		t.Errorf("build version id = %v, want ver-2", version.Version["id"])
	}

	res = callTool(t, cs, "user.microvm.image.delete", map[string]any{"id": "img-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.microvm.image.delete", map[string]any{"id": "img-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}

	res = callTool(t, cs, "user.microvm.image.delete", map[string]any{"id": "in-use", "confirm": true})
	if !res.IsError {
		t.Errorf("delete of an in-use image should fail")
	}
}
