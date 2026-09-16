package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// MicroVM settings tools (contract C4/C8.6): the account's default microVM
// network, used by user.microvm.create and every E2B sandbox create call
// when the request omits network/metadata.location_id.
// `GET/PUT /microvm/settings`.
//
// The shared terraform-provider-iaas client package does not implement this
// endpoint yet (it lands with MV7-13, the OpenTofu provider's
// iaas_microvm_settings resource, built in parallel from the same
// contract - MV7-14 does not wait for it). So these two handlers bypass
// *client.Client and call the endpoint directly through
// iaasauth.RawAPISource, replicating just enough of client.Client's
// request/response and *client.APIError shape (via deps.RawAPI, wired the
// same way TokenSource is) to get identical error mapping through the
// shared MapError. Once MV7-13 ships a GetMicrovmSettings/
// UpdateMicrovmSettings client method, rewrite these onto the standard
// Handler[In, Out] shape like every other tool family and delete this raw
// transport.
const microvmSettingsPath = "/microvm/settings"

func init() {
	toolRegistrars = append(toolRegistrars, registerMicrovmSettingsTools)
}

// ── inputs / outputs ────────────────────────────────────────────────────────

type GetMicrovmSettingsInput struct{}

// MicrovmDefaultNetworkInput is the account-level default network entry
// (C4). Unlike a create-time network[] entry it never carries
// security_group_ids/rate_mbit, and static_ip_id can never be stored as a
// default (C8.6: a static IP is scoped to one location, a default is
// location-independent) - there is deliberately no field for it here.
type MicrovmDefaultNetworkInput struct {
	Kind        string `json:"kind" jsonschema:"public or vpc"`
	SubnetID    string `json:"subnet_id,omitempty" jsonschema:"optional even for kind public (C8.6) - the platform auto-assigns a public subnet when omitted"`
	VpcSubnetID string `json:"vpc_subnet_id,omitempty" jsonschema:"required when kind is vpc; must be a VPC subnet owned by the account"`
}

// UpdateMicrovmSettingsInput sets or clears the stored default. Send
// default_network to set it, or clear:true (with default_network omitted)
// to clear it.
type UpdateMicrovmSettingsInput struct {
	DefaultNetwork *MicrovmDefaultNetworkInput `json:"default_network,omitempty" jsonschema:"the network entry to store as the account's default"`
	Clear          bool                        `json:"clear,omitempty" jsonschema:"set true to clear the stored default (sends default_network: null); default_network is ignored when true"`
}

// MicrovmSettingsResult mirrors the API's {default_network: {...} | null}
// envelope. DefaultNetwork is a *map so the tool's generated output schema
// allows an explicit JSON null (jsonschema-go only widens a field's type to
// include "null" for a pointer) - it is nil when no default is set.
type MicrovmSettingsResult struct {
	DefaultNetwork *map[string]any `json:"default_network"`
}

// defaultNetworkResult builds the result from the API envelope's
// "default_network" key: an object becomes a non-nil pointer to it,
// anything else (JSON null, or the key absent) becomes a nil pointer.
func defaultNetworkResult(top map[string]any) MicrovmSettingsResult {
	if dn, ok := top["default_network"].(map[string]any); ok {
		return MicrovmSettingsResult{DefaultNetwork: &dn}
	}
	return MicrovmSettingsResult{}
}

// ── handlers ────────────────────────────────────────────────────────────────

func getMicrovmSettings(ctx context.Context, raw iaasauth.RawAPISource, _ GetMicrovmSettingsInput) (MicrovmSettingsResult, error) {
	top, err := microvmSettingsRequest(ctx, raw, http.MethodGet, nil)
	if err != nil {
		return MicrovmSettingsResult{}, err
	}
	return defaultNetworkResult(top), nil
}

func updateMicrovmSettings(ctx context.Context, raw iaasauth.RawAPISource, in UpdateMicrovmSettingsInput) (MicrovmSettingsResult, error) {
	var body map[string]any
	if in.Clear || in.DefaultNetwork == nil {
		body = map[string]any{"default_network": nil}
	} else {
		entry := map[string]any{"kind": in.DefaultNetwork.Kind}
		if in.DefaultNetwork.SubnetID != "" {
			entry["subnet_id"] = in.DefaultNetwork.SubnetID
		}
		if in.DefaultNetwork.VpcSubnetID != "" {
			entry["vpc_subnet_id"] = in.DefaultNetwork.VpcSubnetID
		}
		body = map[string]any{"default_network": entry}
	}

	top, err := microvmSettingsRequest(ctx, raw, http.MethodPut, body)
	if err != nil {
		return MicrovmSettingsResult{}, err
	}
	return defaultNetworkResult(top), nil
}

func registerMicrovmSettingsTools(s *mcp.Server, deps Deps) {
	raw := deps.RawAPI

	Register(s, deps, Spec{
		Name:        "user.microvm.settings.get",
		Description: "Get the account's stored default microVM network (contract C4), used by user.microvm.create and E2B sandbox create when the request omits network. Returns default_network: null when none is set.",
	}, func(ctx context.Context, _ *client.Client, in GetMicrovmSettingsInput) (MicrovmSettingsResult, error) {
		return getMicrovmSettings(ctx, raw, in)
	})

	Register(s, deps, Spec{
		Name:        "user.microvm.settings.set",
		Description: "Set or clear the account's default microVM network. {kind: public} needs no subnet_id (C8.6 - a public IPv4 is assigned automatically); a vpc default must name vpc_subnet_id owned by the account. static_ip_id can never be stored as a default. Pass clear:true to remove the stored default.",
	}, func(ctx context.Context, _ *client.Client, in UpdateMicrovmSettingsInput) (MicrovmSettingsResult, error) {
		return updateMicrovmSettings(ctx, raw, in)
	})
}

// ── raw transport (stopgap - see the file docblock) ─────────────────────────

// microvmSettingsRequest issues one GET or PUT against /microvm/settings
// using the endpoint/token/timeout/TLS settings RawAPISource returns, and
// decodes the JSON envelope. A non-2xx response becomes a *client.APIError
// (constructed here with the exported fields client.APIError already has)
// so it flows through the framework's existing MapError unchanged.
func microvmSettingsRequest(ctx context.Context, raw iaasauth.RawAPISource, method string, body any) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("microvm settings transport not configured")
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

	url := strings.TrimRight(endpoint, "/") + microvmSettingsPath
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("building request %s %s: %w", method, url, err)
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
		return nil, fmt.Errorf("executing request %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if apiErr := microvmSettingsResponseError(resp, data); apiErr != nil {
		return nil, apiErr
	}

	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return top, nil
}

// microvmSettingsResponseError mirrors client's private responseError
// closely enough for MapError to behave identically for this one endpoint:
// 2xx -> nil, 422 -> FieldErrors from {"errors": {...}}, 401/403 -> the same
// IP-lock hint text, others -> the body's "message" or a status default.
func microvmSettingsResponseError(resp *http.Response, body []byte) error {
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
