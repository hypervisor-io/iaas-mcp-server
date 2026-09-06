package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

// microvmAppMock mocks the MV2-25 Serverless App endpoints. CREATE echoes the
// received request body back under the returned app's "echo" key so the tests
// can pin exactly how the tool mapped source_kind onto the source object;
// DEPLOY/ROLLBACK refuse (success:false) unless the body carries the expected
// revision_id, and SET ENV refuses unless the body carries an env map, so a
// dropped field fails the tool call instead of passing silently.
func microvmAppMock() http.Handler {
	mux := http.NewServeMux()

	// The merged MV2-25 index nests a Laravel paginator under "apps"
	// ({success,apps:{data:[...]}}), which is what client.ListApps unwraps.
	mux.HandleFunc("GET /microvm/apps", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"apps": map[string]any{
				"data": []any{
					map[string]any{"id": "app-1", "slug": "demo", "state": "running"},
					map[string]any{"id": "app-2", "slug": "blog", "state": "paused"},
				},
			},
		})
	})

	mux.HandleFunc("GET /microvm/app/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "App not found."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":   true,
			"app":       map[string]any{"id": id, "slug": "demo", "state": "running"},
			"revisions": []any{map[string]any{"id": "rev-1", "state": "built"}},
		})
	})

	mux.HandleFunc("POST /microvm/apps", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"app":     map[string]any{"id": "app-new", "slug": body["slug"], "state": "building", "echo": body},
		})
	})

	deployOrRollback := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["revision_id"] != "rev-1" {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"message": "revision_id is required and must name a built revision.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Revision accepted."})
	}
	mux.HandleFunc("POST /microvm/app/{id}/deploy", deployOrRollback)
	mux.HandleFunc("POST /microvm/app/{id}/rollback", deployOrRollback)

	mux.HandleFunc("POST /microvm/app/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "App stopped."})
	})

	mux.HandleFunc("POST /microvm/app/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "App started."})
	})

	mux.HandleFunc("PUT /microvm/app/{id}/env", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["env"].(map[string]any); !ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"message": "A full env map is required.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Environment synced."})
	})

	mux.HandleFunc("DELETE /microvm/app/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "App deleted."})
	})

	return mux
}

func TestListMicrovmApps_ReturnsCountAndItems(t *testing.T) {
	cs := connectSession(t, microvmAppMock())

	res := callTool(t, cs, "user.microvm.app.list", map[string]any{})
	var list tools.MicrovmAppListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Fatalf("expected 2 apps, got %d", list.Count)
	}
	if list.Apps[0]["id"] != "app-1" {
		t.Errorf("apps[0].id = %v, want app-1", list.Apps[0]["id"])
	}
}

