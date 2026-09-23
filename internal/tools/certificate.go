package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// Certificate tools, mirroring the iaas_certificate provider resource plus the
// two operations that resource deliberately does not model: Let's Encrypt
// issuance (asynchronous ACME) and a failed-issuance retry. Certificates are
// ACCOUNT-level - not children of a load balancer (the old iaas_lb_certificate
// / user.load_balancer.certificate_* shims were removed, account-certificates
// Phase 3 cleanup) - and can be attached to any of the account's load balancer
// frontends via certificate_ids (or the legacy single ssl_certificate_id).
// certificate/private_key/chain are write-only: the API never returns them in
// any response.
//
// user.certificate.replace (NUI-V-R17-ACM-ROTATE3) rotates a manually
// uploaded certificate's PEM material IN PLACE via PUT /certificates/{id}
// (NUI-V-R17-ACM-ROTATE1) - the id, and therefore every OpenTofu
// iaas_certificate resource's state and every Kubernetes-bridge reference,
// survives the rotation, unlike delete+re-upload. The shared
// terraform-provider-iaas client package does not implement this endpoint
// yet (NUI-V-R17-ACM-ROTATE2 adds a ReplaceCertificate client method built
// in parallel from the same contract), so replaceCertificate bypasses
// *client.Client and calls the endpoint directly through
// iaasauth.RawAPISource - the same stopgap internal/tools/microvm_settings.go
// pioneered (see its docblock and internal/iaasauth.RawAPISource's). Once
// ROTATE2 ships that client method and this server's go.mod picks up the
// release that carries it, rewrite replaceCertificate onto the standard
// Handler[In, Out]/Register shape like every other certificate tool and
// delete the raw transport below.

