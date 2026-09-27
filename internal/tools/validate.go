package tools

import (
	"fmt"
	"regexp"
	"strconv"
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

// ipv4CIDRPattern is a direct port of the Master repo's server-side rule
// (app/Rules/Ipv4Cidr.php): four decimal octets and a decimal prefix,
// separated by "." and "/", ASCII digits only ([0-9], never a Unicode
// digit lookalike). Each captured segment is re-checked below for range
// and leading-zero. This deliberately does NOT use net.ParseCIDR: that
// accepts a leading-zero prefix (net.ParseCIDR("10.0.0.0/08", ...) is
// valid Go, but the server's regex requires no leading zero) and an
// IPv4-mapped IPv6 literal (::ffff:10.0.0.0/120, whose IP.To4() is
// non-nil even though the server's regex never matches it at all).
var ipv4CIDRPattern = regexp.MustCompile(`^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})/([0-9]{1,2})$`)

// isValidIPv4CIDRSegment reports whether s is a decimal integer with no
// leading zero (unless s is exactly "0") that does not exceed max -
// mirrors Ipv4Cidr::isValid()'s per-segment check:
// `(string) (int) $octet !== $octet || (int) $octet > 255`.
func isValidIPv4CIDRSegment(s string, max int) bool {
	if len(s) > 1 && s[0] == '0' {
		return false
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}

	return n <= max
}

// isValidIPv4CIDR is a direct port of App\Rules\Ipv4Cidr::isValid() in the
// Master repo, so MCP tool-side validation accepts EXACTLY what the API's
// own server-side request validation accepts: four octets 0-255 with no
// leading zero, a prefix 0-32 with no leading zero, and nothing else in
// the string. Host bits set are allowed (10.0.0.1/24 is valid).
func isValidIPv4CIDR(value string) bool {
	m := ipv4CIDRPattern.FindStringSubmatch(value)
	if m == nil {
		return false
	}

	for _, octet := range m[1:5] {
		if !isValidIPv4CIDRSegment(octet, 255) {
			return false
		}
	}

	return isValidIPv4CIDRSegment(m[5], 32)
}

// validateIPv4CIDR requires value to be a strict IPv4 CIDR block (a.b.c.d/p,
// octets 0-255 with no leading zero, prefix 0-32 with no leading zero),
// matching the tunnel_subnet/cidr contract enforced server-side for VPN
// gateway deploy and VPC create. An empty value is not an error here - the
// caller decides whether the field is required (tunnel_subnet defaults
// server-side when omitted).
func validateIPv4CIDR(field, value string) error {
	if value == "" {
		return nil
	}

	if !isValidIPv4CIDR(value) {
		return fmt.Errorf(
			"%s must be a valid IPv4 CIDR block in the form a.b.c.d/p (octets 0-255, prefix 0-32, no leading zeros), got: %q",
			field, value,
		)
	}

	return nil
}
