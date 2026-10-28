package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/hypervisor-io/terraform-provider-iaas/waiter"
)

// IPsec site-to-site connection tools (spec 17, task 337-IPSEC), mirroring the
// iaas_vpn_ipsec_connection resource. A connection is a child of a VPN gateway
// and has its own SHOW and LIST routes.
//
// Writes are recorded at once and APPLIED to the gateway asynchronously (the
// connection's derived apply_state goes pending -> applied, or failed). Create,
// update and delete therefore wait for the gateway by default (wait: false
// returns right after the write is recorded, with applied: false); the live
// tunnel (sa_state) depends on the remote peer and is never waited for.
//
// The pre-shared key (psk) is write-only: the API never returns it, no tool
// description or result ever contains it, validation errors never echo it, and
// a defensive scrub removes a "psk" key from any object a tool returns.

func init() {
	toolRegistrars = append(toolRegistrars, registerVPNIpsecTools)
}

const (
	// ipsecApplyTimeout bounds the wait for the gateway to apply one write, the
	// install wait of a first connection included (the platform's own install
	// timeout is 15 minutes).
	ipsecApplyTimeout = 20 * time.Minute
	// ipsecAdoptTimeout bounds the read-back after an uncertain create answer.
	ipsecAdoptTimeout = 30 * time.Second
)

var ipsecNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$`)

// ── inputs / outputs ────────────────────────────────────────────────────────

type ListVpnIpsecInput struct {
	GatewayID string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
}

type GetVpnIpsecInput struct {
	GatewayID    string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	ConnectionID string `json:"connection_id" jsonschema:"UUID of the IPsec connection"`
}

type CreateVpnIpsecInput struct {
	GatewayID        string   `json:"gateway_id" jsonschema:"UUID of the VPN gateway (must be active)"`
	Name             string   `json:"name" jsonschema:"display name: 2-32 lower-case letters, digits or hyphens, starting and ending with a letter or digit, unique per gateway"`
	PSK              string   `json:"psk" jsonschema:"pre-shared key: 16-128 printable ASCII characters without spaces, double quotes or backslashes. Write-only: it is never returned by any tool"`
	RemoteTS         []string `json:"remote_ts" jsonschema:"remote networks reachable through the tunnel: 1-32 IPv4 CIDRs with a /8 to /32 prefix. They must not overlap any other destination of the gateway (VPC, tunnel subnet, WireGuard peers, peerings, other IPsec connections)"`
	RemoteAddress    *string  `json:"remote_address,omitempty" jsonschema:"public IPv4 address or host name of the remote peer; omit only for a responder that identifies the peer by fqdn, email, keyid or any"`
	LocalIDType      *string  `json:"local_id_type,omitempty" jsonschema:"address (default), fqdn, email or keyid"`
	LocalID          *string  `json:"local_id,omitempty" jsonschema:"local identity value matching local_id_type; default is the gateway public IP"`
	RemoteIDType     *string  `json:"remote_id_type,omitempty" jsonschema:"address (default), fqdn, email, keyid or any (any is for a responder only)"`
	RemoteID         *string  `json:"remote_id,omitempty" jsonschema:"remote identity value matching remote_id_type; default is remote_address"`
	IKEVersion       *int     `json:"ike_version,omitempty" jsonschema:"1 or 2 (default 2); IKEv1 needs proposal_preset compat and a remote_address"`
	ProposalPreset   *string  `json:"proposal_preset,omitempty" jsonschema:"modern (default), compat or custom (custom needs ike_proposal and esp_proposal)"`
	IKEProposal      *string  `json:"ike_proposal,omitempty" jsonschema:"IKE proposal list, custom preset only, e.g. aes256gcm16-prfsha384-ecp384"`
	ESPProposal      *string  `json:"esp_proposal,omitempty" jsonschema:"ESP proposal list, custom preset only, e.g. aes256gcm16-ecp384"`
	LocalTS          []string `json:"local_ts,omitempty" jsonschema:"local networks to expose: the VPC range or its subnets, at most 16 IPv4 CIDRs; default is the whole VPC range"`
	Role             *string  `json:"role,omitempty" jsonschema:"initiator (default, starts the tunnel) or responder (waits)"`
	DPDDelay         *int     `json:"dpd_delay,omitempty" jsonschema:"dead-peer-detection interval in seconds, 0-300 (default 30)"`
	DPDAction        *string  `json:"dpd_action,omitempty" jsonschema:"clear, trap or restart (default restart for an initiator, clear for a responder)"`
	IKELifetime      *int     `json:"ike_lifetime,omitempty" jsonschema:"IKE SA lifetime in seconds, 600-86400 (default 14400)"`
	ChildLifetime    *int     `json:"child_lifetime,omitempty" jsonschema:"child SA lifetime in seconds, 300-28800 and not above ike_lifetime (default 3600)"`
	UDPEncapsulation *bool    `json:"udp_encapsulation,omitempty" jsonschema:"use UDP encapsulation (default true); off needs raw ESP, which security groups cannot admit"`
	Enabled          *bool    `json:"enabled,omitempty" jsonschema:"enable the connection (default true)"`
	Wait             *bool    `json:"wait,omitempty" jsonschema:"wait until the gateway has applied the connection (default true); false returns as soon as it is recorded, then poll user.vpn_ipsec.get for apply_state"`
}

type UpdateVpnIpsecInput struct {
	GatewayID        string   `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	ConnectionID     string   `json:"connection_id" jsonschema:"UUID of the IPsec connection"`
	Name             *string  `json:"name,omitempty" jsonschema:"new display name (same grammar as create)"`
	PSK              *string  `json:"psk,omitempty" jsonschema:"new pre-shared key (same shape as create); omit to keep the stored key. Write-only: never returned"`
	RemoteTS         []string `json:"remote_ts,omitempty" jsonschema:"replacement remote networks (same rules as create)"`
	RemoteAddress    *string  `json:"remote_address,omitempty" jsonschema:"new peer address or host name; to remove it use clear (responder only)"`
	LocalIDType      *string  `json:"local_id_type,omitempty" jsonschema:"address, fqdn, email or keyid"`
	LocalID          *string  `json:"local_id,omitempty" jsonschema:"local identity value; to reset it to the gateway public IP use clear"`
	RemoteIDType     *string  `json:"remote_id_type,omitempty" jsonschema:"address, fqdn, email, keyid or any (responder only)"`
	RemoteID         *string  `json:"remote_id,omitempty" jsonschema:"remote identity value; to reset it to remote_address use clear"`
	IKEVersion       *int     `json:"ike_version,omitempty" jsonschema:"1 or 2"`
	ProposalPreset   *string  `json:"proposal_preset,omitempty" jsonschema:"modern, compat or custom"`
	IKEProposal      *string  `json:"ike_proposal,omitempty" jsonschema:"IKE proposal list (custom preset); to remove it use clear"`
	ESPProposal      *string  `json:"esp_proposal,omitempty" jsonschema:"ESP proposal list (custom preset); to remove it use clear"`
	LocalTS          []string `json:"local_ts,omitempty" jsonschema:"replacement local networks (at most 16 IPv4 CIDRs)"`
	Role             *string  `json:"role,omitempty" jsonschema:"initiator or responder"`
	DPDDelay         *int     `json:"dpd_delay,omitempty" jsonschema:"dead-peer-detection interval in seconds, 0-300"`
	DPDAction        *string  `json:"dpd_action,omitempty" jsonschema:"clear, trap or restart"`
	IKELifetime      *int     `json:"ike_lifetime,omitempty" jsonschema:"IKE SA lifetime in seconds, 600-86400"`
	ChildLifetime    *int     `json:"child_lifetime,omitempty" jsonschema:"child SA lifetime in seconds, 300-28800, not above ike_lifetime"`
	UDPEncapsulation *bool    `json:"udp_encapsulation,omitempty" jsonschema:"use UDP encapsulation"`
	Enabled          *bool    `json:"enabled,omitempty" jsonschema:"enable or disable the connection"`
	Clear            []string `json:"clear,omitempty" jsonschema:"names of nullable fields to unset (sent as null): any of remote_address, local_id, remote_id, ike_proposal, esp_proposal. Empty strings are refused everywhere"`
	Wait             *bool    `json:"wait,omitempty" jsonschema:"wait until the gateway has applied the change (default true)"`
}

