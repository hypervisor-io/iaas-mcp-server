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

	// PUT /certificates/{id} - contract per NUI-V-R17-ACM-ROTATE1: rotates a
	// manual certificate's PEM material in place and re-syncs every load
	// balancer that references it, reporting outcomes in resync[]. 422s carry
	// a "code" alongside "message" for the not-manual case; the parse-failure
	// case is modeled message-only to prove certificateResponseError degrades
	// cleanly when no code is present.
	mux.HandleFunc("PUT /certificates/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		switch id {
		case "missing":
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Certificate not found."})
		case "cert-le-1":
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"code":    "certificate_replace_not_manual",
				"message": "This certificate was issued via Let's Encrypt and cannot be manually replaced.",
			})
		case "cert-badpem":
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "The certificate could not be parsed.",
				"errors":  map[string]any{"certificate": []string{"The certificate could not be parsed."}},
			})
		case "cert-keymismatch":
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"code":    "certificate_key_mismatch",
				"message": "The private key does not match the certificate.",
			})
		default:
			name := body["name"]
			if name == nil || name == "" {
				name = "example"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"success": true,
				"certificate": map[string]any{
					"id":                 id,
					"name":               name,
					"domain":             "example.test",
					"type":               "manual",
					"status":             "active",
					"fingerprint_sha256": "DD:EE:FF",
				},
				"resync": []any{
					map[string]any{"id": "lb-1", "name": "web-lb", "success": true},
					map[string]any{"id": "lb-2", "name": "cluster-lb", "success": false, "error": "sync failed: node unreachable"},
				},
			})
		}
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

func TestCertificate_Replace(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.replace", map[string]any{
		"id":          "cert-1",
		"name":        "rotated",
		"certificate": "-----BEGIN CERTIFICATE-----\nMIIB...NEW\n-----END CERTIFICATE-----",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIE...NEW\n-----END PRIVATE KEY-----",
	})
	if res.IsError {
		t.Fatalf("replace failed: %s", resultText(t, res))
	}
	var out tools.ReplaceCertificateResult
	unmarshalResult(t, res, &out)

	if out.Certificate["id"] != "cert-1" {
		t.Errorf("replace kept id = %v, want cert-1 (rotation must not mint a new id)", out.Certificate["id"])
	}
	if out.Certificate["name"] != "rotated" {
		t.Errorf("replace name = %v, want rotated", out.Certificate["name"])
	}
	if out.Certificate["fingerprint_sha256"] != "DD:EE:FF" {
		t.Errorf("replace fingerprint_sha256 = %v, want the rotated value DD:EE:FF", out.Certificate["fingerprint_sha256"])
	}
	for _, stray := range []string{"certificate", "private_key", "chain"} {
		if _, present := out.Certificate[stray]; present {
			t.Errorf("replace result must NOT include %q; got %v", stray, out.Certificate)
		}
	}

	if len(out.Resync) != 2 {
		t.Fatalf("resync count = %d, want 2", len(out.Resync))
	}
	byID := map[string]tools.CertificateResyncEntry{}
	for _, e := range out.Resync {
		byID[e.ID] = e
	}
	ok, present := byID["lb-1"]
	if !present || !ok.Success || ok.Name != "web-lb" {
		t.Errorf("resync[lb-1] = %+v, want a successful entry named web-lb", ok)
	}
	failed, present := byID["lb-2"]
	if !present || failed.Success || failed.Error == "" {
		t.Errorf("resync[lb-2] = %+v, want a failed entry carrying error", failed)
	}
}

func TestCertificate_ReplaceNotManualIsValidationError(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.replace", map[string]any{
		"id":          "cert-le-1",
		"certificate": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
	})
	if !res.IsError {
		t.Fatalf("replace on a Let's Encrypt certificate should fail")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "validation failed") {
		t.Errorf("replace not-manual: want a validation failed error, got %q", text)
	}
	if !strings.Contains(text, "certificate_replace_not_manual") {
		t.Errorf("replace not-manual: want the certificate_replace_not_manual code surfaced, got %q", text)
	}
}

func TestCertificate_ReplaceParseFailureIsValidationError(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.replace", map[string]any{
		"id":          "cert-badpem",
		"certificate": "not a pem",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
	})
	if !res.IsError {
		t.Fatalf("replace with an unparseable certificate should fail")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "validation failed") || !strings.Contains(text, "parsed") {
		t.Errorf("replace parse failure: want a validation failed error mentioning parsing, got %q", text)
	}
}

func TestCertificate_ReplaceKeyMismatchIsValidationError(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.replace", map[string]any{
		"id":          "cert-keymismatch",
		"certificate": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIE...WRONG\n-----END PRIVATE KEY-----",
	})
	if !res.IsError {
		t.Fatalf("replace with a mismatched key should fail")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "certificate_key_mismatch") || !strings.Contains(text, "does not match") {
		t.Errorf("replace key mismatch: want the certificate_key_mismatch code and message, got %q", text)
	}
}

func TestCertificate_ReplaceNotFoundCrossTenant(t *testing.T) {
	cs := connectSession(t, certificateMock())

	res := callTool(t, cs, "user.certificate.replace", map[string]any{
		"id":          "missing",
		"certificate": "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
	})
	if !res.IsError || !strings.Contains(resultText(t, res), "not found") {
		t.Errorf("replace unknown/cross-tenant id: want not found, got %q", resultText(t, res))
	}
}