func TestGetMicrovmApp_ReturnsApp(t *testing.T) {
	cs := connectSession(t, microvmAppMock())

	res := callTool(t, cs, "user.microvm.app.get", map[string]any{"id": "app-1"})
	var got tools.MicrovmAppResult
	unmarshalResult(t, res, &got)
	if got.App["id"] != "app-1" {
		t.Errorf("get id = %v, want app-1", got.App["id"])
	}

	res = callTool(t, cs, "user.microvm.app.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}
}

func TestCreateMicrovmApp_CallsClientCreateApp(t *testing.T) {
	cs := connectSession(t, microvmAppMock())

	res := callTool(t, cs, "user.microvm.app.create", map[string]any{
		"hypervisor_group_id": "g1",
		"slug":                "demo",
		"source_kind":         "oci",
		"source_image":        "ghcr.io/acme/demo:1",
	})
	var result tools.MicrovmAppResult
	unmarshalResult(t, res, &result)
	if result.App["slug"] != "demo" {
		t.Fatalf("unexpected result: %v", result.App)
	}

	// An oci source must map onto source.image, and only source.image.
	echo, ok := result.App["echo"].(map[string]any)
	if !ok {
		t.Fatalf("create result missing the echoed request body: %v", result.App)
	}
	if echo["hypervisor_group_id"] != "g1" || echo["source_kind"] != "oci" {
		t.Errorf("create body = %v, want it to carry the required fields", echo)
	}
	source, _ := echo["source"].(map[string]any)
	if source["image"] != "ghcr.io/acme/demo:1" {
		t.Errorf("source = %v, want image=ghcr.io/acme/demo:1", echo["source"])
	}
	for _, stray := range []string{"repo", "branch"} {
		if _, present := source[stray]; present {
			t.Errorf("oci create body must NOT carry source.%s; sent %v", stray, echo["source"])
		}
	}
}

func TestCreateMicrovmApp_GitSourceMapsRepoAndBranch(t *testing.T) {
	cs := connectSession(t, microvmAppMock())

	res := callTool(t, cs, "user.microvm.app.create", map[string]any{
		"hypervisor_group_id": "g1",
		"slug":                "demo",
		"source_kind":         "git",
		"source_repo":         "https://github.com/acme/demo",
		"source_branch":       "release",
	})
	var result tools.MicrovmAppResult
	unmarshalResult(t, res, &result)
	echo, ok := result.App["echo"].(map[string]any)
	if !ok {
		t.Fatalf("create result missing the echoed request body: %v", result.App)
	}
	source, _ := echo["source"].(map[string]any)
	if source["repo"] != "https://github.com/acme/demo" || source["branch"] != "release" {
		t.Errorf("source = %v, want repo=https://github.com/acme/demo branch=release", echo["source"])
	}
	if _, present := source["image"]; present {
		t.Errorf("git create body must NOT carry source.image; sent %v", echo["source"])
	}
}

func TestMicrovmAppLifecycle_DeployRollbackStopStartSetEnv(t *testing.T) {
	cs := connectSession(t, microvmAppMock())

	// The mock refuses deploy/rollback unless the request body carries
	// revision_id rev-1, so a success here pins that the tool passed the
	// revision id through in the body.
	res := callTool(t, cs, "user.microvm.app.deploy", map[string]any{"id": "app-1", "revision_id": "rev-1"})
	var out tools.MicrovmAppActionResult
	unmarshalResult(t, res, &out)
	if !out.Done || out.ID != "app-1" {
		t.Errorf("deploy result = %+v, want {app-1 true}", out)
	}

	res = callTool(t, cs, "user.microvm.app.rollback", map[string]any{"id": "app-1", "revision_id": "rev-1"})
	out = tools.MicrovmAppActionResult{}
	unmarshalResult(t, res, &out)
	if !out.Done || out.ID != "app-1" {
		t.Errorf("rollback result = %+v, want {app-1 true}", out)
	}

	res = callTool(t, cs, "user.microvm.app.stop", map[string]any{"id": "app-1"})
	out = tools.MicrovmAppActionResult{}
	unmarshalResult(t, res, &out)
	if !out.Done || out.ID != "app-1" {
		t.Errorf("stop result = %+v, want {app-1 true}", out)
	}

	res = callTool(t, cs, "user.microvm.app.start", map[string]any{"id": "app-1"})
	out = tools.MicrovmAppActionResult{}
	unmarshalResult(t, res, &out)
	if !out.Done || out.ID != "app-1" {
		t.Errorf("start result = %+v, want {app-1 true}", out)
	}

	res = callTool(t, cs, "user.microvm.app.set_env", map[string]any{"id": "app-1", "env": map[string]any{"FOO": "bar"}})
	out = tools.MicrovmAppActionResult{}
	unmarshalResult(t, res, &out)
	if !out.Done || out.ID != "app-1" {
		t.Errorf("set_env result = %+v, want {app-1 true}", out)
	}
}

func TestDeleteMicrovmApp_RequiresConfirmation(t *testing.T) {
	cs := connectSession(t, microvmAppMock())

	// Without confirm the framework refuses before any HTTP call; with
	// confirm:true the delete goes through (the same two-part proof
	// TestKillSandbox_RequiresConfirmation uses).
	res := callTool(t, cs, "user.microvm.app.delete", map[string]any{"id": "app-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.microvm.app.delete", map[string]any{"id": "app-1", "confirm": true})
	var out tools.MicrovmAppActionResult
	unmarshalResult(t, res, &out)
	if !out.Done || out.ID != "app-1" {
		t.Errorf("delete result = %+v, want {app-1 true}", out)
	}
}
