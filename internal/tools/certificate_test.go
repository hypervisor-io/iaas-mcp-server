package tools_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/hypervisor-io/iaas-mcp-server/internal/tools"
)

func certificateMock() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /certificates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"certificates": []any{
				map[string]any{"id": "cert-1", "name": "example", "domain": "example.test", "status": "active"},
				map[string]any{"id": "cert-2", "name": "le", "domain": "le.test", "status": "pending", "type": "letsencrypt"},
			},
		})
	})

	mux.HandleFunc("GET /certificate/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "missing" {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Certificate not found."})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"certificate": map[string]any{
				"id":                 id,
				"name":               "example",
				"domain":             "example.test",
				"san_domains":        []any{"www.example.test"},
				"type":               "manual",
				"status":             "active",
				"fingerprint_sha256": "AA:BB:CC",
				"usage_count":        1,
				"usages": []any{
					map[string]any{"load_balancer_id": "lb-1", "load_balancer_name": "web-lb", "port": 443, "is_default": true},
				},
			},
		})
	})

	mux.HandleFunc("POST /certificates", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "Certificate uploaded successfully.",
			"certificate": map[string]any{
				"id":     "cert-new",
				"name":   body["name"],
				"domain": "uploaded.test",
				"status": "active",
			},
		})
	})

	mux.HandleFunc("POST /certificates/letsencrypt", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"certificate": map[string]any{
				"id":                   "cert-le-1",
				"name":                 body["name"],
				"domain":               "le.test",
				"type":                 "letsencrypt",
				"status":               "pending",
				"letsencrypt_status":   "pending",
				"via_load_balancer_id": body["via_load_balancer_id"],
			},
		})
	})

	mux.HandleFunc("POST /certificate/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Retry queued."})
	})

	mux.HandleFunc("DELETE /certificate/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "in-use" {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"message": "This certificate is in use by 1 load balancer frontend and cannot be deleted.",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Certificate deleted."})
	})

	return mux
}

func TestCertificate_ListGet(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.list", map[string]any{})
	var list tools.CertificateListResult
	unmarshalResult(t, res, &list)
	if list.Count != 2 {
		t.Errorf("list count = %d, want 2", list.Count)
	}

	res = callTool(t, cs, "user.certificate.get", map[string]any{"id": "cert-1"})
	var got tools.CertificateResult
	unmarshalResult(t, res, &got)
	if got.Certificate["id"] != "cert-1" {
		t.Errorf("get id = %v, want cert-1", got.Certificate["id"])
	}
	usages, ok := got.Certificate["usages"].([]any)
	if !ok || len(usages) != 1 {
		t.Errorf("get usages = %v, want a 1-element array", got.Certificate["usages"])
	}
	for _, stray := range []string{"certificate", "private_key", "chain"} {
		if _, present := got.Certificate[stray]; present {
			t.Errorf("get result must NOT include %q; got %v", stray, got.Certificate)
		}
	}

	res = callTool(t, cs, "user.certificate.get", map[string]any{"id": "missing"})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("get unknown: want not found, got %q", resultText(t, res))
	}
}

func TestCertificate_Upload(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.upload", map[string]any{
		"name":        "example",
		"certificate": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
	})
	var out tools.CertificateResult
	unmarshalResult(t, res, &out)
	if out.Certificate["id"] != "cert-new" {
		t.Errorf("upload id = %v, want cert-new", out.Certificate["id"])
	}
	if out.Certificate["name"] != "example" {
		t.Errorf("upload name = %v, want example", out.Certificate["name"])
	}
}

func TestCertificate_RequestLetsEncrypt(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.request_letsencrypt", map[string]any{
		"domains":              []string{"le.test", "www.le.test"},
		"via_load_balancer_id": "lb-1",
	})
	var out tools.CertificateResult
	unmarshalResult(t, res, &out)
	if out.Certificate["id"] != "cert-le-1" {
		t.Errorf("request_letsencrypt id = %v, want cert-le-1", out.Certificate["id"])
	}
	if out.Certificate["status"] != "pending" {
		t.Errorf("request_letsencrypt status = %v, want pending", out.Certificate["status"])
	}
}

func TestCertificate_Retry(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.retry", map[string]any{"id": "cert-le-1"})
	var out tools.RetryCertificateResult
	unmarshalResult(t, res, &out)
	if !out.Retried || out.ID != "cert-le-1" {
		t.Errorf("retry result = %+v, want {cert-le-1 true}", out)
	}
}

func TestCertificate_DeleteConfirmGate(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.delete", map[string]any{"id": "cert-1"})
	if !res.IsError || !strings.Contains(resultText(t, res), "confirm") {
		t.Fatalf("delete without confirm should refuse; got %q", resultText(t, res))
	}

	res = callTool(t, cs, "user.certificate.delete", map[string]any{"id": "cert-1", "confirm": true})
	var del tools.DeleteResult
	unmarshalResult(t, res, &del)
	if !del.Deleted {
		t.Errorf("confirmed delete did not succeed")
	}
}

func TestCertificate_DeleteInUseFailure(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.delete", map[string]any{"id": "in-use", "confirm": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "in use") {
		t.Errorf("delete in-use: want an 'in use' error, got %q", resultText(t, res))
	}
}