type DeleteVpnIpsecInput struct {
	GatewayID    string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	ConnectionID string `json:"connection_id" jsonschema:"UUID of the IPsec connection to delete"`
	Wait         *bool  `json:"wait,omitempty" jsonschema:"wait until the gateway has confirmed the removal (default true); the connection's networks stay reserved until it does"`
	Confirmation
}

type RestartVpnIpsecInput struct {
	GatewayID    string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	ConnectionID string `json:"connection_id" jsonschema:"UUID of the IPsec connection"`
}

type RemoteSettingsVpnIpsecInput struct {
	GatewayID    string `json:"gateway_id" jsonschema:"UUID of the VPN gateway"`
	ConnectionID string `json:"connection_id" jsonschema:"UUID of the IPsec connection"`
	Format       string `json:"format,omitempty" jsonschema:"json (default) or text for a plain-text copy"`
}

type VpnIpsecListResult struct {
	Connections []map[string]any `json:"connections"`
	// Gateway is the gateway IPsec state: install state, apply state, generations.
	Gateway map[string]any `json:"gateway,omitempty"`
}

type VpnIpsecConnectionResult struct {
	Connection map[string]any `json:"connection"`
	// Warnings are machine codes such as security_group_blocks_esp.
	Warnings []string `json:"warnings,omitempty"`
	// Applied is true when the gateway has applied the change (apply_state applied).
	Applied bool `json:"applied"`
}

type VpnIpsecDeleteResult struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	// Confirmed is true when the gateway has confirmed the removal.
	Confirmed bool   `json:"confirmed"`
	Message   string `json:"message,omitempty"`
}

type VpnIpsecRemoteSettingsResult struct {
	RemoteSettings map[string]any `json:"remote_settings,omitempty"`
	Text           string         `json:"text,omitempty"`
}