func init() {
	toolRegistrars = append(toolRegistrars, registerCertificateTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListCertificatesInput struct{}

type GetCertificateInput struct {
	ID string `json:"id" jsonschema:"UUID of the certificate"`
}

// UploadCertificateInput uploads a manual PEM certificate. name, certificate
// and private_key are required by the controller; chain is optional.
type UploadCertificateInput struct {
	Name        string `json:"name" jsonschema:"display name for the certificate"`
	Certificate string `json:"certificate" jsonschema:"PEM-encoded certificate (-----BEGIN CERTIFICATE-----...)"`
	PrivateKey  string `json:"private_key" jsonschema:"PEM-encoded private key"`
	Chain       string `json:"chain,omitempty" jsonschema:"optional PEM-encoded intermediate certificate chain"`
}

// RequestLetsEncryptCertificateInput requests ACME issuance. domains and
// via_load_balancer_id are required; name is optional. Issuance is
// asynchronous - poll with user.certificate.get.
type RequestLetsEncryptCertificateInput struct {
	Name              string   `json:"name,omitempty" jsonschema:"optional display name for the certificate"`
	Domains           []string `json:"domains" jsonschema:"domains to cover; each must already resolve to via_load_balancer_id's public IP"`
	ViaLoadBalancerID string   `json:"via_load_balancer_id" jsonschema:"UUID of the load balancer whose public IP serves the HTTP-01 challenge"`
}

type RetryCertificateInput struct {
	ID string `json:"id" jsonschema:"UUID of the certificate to retry issuance/renewal for"`
}

// RetryCertificateResult reports the retry outcome. The API's retry response
// carries only {success,message}, never the certificate object itself - poll
// user.certificate.get to observe the resulting status.
type RetryCertificateResult struct {
	ID      string `json:"id"`
	Retried bool   `json:"retried"`
}

// DeleteCertificateInput deletes an account certificate. Confirm-gated
// (destructive) - the API refuses (success:false) if the certificate is
// still in use by a load balancer frontend.
type DeleteCertificateInput struct {
	ID string `json:"id" jsonschema:"UUID of the certificate to delete"`
	Confirmation
}

// ReplaceCertificateInput rotates a manually uploaded certificate's PEM
// material in place, keeping its id. Mirrors UploadCertificateInput's schema
// (certificate + private_key required, chain optional) plus id; name is
// optional here (unlike upload) since a rotation commonly keeps the existing
// display name. Refused with a 422 when id names a Let's Encrypt-issued
// certificate (those renew themselves - use user.certificate.retry for a
// stuck renewal), when certificate/chain fails to parse, or when private_key
// does not match certificate.
type ReplaceCertificateInput struct {
	ID          string `json:"id" jsonschema:"UUID of the certificate to replace"`
	Name        string `json:"name,omitempty" jsonschema:"optional new display name for the certificate"`
	Certificate string `json:"certificate" jsonschema:"PEM-encoded certificate (-----BEGIN CERTIFICATE-----...) replacing the current one"`
	PrivateKey  string `json:"private_key" jsonschema:"PEM-encoded private key matching certificate"`
	Chain       string `json:"chain,omitempty" jsonschema:"optional PEM-encoded intermediate certificate chain"`
}

// CertificateResyncEntry reports one load balancer's outcome when the
// platform re-applies a replaced certificate to every frontend (including
// Kubernetes-bridge-managed ones) that references it. A sync failure here
// does NOT fail the replace call - the certificate material is already
// rotated; the platform's load balancer reconciler catches up independently.
type CertificateResyncEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type CertificateResult struct {
	Certificate map[string]any `json:"certificate"`
}

// ReplaceCertificateResult is user.certificate.replace's response: the
// updated certificate (same shape as user.certificate.get; still never
// certificate/private_key/chain) plus the per-load-balancer resync outcomes.
type ReplaceCertificateResult struct {
	Certificate map[string]any           `json:"certificate"`
	Resync      []CertificateResyncEntry `json:"resync"`
}

type CertificateListResult struct {
	Certificates []map[string]any `json:"certificates"`
	Count        int              `json:"count"`
}

// ── handlers ────────────────────────────────────────────────────────────────

func listCertificates(ctx context.Context, cl *client.Client, _ ListCertificatesInput) (CertificateListResult, error) {
	items, err := cl.ListCertificates(ctx)
	if err != nil {
		return CertificateListResult{}, err
	}
	return CertificateListResult{Certificates: items, Count: len(items)}, nil
}

// getCertificate returns the certificate including its usages[] (the load
// balancer frontends that reference it via ssl_certificate_id) - the SHOW
// response embeds usages directly, so no second call is needed.
func getCertificate(ctx context.Context, cl *client.Client, in GetCertificateInput) (CertificateResult, error) {
	obj, err := cl.GetCertificate(ctx, in.ID)
	if err != nil {
		return CertificateResult{}, err
	}
	return CertificateResult{Certificate: obj}, nil
}

func uploadCertificate(ctx context.Context, cl *client.Client, in UploadCertificateInput) (CertificateResult, error) {
	body := map[string]any{
		"name":        in.Name,
		"certificate": in.Certificate,
		"private_key": in.PrivateKey,
	}
	if in.Chain != "" {
		body["chain"] = in.Chain
	}
	obj, err := cl.CreateCertificate(ctx, body)
	if err != nil {
		return CertificateResult{}, err
	}
	return CertificateResult{Certificate: obj}, nil
}

func requestLetsEncryptCertificate(ctx context.Context, cl *client.Client, in RequestLetsEncryptCertificateInput) (CertificateResult, error) {
	body := map[string]any{
		"domains":              in.Domains,
		"via_load_balancer_id": in.ViaLoadBalancerID,
	}
	if in.Name != "" {
		body["name"] = in.Name
	}
	obj, err := cl.RequestLetsEncryptCertificate(ctx, body)
	if err != nil {
		return CertificateResult{}, err
	}
	return CertificateResult{Certificate: obj}, nil
}

func retryCertificate(ctx context.Context, cl *client.Client, in RetryCertificateInput) (RetryCertificateResult, error) {
	if err := cl.RetryCertificate(ctx, in.ID); err != nil {
		return RetryCertificateResult{}, err
	}
	return RetryCertificateResult{ID: in.ID, Retried: true}, nil
}

func deleteCertificate(ctx context.Context, cl *client.Client, in DeleteCertificateInput) (DeleteResult, error) {
	if err := cl.DeleteCertificate(ctx, in.ID); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{ID: in.ID, Deleted: true}, nil
}

// replaceCertificate calls PUT /certificates/{id} via the RawAPISource
// escape hatch (see the file docblock) and decodes the {certificate, resync}
// envelope.
func replaceCertificate(ctx context.Context, raw iaasauth.RawAPISource, in ReplaceCertificateInput) (ReplaceCertificateResult, error) {
	body := map[string]any{
		"certificate": in.Certificate,
		"private_key": in.PrivateKey,
	}
	if in.Chain != "" {
		body["chain"] = in.Chain
	}
	if in.Name != "" {
		body["name"] = in.Name
	}

	top, err := certificateRawRequest(ctx, raw, http.MethodPut, "/certificates/"+url.PathEscape(in.ID), body)
	if err != nil {
		return ReplaceCertificateResult{}, err
	}
	return decodeReplaceCertificateResult(top), nil
}

// decodeReplaceCertificateResult pulls "certificate" (an object) and
// "resync" (an array of {id,name,success,error}) out of the raw envelope.
// Both are best-effort: a missing or mistyped key yields the zero value
// rather than an error, so an envelope shape the tests didn't anticipate
// degrades to an empty field instead of a decode failure.
func decodeReplaceCertificateResult(top map[string]any) ReplaceCertificateResult {
	out := ReplaceCertificateResult{}
	if cert, ok := top["certificate"].(map[string]any); ok {
		out.Certificate = cert
	}
	items, ok := top["resync"].([]any)
	if !ok {
		return out
	}
	out.Resync = make([]CertificateResyncEntry, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry := CertificateResyncEntry{}
		if id, ok := m["id"].(string); ok {
			entry.ID = id
		}
		if name, ok := m["name"].(string); ok {
			entry.Name = name
		}
		if success, ok := m["success"].(bool); ok {
			entry.Success = success
		}
		if errMsg, ok := m["error"].(string); ok {
			entry.Error = errMsg
		}
		out.Resync = append(out.Resync, entry)
	}
	return out
}

func registerCertificateTools(s *mcp.Server, deps Deps) {
	raw := deps.RawAPI
	Register(s, deps, Spec{
		Name:        "user.certificate.list",
		Description: "List all account SSL/TLS certificates owned by the caller.",
	}, listCertificates)

	Register(s, deps, Spec{
		Name:        "user.certificate.get",
		Description: "Get an account certificate by UUID, including the load balancer frontends that use it (usages).",
	}, getCertificate)

	Register(s, deps, Spec{
		Name:        "user.certificate.upload",
		Description: "Upload a manual PEM certificate (certificate + private_key, optional chain) as an account-level certificate.",
	}, uploadCertificate)

	Register(s, deps, Spec{
		Name: "user.certificate.request_letsencrypt",
		Description: "Request a Let's Encrypt certificate for the given domains via a load balancer's " +
			"public IP (HTTP-01 challenge). Issuance is asynchronous; poll user.certificate.get for status.",
	}, requestLetsEncryptCertificate)

	Register(s, deps, Spec{
		Name:        "user.certificate.retry",
		Description: "Retry a failed Let's Encrypt issuance or renewal.",
	}, retryCertificate)

	Register(s, deps, Spec{
		Name: "user.certificate.delete",
		Description: "Delete an account certificate. DESTRUCTIVE: requires \"confirm\": true. Fails if the " +
			"certificate is still in use by a load balancer frontend.",
		Destructive: true,
	}, deleteCertificate)

	Register(s, deps, Spec{
		Name: "user.certificate.replace",
		Description: "Replace (rotate) a manually uploaded certificate's PEM material in place - certificate, " +
			"private_key, optional chain and optional name - keeping its id so every load balancer, OpenTofu " +
			"iaas_certificate resource and Kubernetes-bridge reference stays valid. Every load balancer that " +
			"references the certificate is re-synced afterwards; per-load-balancer outcomes come back in resync " +
			"and a sync failure there does not fail the call. Refused with a 422 when id names a Let's " +
			"Encrypt-issued certificate (those renew themselves; use user.certificate.retry for a stuck renewal), " +
			"when certificate/chain fails to parse, or when private_key does not match certificate.",
	}, func(ctx context.Context, _ *client.Client, in ReplaceCertificateInput) (ReplaceCertificateResult, error) {
		return replaceCertificate(ctx, raw, in)
	})
}

// ── raw transport (stopgap - see the file docblock) ─────────────────────────

// certificateRawRequest issues one request against a /certificates* path
// using the endpoint/token/timeout/TLS settings RawAPISource returns, and
// decodes the JSON envelope. A non-2xx response becomes a *client.APIError
// (built here with the exported fields client.APIError already has) so it
// flows through the framework's existing MapError unchanged - mirrors
// internal/tools/microvm_settings.go's microvmSettingsRequest, generalised
// to a caller-supplied path.
func certificateRawRequest(ctx context.Context, raw iaasauth.RawAPISource, method, path string, body any) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("certificate replace transport not configured")
	}

	endpoint, token, timeout, insecure, err := raw(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication required: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		encoded, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshalling request body: %w", marshalErr)
		}
		reqBody = bytes.NewReader(encoded)
	}

	fullURL := strings.TrimRight(endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("building request %s %s: %w", method, fullURL, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpClient := &http.Client{Timeout: timeout}
	if insecure {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional; caller opts in
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request %s %s: %w", method, fullURL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if apiErr := certificateResponseError(resp, data); apiErr != nil {
		return nil, apiErr
	}

	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return top, nil
}

// certificateResponseError mirrors client's private responseError closely
// enough for MapError to behave identically: 2xx -> nil, 422 -> FieldErrors
// from {"errors":{...}} with a top-level "code" (when the API sends one,
// e.g. certificate_replace_not_manual) folded into the message so an agent
// can act on it, 401/403 -> the same IP-lock hint text, others -> the body's
// "message"/"error" or a status default.
func certificateResponseError(resp *http.Response, body []byte) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	apiErr := &client.APIError{
		Status:    resp.StatusCode,
		RequestID: resp.Header.Get("X-Request-Id"),
	}

	var parsed struct {
		Message string                     `json:"message"`
		Error   string                     `json:"error"`
		Code    string                     `json:"code"`
		Errors  map[string]json.RawMessage `json:"errors"`
	}
	_ = json.Unmarshal(body, &parsed)

	msg := parsed.Message
	if msg == "" {
		msg = parsed.Error
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
	}
	if parsed.Code != "" {
		msg = fmt.Sprintf("[%s] %s", parsed.Code, msg)
	}

	switch resp.StatusCode {
	case http.StatusUnprocessableEntity:
		apiErr.Message = msg
		if len(parsed.Errors) > 0 {
			fieldErrs := make(map[string][]string, len(parsed.Errors))
			keys := make([]string, 0, len(parsed.Errors))
			for field := range parsed.Errors {
				keys = append(keys, field)
			}
			sort.Strings(keys)
			for _, field := range keys {
				var msgs []string
				if err := json.Unmarshal(parsed.Errors[field], &msgs); err == nil {
					fieldErrs[field] = msgs
				}
			}
			apiErr.FieldErrors = fieldErrs
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		apiErr.Message = msg + "; check that your token is registered from this IP address (tokens are IP-locked), " +
			"the token is enabled and not expired, and has the required scope / subuser permission"
	default:
		apiErr.Message = msg
	}

	return apiErr
}
