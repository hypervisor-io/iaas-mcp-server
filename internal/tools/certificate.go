package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

type CertificateResult struct {
	Certificate map[string]any `json:"certificate"`
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

func registerCertificateTools(s *mcp.Server, deps Deps) {
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
}