// ── validation ──────────────────────────────────────────────────────────────

// isValidIPsecPSK mirrors the server's App\Rules\IpsecPsk.
func isValidIPsecPSK(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7E || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

// errInvalidIPsecPSK deliberately names no part of the value.
var errInvalidIPsecPSK = errors.New("psk must be 16-128 printable ASCII characters without spaces, double quotes or backslashes (the value is not shown because it is a secret)")

func validateIPsecCIDRs(field string, values []string, min, max, minPrefix int, remote bool) error {
	if len(values) < min || len(values) > max {
		return fmt.Errorf("%s must hold %d to %d IPv4 CIDRs, got %d (an empty list is refused: omit the field instead)", field, min, max, len(values))
	}
	for i, v := range values {
		if !isValidIPv4CIDR(v) {
			return fmt.Errorf("%s[%d] must be a valid IPv4 CIDR block in the form a.b.c.d/p, got: %q", field, i, v)
		}
		prefix, _ := strconv.Atoi(v[strings.IndexByte(v, '/')+1:])
		if prefix < minPrefix {
			return fmt.Errorf("%s[%d] must have a prefix length from /%d to /32, got: %q", field, i, minPrefix, v)
		}
	}
	if p := ipsecSelectorProblem(values, remote); p != "" {
		return fmt.Errorf("%s: %s", field, p)
	}
	return nil
}

// ipsecReserved mirrors App\Rules\IpsecTrafficSelectors::RESERVED.
var ipsecReserved = []string{"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}

func ipsecRange(s string) (first, last uint32, canonical, ok bool) {
	if !isValidIPv4CIDR(s) {
		return 0, 0, false, false
	}
	slash := strings.IndexByte(s, '/')
	prefix, _ := strconv.Atoi(s[slash+1:])
	var ip uint32
	for _, o := range strings.Split(s[:slash], ".") {
		n, _ := strconv.Atoi(o)
		ip = ip<<8 | uint32(n)
	}
	var mask uint32
	if prefix > 0 {
		mask = ^uint32(0) << (32 - prefix)
	}
	return ip & mask, ip | ^mask, ip&mask == ip, true
}

func ipsecOverlap(a, b string) bool {
	af, al, _, aok := ipsecRange(a)
	bf, bl, _, bok := ipsecRange(b)
	return aok && bok && af <= bl && bf <= al
}

// ipsecSelectorProblem mirrors App\Rules\IpsecTrafficSelectors for a list of
// valid CIDRs: canonical form, no overlap inside the list and, for the remote
// list, no reserved range. IPsec specific; the generic CIDR check is unchanged.
func ipsecSelectorProblem(list []string, remote bool) string {
	for _, c := range list {
		if _, _, canonical, ok := ipsecRange(c); ok && !canonical {
			return fmt.Sprintf("%q is not in canonical form (host bits must be zero)", c)
		}
	}
	for i := range list {
		for j := i + 1; j < len(list); j++ {
			if ipsecOverlap(list[i], list[j]) {
				return fmt.Sprintf("%q and %q overlap", list[i], list[j])
			}
		}
	}
	if remote {
		for _, c := range list {
			for _, r := range ipsecReserved {
				if ipsecOverlap(c, r) {
					return fmt.Sprintf("%q touches the reserved range %s", c, r)
				}
			}
		}
	}
	return ""
}

// plainOptional reports whether s survives the platform's request normalisation
// (TrimStrings, ConvertEmptyStringsToNull) unchanged.
func plainOptional(s string) bool { return s != "" && strings.TrimSpace(s) == s }

func oneOf(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, got: %q", field, strings.Join(allowed, ", "), value)
}

func intBetween(field string, v *int, lo, hi int) error {
	if v != nil && (*v < lo || *v > hi) {
		return fmt.Errorf("%s must be between %d and %d, got %d", field, lo, hi, *v)
	}
	return nil
}

// ipsecFields is the field set shared by create and update after both are
// normalised to pointers: nil means "not sent".
type ipsecFields struct {
	name, psk, remoteAddress, localIDType, localID, remoteIDType, remoteID *string
	proposalPreset, ikeProposal, espProposal, role, dpdAction              *string
	remoteTS, localTS                                                      *[]string
	ikeVersion, dpdDelay, ikeLifetime, childLifetime                       *int
	udp, enabled                                                           *bool
	clear                                                                  []string
}

func (f ipsecFields) validate() error {
	for _, c := range f.clear {
		switch c {
		case "remote_address", "local_id", "remote_id", "ike_proposal", "esp_proposal":
		default:
			return fmt.Errorf("clear can name remote_address, local_id, remote_id, ike_proposal or esp_proposal, got: %q", c)
		}
		if (c == "remote_address" && f.remoteAddress != nil) || (c == "local_id" && f.localID != nil) ||
			(c == "remote_id" && f.remoteID != nil) || (c == "ike_proposal" && f.ikeProposal != nil) ||
			(c == "esp_proposal" && f.espProposal != nil) {
			return fmt.Errorf("%s is both set and listed in clear", c)
		}
	}
	if f.name != nil && !ipsecNameRE.MatchString(*f.name) {
		return fmt.Errorf("name must be 2-32 lower-case letters, digits or hyphens, starting and ending with a letter or digit, got: %q", *f.name)
	}
	if f.psk != nil && !isValidIPsecPSK(*f.psk) {
		return errInvalidIPsecPSK
	}
	if f.remoteTS != nil {
		if err := validateIPsecCIDRs("remote_ts", *f.remoteTS, 1, 32, 8, true); err != nil {
			return err
		}
	}
	if f.localTS != nil {
		if err := validateIPsecCIDRs("local_ts", *f.localTS, 1, 16, 0, false); err != nil {
			return err
		}
	}
	for _, c := range []struct {
		field string
		v     *string
	}{
		{"remote_address", f.remoteAddress}, {"local_id", f.localID}, {"remote_id", f.remoteID},
		{"ike_proposal", f.ikeProposal}, {"esp_proposal", f.espProposal},
	} {
		if c.v == nil {
			continue
		}
		if !plainOptional(*c.v) {
			return fmt.Errorf("%s must not be empty or have leading or trailing whitespace (the platform would store an empty string as null); omit it to leave it unset, or use clear on update", c.field)
		}
		if err := validateNoControlCharacters(c.field, *c.v); err != nil {
			return err
		}
		if len(*c.v) > 253 && c.field != "ike_proposal" && c.field != "esp_proposal" {
			return fmt.Errorf("%s must be at most 253 characters", c.field)
		}
	}
	if f.localIDType != nil {
		if err := oneOf("local_id_type", *f.localIDType, "address", "fqdn", "email", "keyid"); err != nil {
			return err
		}
	}
	if f.remoteIDType != nil {
		if err := oneOf("remote_id_type", *f.remoteIDType, "address", "fqdn", "email", "keyid", "any"); err != nil {
			return err
		}
	}
	if f.proposalPreset != nil {
		if err := oneOf("proposal_preset", *f.proposalPreset, "modern", "compat", "custom"); err != nil {
			return err
		}
	}
	if f.role != nil {
		if err := oneOf("role", *f.role, "initiator", "responder"); err != nil {
			return err
		}
	}
	if f.dpdAction != nil {
		if err := oneOf("dpd_action", *f.dpdAction, "clear", "trap", "restart"); err != nil {
			return err
		}
	}
	if f.ikeVersion != nil && *f.ikeVersion != 1 && *f.ikeVersion != 2 {
		return fmt.Errorf("ike_version must be 1 or 2, got %d", *f.ikeVersion)
	}
	for _, r := range []struct {
		field  string
		v      *int
		lo, hi int
	}{
		{"dpd_delay", f.dpdDelay, 0, 300}, {"ike_lifetime", f.ikeLifetime, 600, 86400}, {"child_lifetime", f.childLifetime, 300, 28800},
	} {
		if err := intBetween(r.field, r.v, r.lo, r.hi); err != nil {
			return err
		}
	}
	return nil
}

// validateCreate adds the cross-field rules decidable from a create alone (an
// update merges with the stored connection, which only the server can see).
func (f ipsecFields) validateCreate() error {
	if err := f.validate(); err != nil {
		return err
	}
	preset := "modern"
	if f.proposalPreset != nil {
		preset = *f.proposalPreset
	}
	role := "initiator"
	if f.role != nil {
		role = *f.role
	}
	if f.ikeVersion != nil && *f.ikeVersion == 1 {
		if preset != "compat" {
			return errors.New("ike_version 1 is available with proposal_preset compat only")
		}
		if f.remoteAddress == nil {
			return errors.New("ike_version 1 needs a static remote_address")
		}
	}
	if role != "responder" && f.remoteAddress == nil {
		return errors.New("remote_address is required for an initiator; only role responder may omit it")
	}
	if f.remoteIDType != nil && *f.remoteIDType == "any" && role != "responder" {
		return errors.New("remote_id_type any is accepted for role responder only")
	}
	if preset == "custom" {
		if f.ikeProposal == nil || f.espProposal == nil {
			return errors.New("proposal_preset custom requires both ike_proposal and esp_proposal")
		}
	} else if f.ikeProposal != nil || f.espProposal != nil {
		return errors.New("ike_proposal and esp_proposal can be set with proposal_preset custom only")
	}
	ike, child := 14400, 3600
	if f.ikeLifetime != nil {
		ike = *f.ikeLifetime
	}
	if f.childLifetime != nil {
		child = *f.childLifetime
	}
	if child > ike {
		return fmt.Errorf("child_lifetime (%d) must not be above ike_lifetime (%d); both default to 3600 and 14400", child, ike)
	}
	return nil
}

func createFields(in CreateVpnIpsecInput) ipsecFields {
	name, psk := in.Name, in.PSK
	rts := in.RemoteTS // required: nil and empty both fail the length check
	f := ipsecFields{
		name: &name, psk: &psk, remoteTS: &rts,
		remoteAddress: in.RemoteAddress, localIDType: in.LocalIDType, localID: in.LocalID,
		remoteIDType: in.RemoteIDType, remoteID: in.RemoteID, proposalPreset: in.ProposalPreset,
		ikeProposal: in.IKEProposal, espProposal: in.ESPProposal, role: in.Role, dpdAction: in.DPDAction,
		ikeVersion: in.IKEVersion, dpdDelay: in.DPDDelay, ikeLifetime: in.IKELifetime,
		childLifetime: in.ChildLifetime, udp: in.UDPEncapsulation, enabled: in.Enabled,
	}
	if in.LocalTS != nil { // absent keeps the server default; an explicit empty list is refused
		f.localTS = &in.LocalTS
	}
	return f
}

func updateFields(in UpdateVpnIpsecInput) ipsecFields {
	return ipsecFields{
		name: in.Name, psk: in.PSK, remoteTS: ptrIfSet(in.RemoteTS), localTS: ptrIfSet(in.LocalTS), clear: in.Clear, remoteAddress: in.RemoteAddress,
		localIDType: in.LocalIDType, localID: in.LocalID, remoteIDType: in.RemoteIDType, remoteID: in.RemoteID,
		ikeVersion: in.IKEVersion, proposalPreset: in.ProposalPreset, ikeProposal: in.IKEProposal, espProposal: in.ESPProposal,
		role: in.Role, dpdDelay: in.DPDDelay, dpdAction: in.DPDAction, ikeLifetime: in.IKELifetime,
		childLifetime: in.ChildLifetime, udp: in.UDPEncapsulation, enabled: in.Enabled,
	}
}

// body renders the request body. Empty strings never reach it (validate refuses
// them), and `clear` is the only path that sends a null.
func (f ipsecFields) body(update bool) map[string]any {
	b := map[string]any{}
	put := func(key string, v *string, _ bool) {
		if v != nil {
			b[key] = *v
		}
	}
	for _, c := range f.clear {
		b[c] = nil
	}
	put("name", f.name, false)
	put("psk", f.psk, false)
	put("remote_address", f.remoteAddress, true)
	put("local_id_type", f.localIDType, false)
	put("local_id", f.localID, true)
	put("remote_id_type", f.remoteIDType, false)
	put("remote_id", f.remoteID, true)
	put("proposal_preset", f.proposalPreset, false)
	put("ike_proposal", f.ikeProposal, true)
	put("esp_proposal", f.espProposal, true)
	put("role", f.role, false)
	put("dpd_action", f.dpdAction, false)
	if f.remoteTS != nil {
		b["remote_ts"] = *f.remoteTS
	}
	if f.localTS != nil {
		b["local_ts"] = *f.localTS
	}
	for k, v := range map[string]*int{"ike_version": f.ikeVersion, "dpd_delay": f.dpdDelay, "ike_lifetime": f.ikeLifetime, "child_lifetime": f.childLifetime} {
		if v != nil {
			b[k] = *v
		}
	}
	if f.udp != nil {
		b["udp_encapsulation"] = *f.udp
	}
	if f.enabled != nil {
		b["enabled"] = *f.enabled
	}
	return b
}

// scrubPSK removes secret members at every depth, including arrays. Keep
// psk_version: it is public rotation metadata, not a key.
func scrubPSK(m map[string]any) map[string]any {
	for key, value := range m {
		switch strings.ToLower(key) {
		case "psk", "pre_shared_key", "preshared_key":
			delete(m, key)
		default:
			scrubIPsecValue(value)
		}
	}
	return m
}

func scrubIPsecValue(value any) {
	switch value := value.(type) {
	case map[string]any:
		scrubPSK(value)
	case []any:
		for _, item := range value {
			scrubIPsecValue(item)
		}
	}
}

// At the tool boundary, even a malformed server error must not echo a submitted
// key. Map first so the shared error mapper cannot unwrap an unsanitized cause.
func redactIPsecWriteError(err error, key string) error {
	if err == nil || key == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(MapError(err).Error(), key, "[redacted]"))
}

// ptrIfSet keeps "absent" (nil) distinct from "present but empty": the latter is
// returned as a non-nil pointer so validation can refuse it.
func ptrIfSet(v []string) *[]string {
	if v == nil {
		return nil
	}
	return &v
}

func waitWanted(w *bool) bool { return w == nil || *w }

// ── waiting ─────────────────────────────────────────────────────────────────

// ipsecMaxRetryWait bounds one server-named wait (Retry-After).
const ipsecMaxRetryWait = 60 * time.Second

// ipsecRetryWait reports whether err is a definite refusal that recorded nothing
// and can be retried after a wait: 409 ipsec_installing (poll interval) or 429
// (Retry-After when named, bounded).
func ipsecRetryWait(err error) (time.Duration, bool) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return 0, false
	}
	switch {
	case apiErr.Status == http.StatusConflict && apiErr.Reason == client.ReasonIpsecInstalling:
		return pollInterval(), true
	case apiErr.Status == http.StatusTooManyRequests:
		d := apiErr.RetryAfter
		if d <= 0 {
			d = pollInterval()
		}
		if d > ipsecMaxRetryWait {
			d = ipsecMaxRetryWait
		}
		return d, true
	}
	return 0, false
}

