package tools_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// 3.3.8.8: the panel checks an instance hostname (RFC 1123) and the cloud-init user data when they are submitted
// (422 with reason invalid_hostname or invalid_cloud_config and errors.hostname or errors.cloudcfg), and refuses an
// invalid SSH key with 422 errors.public_key. The tools show the field message and what to do next.

const hostSentence = "The hostname may contain only letters, digits and hyphens, with dots between parts."

func inputPanel(t *testing.T, phase1, deploy func(w http.ResponseWriter), deploys, creates *atomic.Int32) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cloud-service/instances", func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		phase1(w)
	})
	mux.HandleFunc("POST /instance/{id}/deploy", func(w http.ResponseWriter, r *http.Request) {
		deploys.Add(1)
		deploy(w)
	})
	return mux
}

func TestInstanceCreate_InvalidHostnameIsAClearValidationError(t *testing.T) {
	var creates, deploys atomic.Int32
	h := inputPanel(t, func(w http.ResponseWriter) {
		writeJSON(w, 422, map[string]any{"success": false, "reason": "invalid_hostname", "message": hostSentence,
			"errors": map[string]any{"hostname": []string{hostSentence}}})
	}, func(w http.ResponseWriter) {}, &deploys, &creates)
	cs := connectSession(t, h)
	res := callTool(t, cs, "user.instance.create", map[string]any{"location_id": "l", "plan_id": "p", "image_id": "i", "hostname": "web_1"})
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	msg := resultText(t, res)
	for _, want := range []string{"invalid_hostname", hostSentence, "hostname: ["} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if deploys.Load() != 0 {
		t.Errorf("no deploy after a refused create, got %d", deploys.Load())
	}
}

func TestInstanceCreate_InvalidCloudConfigNamesTheRecordAndTheNextStep(t *testing.T) {
	var creates, deploys atomic.Int32
	key := `The cloud-init key "runcmd" must be a list (one "- " line per entry), not a single value.`
	h := inputPanel(t, func(w http.ResponseWriter) {
		writeJSON(w, 200, map[string]any{"success": true, "instance": map[string]any{"id": "inst-9"}})
	}, func(w http.ResponseWriter) {
		writeJSON(w, 422, map[string]any{"success": false, "reason": "invalid_cloud_config", "message": key,
			"errors": map[string]any{"cloudcfg": []string{key}}})
	}, &deploys, &creates)
	cs := connectSession(t, h)
	res := callTool(t, cs, "user.instance.create", map[string]any{"location_id": "l", "plan_id": "p", "image_id": "i", "cloudcfg": "#cloud-config\nruncmd: echo hi"})
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	msg := resultText(t, res)
	for _, want := range []string{"inst-9", "recorded but not deployed", "user.instance.deploy", "invalid_cloud_config", `"runcmd" must be a list`, "not a script"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestSSHKeyCreate_InvalidKeyShowsTheFieldMessage(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("POST /ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 422, map[string]any{"message": "The public key is not a valid SSH public key.", "errors": map[string]any{"public_key": []string{"The public key is not a valid SSH public key."}}})
	})
	cs := connectSession(t, h)
	res := callTool(t, cs, "user.ssh_key.create", map[string]any{"name": "k", "public_key": "nope"})
	msg := resultText(t, res)
	if !res.IsError || !strings.Contains(msg, "validation failed") || !strings.Contains(msg, "public_key") || !strings.Contains(msg, "not a valid SSH public key") {
		t.Fatalf("got error=%v %q", res.IsError, msg)
	}
}
