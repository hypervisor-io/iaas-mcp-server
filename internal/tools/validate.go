package tools

import (
	"fmt"
	"net"
)

// This file holds tool-side input validation that mirrors server-side
// user_api request rules the platform enforces on VPN gateway/peer and VPC
// fields. It exists so a hostile or malformed value fails fast, with a clear
// tool error, before the client ever makes an HTTP call - matching the
// Master repo's tri-sync contract (see api.md / api-contract.md) rather than
// letting the API's own 422 be the only signal. These are duplicated,
// intentionally simple checks: the platform's own validation remains the
// authority, this is a plan-time-equivalent convenience for MCP callers.

// validateNoControlCharacters rejects any C0 control character (0x00-0x1F)
// or DEL (0x7F) in value, matching the user_api's "invalid_control_characters"
// rule for VPN peer fields (name, endpoint, preshared_key, public_key, dns,
// and each allowed_ips element). An empty value is not an error here - the
// caller decides whether the field is required.
func validateNoControlCharacters(field, value string) error {
	for i, r := range value {
		if (r >= 0x00 && r <= 0x1F) || r == 0x7F {
			return fmt.Errorf(
				"%s must not contain control characters (0x00-0x1F or 0x7F); found byte 0x%02X at offset %d",
				field, r, i,
			)
		}
	}

	return nil
}

// validateNoControlCharactersEach applies validateNoControlCharacters to
// every element of values, naming the failing element's index in the field
// label (e.g. "allowed_ips[2]").
func validateNoControlCharactersEach(field string, values []string) error {
	for i, v := range values {
		if err := validateNoControlCharacters(fmt.Sprintf("%s[%d]", field, i), v); err != nil {
			return err
		}
	}

	return nil
}

// validateVpnPeerStrings validates the full VPN peer field set the user_api's
// "invalid_control_characters" rule covers on add (POST): name, endpoint,
// preshared_key, public_key, dns, and each allowed_ips element.
func validateVpnPeerStrings(name, endpoint, presharedKey, publicKey, dns string, allowedIPs []string) error {
	for _, f := range []struct {
		field string
		value string
	}{
		{"name", name},
		{"endpoint", endpoint},
		{"preshared_key", presharedKey},
		{"public_key", publicKey},
		{"dns", dns},
	} {
		if err := validateNoControlCharacters(f.field, f.value); err != nil {
			return err
		}
	}

	return validateNoControlCharactersEach("allowed_ips", allowedIPs)
}

// validateVpnPeerUpdateStrings validates the subset of VPN peer fields the
// user_api's "invalid_control_characters" rule covers that are actually
// updatable (PATCH): name, public_key, endpoint, and each allowed_ips
// element. Pointer fields are only checked when the caller set them.
func validateVpnPeerUpdateStrings(name, publicKey, endpoint *string, allowedIPs []string) error {
	for _, f := range []struct {
		field string
		value *string
	}{
		{"name", name},
		{"public_key", publicKey},
		{"endpoint", endpoint},
	} {
		if f.value == nil {
			continue
		}

		if err := validateNoControlCharacters(f.field, *f.value); err != nil {
			return err
		}
	}

	return validateNoControlCharactersEach("allowed_ips", allowedIPs)
}

// validateIPv4CIDR requires value to be a strict IPv4 CIDR block (a.b.c.d/p,
// prefix 0-32), matching the tunnel_subnet/cidr contract enforced server-side
// for VPN gateway deploy and VPC create. An empty value is not an error here -
// the caller decides whether the field is required (tunnel_subnet defaults
// server-side when omitted).
func validateIPv4CIDR(field, value string) error {
	if value == "" {
		return nil
	}

	ip, ipNet, err := net.ParseCIDR(value)
	if err != nil || ipNet == nil {
		return fmt.Errorf(
			"%s must be a valid IPv4 CIDR block in the form a.b.c.d/p (prefix 0-32), got: %q",
			field, value,
		)
	}

	if ip.To4() == nil || ipNet.IP.To4() == nil {
		return fmt.Errorf("%s must be an IPv4 CIDR block, got an IPv6 value: %q", field, value)
	}

	return nil
}