func sleepOrDone(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ipsecUncertain: no definite answer (transport error, 5xx). A 4xx refusal, a
// 429 included, is definite: nothing was recorded.
func ipsecUncertain(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= http.StatusInternalServerError
	}
	return true
}

// ipsecWaitApplied polls the connection until the gateway applies the latest
// generation; "failed" is terminal and names the gateway's apply error.
func ipsecWaitApplied(ctx context.Context, cl *client.Client, gwID, id string) (map[string]any, error) {
	var last map[string]any
	err := waiter.WaitFor(ctx, waiter.Options{
		Interval: pollInterval(),
		Timeout:  ipsecApplyTimeout,
		Refresh: func() (string, bool, error) {
			obj, err := cl.GetVpnIpsecConnection(ctx, gwID, id)
			if err != nil {
				return "", false, err
			}
			last = obj
			switch state, _ := obj["apply_state"].(string); state {
			case "applied":
				return state, true, nil
			case "failed":
				suffix := ""
				if _, gw, lerr := cl.ListVpnIpsecConnections(ctx, gwID); lerr == nil {
					if code, _ := gw["ipsec_apply_error"].(string); code != "" {
						suffix = " (" + code + ")"
					}
				}
				return state, false, fmt.Errorf("the gateway failed to apply the change%s", suffix)
			default:
				// The gateway LIST poll is required: a failed IPsec install never
				// flips the connection's apply_state (the install state is separate),
				// so a pending change would otherwise wait for the whole timeout.
				if _, gw, lerr := cl.ListVpnIpsecConnections(ctx, gwID); lerr == nil && gw != nil {
					if is, _ := gw["ipsec_state"].(string); is == "failed" {
						code := ""
						if c, _ := gw["ipsec_apply_error"].(string); c != "" {
							code = ", " + c
						}
						return state, false, fmt.Errorf("installing IPsec on the gateway failed (ipsec_state failed%s)", code)
					}
				}
				return state, false, nil
			}
		},
	})
	return last, err
}

