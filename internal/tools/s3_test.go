package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func s3Mock() http.Handler {
	mux := http.NewServeMux()
	// Bucket create returns no id/body.
	mux.HandleFunc("POST /object-storage/buckets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "created"})
	})
	mux.HandleFunc("GET /object-storage/buckets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "bkt-1", "name": "assets"}},
		})
	})
	mux.HandleFunc("GET /object-storage/bucket/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":    true,
			"bucket":     map[string]any{"id": r.PathValue("id"), "name": "assets"},
			"access_key": "ak_public",
			"secret_key": "sk_secret",
			"endpoint":   "https://s3.example.com",
		})
	})
	mux.HandleFunc("PATCH /object-storage/bucket/{id}/acl/{action}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "acl set"})
	})
	mux.HandleFunc("DELETE /object-storage/bucket/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "deleted"})
	})
	mux.HandleFunc("GET /object-storage/bucket/{id}/keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "key-1", "pivot": map[string]any{"permission": "readwrite"}}},
		})
	})
	mux.HandleFunc("POST /object-storage/bucket/{bid}/attach/{kid}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "attached"})
	})

	// Access keys.
	mux.HandleFunc("POST /object-storage/access-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true, "data": map[string]any{"access_key": "ak_public", "secret_key": "sk_shown_once"},
		})
	})
	mux.HandleFunc("GET /object-storage/access-keys", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"current_page": 1, "last_page": 1,
			"data": []any{map[string]any{"id": "akr-1", "name": "ci", "access_key": "ak_public", "active": true}},
		})
	})
	mux.HandleFunc("PATCH /object-storage/access-key/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "updated"})
	})
	mux.HandleFunc("DELETE /object-storage/access-key/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "akr-down" {
			writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "reason": "storage_unreachable", "message": "A storage server is not reachable right now, so the key was not deleted. Try again."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Access key deleted successfully!"})
	})
	mux.HandleFunc("POST /object-storage/bucket/{id}/object/public-url", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "url": "https://s3.example.com/b/" + body["object_key"].(string) + "?X-Amz-Signature=x", "expires_in": body["expires_in"], "expires_at": "2026-11-05T15:00:00+00:00"})
	})
	return mux
}

func TestS3Bucket_Lifecycle(t *testing.T) {
	cs := connectSession(t, s3Mock())

	res := callTool(t, cs, "user.s3_bucket.create", map[string]any{
		"name": "assets", "s3_plan_id": "sp-1", "s3_server_id": "srv-1",
	})
	var created tools.S3BucketResult
	unmarshalResult(t, res, &created)
	if created.Bucket["id"] != "bkt-1" {
		t.Fatalf("create readback id = %v, want bkt-1", created.Bucket["id"])
	}

	res = callTool(t, cs, "user.s3_bucket.get", map[string]any{"id": "bkt-1"})
	var got tools.S3BucketResult
	unmarshalResult(t, res, &got)
	if got.AccessKey != "ak_public" || got.Endpoint == "" {
		t.Errorf("get creds = %q / endpoint %q, want populated", got.AccessKey, got.Endpoint)
	}

	res = callTool(t, cs, "user.s3_bucket.attach_key", map[string]any{"bucket_id": "bkt-1", "key_id": "key-1", "permission": "readwrite"})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("attach_key ok = false")
	}

	// attach without permission -> handler error.
	res = callTool(t, cs, "user.s3_bucket.attach_key", map[string]any{"bucket_id": "bkt-1", "key_id": "key-1"})
	if !res.IsError {
		t.Errorf("attach_key without permission should error")
	}

	res = callTool(t, cs, "user.s3_bucket.delete", map[string]any{"id": "bkt-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}
	res = callTool(t, cs, "user.s3_bucket.delete", map[string]any{"id": "bkt-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}

func TestS3AccessKey_CreateReturnsSecretOnce(t *testing.T) {
	cs := connectSession(t, s3Mock())

	res := callTool(t, cs, "user.s3_access_key.create", map[string]any{"name": "ci"})
	var created tools.S3AccessKeyResult
	unmarshalResult(t, res, &created)
	if created.Secret != "sk_shown_once" {
		t.Fatalf("create secret = %q, want sk_shown_once", created.Secret)
	}
	if created.AccessKey["id"] != "akr-1" {
		t.Errorf("create resolved id = %v, want akr-1", created.AccessKey["id"])
	}

	res = callTool(t, cs, "user.s3_access_key.list", map[string]any{})
	var list tools.S3AccessKeyListResult
	unmarshalResult(t, res, &list)
	if list.Count != 1 {
		t.Errorf("list count = %d, want 1", list.Count)
	}

	res = callTool(t, cs, "user.s3_access_key.update", map[string]any{"id": "akr-1", "active": false})
	var ok tools.OKResult
	unmarshalResult(t, res, &ok)
	if !ok.OK {
		t.Errorf("update ok = false")
	}
}

func TestS3AccessKey_DeleteNeedsConfirmAndSurfacesAKeptKey(t *testing.T) {
	cs := connectSession(t, s3Mock())

	res := callTool(t, cs, "user.s3_access_key.delete", map[string]any{"id": "akr-1"})
	if !res.IsError {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.s3_access_key.delete", map[string]any{"id": "akr-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted || del.ID != "akr-1" {
		t.Errorf("confirmed delete result = %+v", del)
	}

	// A storage server that could not be reached keeps the key: the tool must fail, not report Deleted.
	res = callTool(t, cs, "user.s3_access_key.delete", map[string]any{"id": "akr-down", "confirm": true})
	if !res.IsError {
		t.Fatalf("a 502 must surface as a tool error; got %q", resultText(t, res))
	}
}

func TestS3ObjectPublicURL_PassesExpiryAndReturnsIt(t *testing.T) {
	cs := connectSession(t, s3Mock())

	res := callTool(t, cs, "user.s3_bucket.object_public_url", map[string]any{"bucket_id": "bkt-1", "object_key": "a.txt", "expires_in": 600})
	var out tools.URLResult
	unmarshalResult(t, res, &out)
	if out.ExpiresIn != 600 || out.ExpiresAt == "" || !strings.Contains(out.URL, "X-Amz-Signature") {
		t.Errorf("result = %+v", out)
	}
}
