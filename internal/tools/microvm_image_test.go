package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// microvmImageMock mocks the MVU2-1 image endpoints. CREATE echoes the
// received request body back under the returned image's "echo" key so the
// tests can pin exactly which fields the tool sent; INDEX echoes the query
// parameters into each item.
func microvmImageMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /microvm/images", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"images": map[string]any{
				"current_page": 1,
				"last_page":    1,
				"data": []any{
					map[string]any{
						"id": "img-1", "name": "ci-runner", "status": "ready",
						"queried_search": q.Get("search"), "queried_kind": q.Get("kind"), "queried_status": q.Get("status"),
					},
				},
			},
		})
	})

	mux.HandleFunc("GET /microvm/image/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Image not found."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":        true,
			"image":          map[string]any{"id": id, "name": "ci-runner", "status": "ready"},
			"versions":       []any{map[string]any{"id": "v-2", "version": 2, "status": "ready"}},
			"microvms_count": 2,
		})
	})

	mux.HandleFunc("POST /microvm/images", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"image":   map[string]any{"id": "img-new", "status": "pending", "echo": body},
		})
	})

	mux.HandleFunc("POST /microvm/image/{id}/build", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusCreated, map[string]any{
			"success": true,
			"version": map[string]any{"id": "v-3", "version": 3, "status": "pending", "echo": body},
		})
	})

	mux.HandleFunc("DELETE /microvm/image/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "in-use" {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "image_in_use"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Image deleted."})
	})

	return mux
}

func TestListMicrovmImages_FiltersReachQuery(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.list", map[string]any{
		"search": "ci", "kind": "dockerfile", "status": "ready",
	})
	var list tools.MicrovmImageListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 || list.Images[0]["id"] != "img-1" {
		t.Fatalf("list = %+v, want one row img-1", list)
	}
	first := list.Images[0]
	if first["queried_search"] != "ci" || first["queried_kind"] != "dockerfile" || first["queried_status"] != "ready" {
		t.Errorf("filters did not reach the query string: %v", first)
	}
}

func TestGetMicrovmImage_ReturnsEnvelope(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.get", map[string]any{"id": "img-1"})
	var got tools.MicrovmImageShowResult
	unmarshalResult(t, res, &got)
	if got.Image["id"] != "img-1" {
		t.Errorf("image id = %v, want img-1", got.Image["id"])
	}
	if len(got.Versions) != 1 || got.Versions[0]["version"] != float64(2) {
		t.Errorf("versions = %v, want one row version 2", got.Versions)
	}
	if got.MicrovmsCount != 2 {
		t.Errorf("microvms_count = %d, want 2", got.MicrovmsCount)
	}

	res = callTool(t, cs, "user.microvm.image.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}
}

func TestCreateMicrovmImage_MapsFlatInputsToNestedBody(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	// oci: source carries {image}; auth carries {kind, username, password};
	// unset optional fields must not appear in the body at all.
	res := callTool(t, cs, "user.microvm.image.create", map[string]any{
		"name":                "worker",
		"source_kind":         "oci",
		"source_image":        "registry.example.test/worker:latest",
		"hypervisor_group_id": "hg-1",
		"auth_kind":           "registry",
		"registry_username":   "robot",
		"registry_password":   "test-password-placeholder",
	})
	var out tools.MicrovmImageResult
	unmarshalResult(t, res, &out)
	echo, ok := out.Image["echo"].(map[string]any)
	if !ok {
		t.Fatalf("create result missing the echoed request body: %v", out.Image)
	}
	source, _ := echo["source"].(map[string]any)
	if source["image"] != "registry.example.test/worker:latest" {
		t.Errorf("source = %v, want {image: ...}", source)
	}
	auth, _ := echo["auth"].(map[string]any)
	if auth["kind"] != "registry" || auth["username"] != "robot" || auth["password"] != "test-password-placeholder" {
		t.Errorf("auth = %v, want {kind: registry, username, password}", auth)
	}
	for _, stray := range []string{"description", "base_image_id", "env", "lifecycle_hooks", "build_hooks"} {
		if _, present := echo[stray]; present {
			t.Errorf("create body must NOT carry %q when unset; sent %v", stray, echo)
		}
	}

	// git: source carries {repo, branch}; auth_kind git_source carries
	// {kind, git_source_id}.
	res = callTool(t, cs, "user.microvm.image.create", map[string]any{
		"name":                "from-git",
		"source_kind":         "git",
		"source_repo":         "https://git.example.test/org/repo.git",
		"source_branch":       "main",
		"hypervisor_group_id": "hg-1",
		"auth_kind":           "git_source",
		"git_source_id":       "gs-1",
	})
	out = tools.MicrovmImageResult{}
	unmarshalResult(t, res, &out)
	echo, _ = out.Image["echo"].(map[string]any)
	source, _ = echo["source"].(map[string]any)
	if source["repo"] != "https://git.example.test/org/repo.git" || source["branch"] != "main" {
		t.Errorf("source = %v, want {repo, branch}", source)
	}
	auth, _ = echo["auth"].(map[string]any)
	if auth["kind"] != "git_source" || auth["git_source_id"] != "gs-1" {
		t.Errorf("auth = %v, want {kind: git_source, git_source_id}", auth)
	}
	if _, present := auth["password"]; present {
		t.Errorf("git_source auth must not carry a password: %v", auth)
	}
}

func TestBuildMicrovmImage_SendsBuilderLocation(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.build", map[string]any{"id": "img-1", "hypervisor_group_id": "hg-1"})
	var out tools.MicrovmImageVersionResult
	unmarshalResult(t, res, &out)
	if out.Version["version"] != float64(3) {
		t.Errorf("version = %v, want 3", out.Version["version"])
	}
	echo, _ := out.Version["echo"].(map[string]any)
	if echo["hypervisor_group_id"] != "hg-1" {
		t.Errorf("build body = %v, want hypervisor_group_id hg-1", echo)
	}
}

func TestDeleteMicrovmImage_ConfirmGateAndInUse(t *testing.T) {
	cs := connectSession(t, microvmImageMock())

	res := callTool(t, cs, "user.microvm.image.delete", map[string]any{"id": "img-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.image.delete", map[string]any{"id": "img-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "img-1" {
		t.Errorf("delete result = %+v, want {img-1 true}", del)
	}

	// image_in_use surfaces as an error result (409), never as Deleted:true.
	res = callTool(t, cs, "user.microvm.image.delete", map[string]any{"id": "in-use", "confirm": true})
	if !res.IsError {
		t.Fatalf("delete of an in-use image must be an error result, got %q", resultText(t, res))
	}
}