func ipsecNumber(v any) (int64, bool) {
	if f, ok := v.(float64); ok {
		return int64(f), true
	}
	return 0, false
}

// ipsecWaitGatewayApplied waits until the gateway's applied generation has
// caught up with its generation (a removal included).
func ipsecWaitGatewayApplied(ctx context.Context, cl *client.Client, gwID string) error {
	return waiter.WaitFor(ctx, waiter.Options{
		Interval: pollInterval(),
		Timeout:  ipsecApplyTimeout,
		Refresh: func() (string, bool, error) {
			_, gw, err := cl.ListVpnIpsecConnections(ctx, gwID)
			if err != nil {
				if client.IsNotFound(err) {
					return "gone", true, nil
				}
				return "", false, err
			}
			state, _ := gw["ipsec_apply_state"].(string)
			if state == "failed" {
				code, _ := gw["ipsec_apply_error"].(string)
				return state, false, fmt.Errorf("the gateway failed to apply the change (%s)", code)
			}
			gen, ok1 := ipsecNumber(gw["ipsec_generation"])
			applied, ok2 := ipsecNumber(gw["ipsec_applied_generation"])
			if ok1 && ok2 && applied >= gen {
				return "applied", true, nil
			}
			return state, false, nil
		},
	})
}

// ── handlers ────────────────────────────────────────────────────────────────

func listVpnIpsec(ctx context.Context, cl *client.Client, in ListVpnIpsecInput) (VpnIpsecListResult, error) {
	conns, gw, err := cl.ListVpnIpsecConnections(ctx, in.GatewayID)
	if err != nil {
		return VpnIpsecListResult{}, err
	}
	for _, c := range conns {
		scrubPSK(c)
	}
	if conns == nil {
		conns = []map[string]any{}
	}
	return VpnIpsecListResult{Connections: conns, Gateway: scrubPSK(gw)}, nil
}

func getVpnIpsec(ctx context.Context, cl *client.Client, in GetVpnIpsecInput) (VpnIpsecConnectionResult, error) {
	obj, err := cl.GetVpnIpsecConnection(ctx, in.GatewayID, in.ConnectionID)
	if err != nil {
		return VpnIpsecConnectionResult{}, err
	}
	scrubPSK(obj)
	return VpnIpsecConnectionResult{Connection: obj, Applied: obj["apply_state"] == "applied"}, nil
}

func createVpnIpsec(ctx context.Context, cl *client.Client, in CreateVpnIpsecInput) (result VpnIpsecConnectionResult, resultErr error) {
	defer func() { resultErr = redactIPsecWriteError(resultErr, in.PSK) }()
	f := createFields(in)
	if err := f.validateCreate(); err != nil {
		return VpnIpsecConnectionResult{}, err
	}
	body := f.body(false)

	// Connection ids that exist before the send: an uncertain answer is resolved by
	// adopting a same-name connection that is NOT in this set, never by sending again.
	before, _, err := cl.ListVpnIpsecConnections(ctx, in.GatewayID)
	if err != nil {
		return VpnIpsecConnectionResult{}, err
	}
	known := map[string]bool{}
	for _, c := range before {
		if id, _ := c["id"].(string); id != "" {
			known[id] = true
		}
	}

	var (
		conn     map[string]any
		warnings []string
	)
	deadline := time.Now().Add(ipsecApplyTimeout)
	for {
		conn, warnings, err = cl.CreateVpnIpsecConnection(ctx, in.GatewayID, body)
		wait, retry := ipsecRetryWait(err)
		if !retry {
			break
		}
		// 409 ipsec_installing or 429: nothing was recorded; wait and retry.
		if time.Now().After(deadline) {
			return VpnIpsecConnectionResult{}, fmt.Errorf("timed out waiting to retry (the gateway is installing IPsec or the API is rate limiting): %w", err)
		}
		if werr := sleepOrDone(ctx, wait); werr != nil {
			return VpnIpsecConnectionResult{}, werr
		}
	}
	if err != nil {
		if !ipsecUncertain(err) {
			return VpnIpsecConnectionResult{}, err
		}
		return VpnIpsecConnectionResult{}, uncertainIpsecCreate(cl, in.GatewayID, in.Name, known, err)
	}

	id, _ := conn["id"].(string)
	if id == "" {
		return VpnIpsecConnectionResult{}, errors.New("create response did not include a connection id")
	}
	return finishIpsecWrite(ctx, cl, in.GatewayID, id, conn, warnings, waitWanted(in.Wait))
}

// uncertainIpsecCreate builds the error for a create that got no definite answer.
// The create is never replayed and never adopted: the API has no idempotency key
// or client token for this route, so a same-name connection that appeared after
// the pre-send list may belong to another writer. The caller decides, with the
// exact get call.
func uncertainIpsecCreate(cl *client.Client, gwID, name string, known map[string]bool, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), ipsecAdoptTimeout)
	defer cancel()
	conns, _, err := cl.ListVpnIpsecConnections(ctx, gwID)
	if err != nil {
		return fmt.Errorf("the create request got no definite answer (%v) and the connection list could not be read (%v); "+
			"the connection may or may not exist: call user.vpn_ipsec.list for gateway %s before creating it again", cause, err, gwID)
	}
	for _, c := range conns {
		id, _ := c["id"].(string)
		if n, _ := c["name"].(string); n == name && id != "" && !known[id] {
			return fmt.Errorf("the create request got no definite answer (%v). A connection named %q (id %s) now exists on gateway %s, "+
				"but it cannot be proven to be the one this request created (the API has no request token). Inspect it with "+
				"user.vpn_ipsec.get {gateway_id: %q, connection_id: %q}; if it is yours, rotate its key with user.vpn_ipsec.update, "+
				"otherwise create again", cause, name, id, gwID, gwID, id)
		}
	}
	return fmt.Errorf("the create request got no definite answer (%v) and no connection named %q exists on the gateway yet; "+
		"it was probably not recorded, so create again (a late-recorded one would then be refused as name_taken)", cause, name)
}

// finishIpsecWrite waits (when asked) for the gateway to apply the write and
// returns the final object.
func finishIpsecWrite(ctx context.Context, cl *client.Client, gwID, id string, conn map[string]any, warnings []string, wait bool) (VpnIpsecConnectionResult, error) {
	if wait {
		final, err := ipsecWaitApplied(ctx, cl, gwID, id)
		if final != nil {
			conn = final
		}
		if err != nil {
			return VpnIpsecConnectionResult{}, fmt.Errorf("IPsec connection %s was recorded but is not applied: %w", id, err)
		}
	}
	scrubPSK(conn)
	return VpnIpsecConnectionResult{Connection: conn, Warnings: warnings, Applied: conn["apply_state"] == "applied"}, nil
}

func updateVpnIpsec(ctx context.Context, cl *client.Client, in UpdateVpnIpsecInput) (result VpnIpsecConnectionResult, resultErr error) {
	defer func() {
		if in.PSK != nil {
			resultErr = redactIPsecWriteError(resultErr, *in.PSK)
		}
	}()
	f := updateFields(in)
	if err := f.validate(); err != nil {
		return VpnIpsecConnectionResult{}, err
	}
	body := f.body(true)
	if len(body) == 0 {
		return VpnIpsecConnectionResult{}, errors.New("nothing to update: send at least one field")
	}

	var (
		conn     map[string]any
		warnings []string
		err      error
	)
	deadline := time.Now().Add(ipsecApplyTimeout)
	for {
		conn, warnings, err = cl.UpdateVpnIpsecConnection(ctx, in.GatewayID, in.ConnectionID, body)
		wait, retry := ipsecRetryWait(err)
		if !retry {
			break
		}
		if time.Now().After(deadline) {
			return VpnIpsecConnectionResult{}, fmt.Errorf("timed out waiting to retry (the gateway is installing IPsec or the API is rate limiting): %w", err)
		}
		if werr := sleepOrDone(ctx, wait); werr != nil {
			return VpnIpsecConnectionResult{}, werr
		}
	}
	if err != nil {
		return VpnIpsecConnectionResult{}, err
	}
	return finishIpsecWrite(ctx, cl, in.GatewayID, in.ConnectionID, conn, warnings, waitWanted(in.Wait))
}

func deleteVpnIpsec(ctx context.Context, cl *client.Client, in DeleteVpnIpsecInput) (VpnIpsecDeleteResult, error) {
	if err := cl.DeleteVpnIpsecConnection(ctx, in.GatewayID, in.ConnectionID); err != nil {
		if !client.IsNotFound(err) {
			return VpnIpsecDeleteResult{}, err
		}
	}
	res := VpnIpsecDeleteResult{ID: in.ConnectionID, Deleted: true}
	if !waitWanted(in.Wait) {
		res.Message = "deleted; the gateway removes it asynchronously and keeps its networks reserved until then"
		return res, nil
	}
	if err := ipsecWaitGatewayApplied(ctx, cl, in.GatewayID); err != nil {
		res.Message = "deleted, but the gateway has not confirmed the removal yet (" + err.Error() +
			"); its networks stay reserved until it does"
		return res, nil
	}
	res.Confirmed = true
	return res, nil
}

func restartVpnIpsec(ctx context.Context, cl *client.Client, in RestartVpnIpsecInput) (VpnIpsecConnectionResult, error) {
	obj, err := cl.RestartVpnIpsecConnection(ctx, in.GatewayID, in.ConnectionID)
	if err != nil {
		return VpnIpsecConnectionResult{}, err
	}
	scrubPSK(obj)
	return VpnIpsecConnectionResult{Connection: obj, Applied: obj["apply_state"] == "applied"}, nil
}

func remoteSettingsVpnIpsec(ctx context.Context, cl *client.Client, in RemoteSettingsVpnIpsecInput) (VpnIpsecRemoteSettingsResult, error) {
	switch in.Format {
	case "", "json":
		obj, err := cl.GetVpnIpsecRemoteSettings(ctx, in.GatewayID, in.ConnectionID)
		if err != nil {
			return VpnIpsecRemoteSettingsResult{}, err
		}
		return VpnIpsecRemoteSettingsResult{RemoteSettings: scrubPSK(obj)}, nil
	case "text":
		txt, err := cl.GetVpnIpsecRemoteSettingsText(ctx, in.GatewayID, in.ConnectionID)
		if err != nil {
			return VpnIpsecRemoteSettingsResult{}, err
		}
		return VpnIpsecRemoteSettingsResult{Text: txt}, nil
	default:
		return VpnIpsecRemoteSettingsResult{}, fmt.Errorf("format must be json or text, got: %q", in.Format)
	}
}

func registerVPNIpsecTools(s *mcp.Server, deps Deps) {
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.list",
		Description: "List the IPsec site-to-site connections of a VPN gateway with their live tunnel state (sa_state) and " +
			"the gateway IPsec state (install state, apply state, generations and unattributed diagnostic code/time). The pre-shared key is never returned.",
	}, listVpnIpsec)
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.get",
		Description: "Get one IPsec connection with its live status (sa_state, apply_state, typed last_error_code and fixed diagnostic sentence). " +
			"The pre-shared key is never returned.",
	}, getVpnIpsec)
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.create",
		Description: "Create an IPsec site-to-site connection (IKEv2 or IKEv1, pre-shared key) on an active VPN gateway. " +
			"The first connection also installs the IPsec packages in the gateway, so this can take several minutes. " +
			"By default it waits until the gateway has applied the connection (wait: false returns right after it is recorded); " +
			"the live tunnel (sa_state) depends on the remote device and is not waited for. " +
			"Use user.vpn_ipsec.remote_settings for the values to enter on the other device.",
	}, createVpnIpsec)
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.update",
		Description: "Partially update an IPsec connection: only the fields sent change. Omit psk to keep the stored key; " +
			"a new psk replaces it in place; use clear to unset a nullable field. By default it waits until the gateway has applied the change. " +
			"A change that alters nothing does not touch the tunnel.",
	}, updateVpnIpsec)
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.delete",
		Description: "Delete an IPsec connection. It disappears from reads at once and is removed from the gateway " +
			"asynchronously; by default this waits for the gateway to confirm. DESTRUCTIVE: requires \"confirm\": true.",
		Destructive: true,
	}, deleteVpnIpsec)
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.restart",
		Description: "Terminate and re-initiate an IPsec tunnel (also the retry after a phase 2 failure). The connection must " +
			"be enabled. A 503 node_dispatch_unconfirmed means the restart is stored but the gateway has not confirmed it yet; " +
			"the platform retries the stored intent automatically. Do not repeat the restart to poll it; use user.vpn_ipsec.get. A successful reply confirms the request, not an established tunnel.",
	}, restartVpnIpsec)
	Register(s, deps, Spec{
		Name: "user.vpn_ipsec.remote_settings",
		Description: "Get the values to enter on the other device of an IPsec connection (identities, proposals, networks, " +
			"lifetimes, NAT traversal, per-subnet route commands with subnet, cidr and gateway). format text returns the platform's plain-text summary; use json for route commands. Never includes the pre-shared key.",
	}, remoteSettingsVpnIpsec)
}
