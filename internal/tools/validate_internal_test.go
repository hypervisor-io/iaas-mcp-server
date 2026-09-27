package tools

import (
	"strings"
	"testing"
)

// This file is package tools (not tools_test), so it can exercise the
// unexported validate.go helpers directly with a table of exact values,
// mirroring the equivalent table tests in the terraform-provider-iaas repo's
// internal/validators package.

func TestValidateNoControlCharacters(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value     string
		wantError bool
	}{
		"plain-name":         {value: "office-vpn", wantError: false},
		"endpoint-host-port": {value: "203.0.113.1:51820", wantError: false},
		"cidr-allowed-ip":    {value: "10.0.0.0/24", wantError: false},
		"empty":              {value: "", wantError: false},
		"unicode":            {value: "büro-vpn", wantError: false},
		"null-byte":          {value: "peer\x00name", wantError: true},
		"newline":            {value: "peer\nname", wantError: true},
		"carriage-return":    {value: "peer\rname", wantError: true},
		"tab":                {value: "peer\tname", wantError: true},
		"escape":             {value: "peer\x1bname", wantError: true},
		"del":                {value: "peer\x7fname", wantError: true},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := validateNoControlCharacters("field", tc.value)
			if (err != nil) != tc.wantError {
				t.Fatalf("validateNoControlCharacters(%q) error = %v, wantError %v", tc.value, err, tc.wantError)
			}
		})
	}
}

func TestValidateNoControlCharactersEach(t *testing.T) {
	t.Parallel()

	if err := validateNoControlCharactersEach("allowed_ips", []string{"10.0.0.0/24", "192.168.0.0/16"}); err != nil {
		t.Fatalf("all-clean slice should not error, got: %v", err)
	}

	if err := validateNoControlCharactersEach("allowed_ips", nil); err != nil {
		t.Fatalf("nil slice should not error, got: %v", err)
	}

	err := validateNoControlCharactersEach("allowed_ips", []string{"10.0.0.0/24", "10.0.0.0/24;\rrm -rf /"})
	if err == nil {
		t.Fatal("a dirty element should error")
	}
	if got := err.Error(); !strings.Contains(got, "allowed_ips[1]") {
		t.Errorf("error = %q, want it to name allowed_ips[1]", got)
	}
}

func TestValidateIPv4CIDR(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value     string
		wantError bool
	}{
		"class-c":                   {value: "10.0.0.0/24", wantError: false},
		"default-route":             {value: "0.0.0.0/0", wantError: false},
		"single-host":               {value: "10.99.0.0/32", wantError: false},
		"empty-is-optional":         {value: "", wantError: false},
		"command-injection-suffix":  {value: "10.99.0.0/24 -j MASQUERADE", wantError: true},
		"shell-metacharacter-in-ip": {value: "touch /tmp/x; #", wantError: true},
		"missing-prefix":            {value: "10.0.0.0", wantError: true},
		"octet-out-of-range":        {value: "256.0.0.0/8", wantError: true},
		"prefix-out-of-range":       {value: "10.0.0.0/33", wantError: true},
		"ipv6":                      {value: "2001:db8::/32", wantError: true},
		"leading-space":             {value: " 10.0.0.0/24", wantError: true},
		"trailing-space":            {value: "10.0.0.0/24 ", wantError: true},
		"embedded-newline":          {value: "10.0.0.0/24\n", wantError: true},

		// Shared server-parity table (Master app/Rules/Ipv4Cidr.php and its
		// tests/Unit/Rules/Ipv4CidrTest.php): the client must accept and
		// reject EXACTLY what the server's regex does, never merely a
		// syntactically-valid net.ParseCIDR value.
		"leading-zero-prefix-08":      {value: "10.0.0.0/08", wantError: true},
		"leading-zero-prefix-00":      {value: "0.0.0.0/00", wantError: true},
		"leading-zero-prefix-032":     {value: "10.0.0.0/032", wantError: true},
		"ipv4-mapped-ipv6-prefix-120": {value: "::ffff:10.0.0.0/120", wantError: true},
		"ipv4-mapped-ipv6-prefix-24":  {value: "::ffff:10.0.0.0/24", wantError: true},
		"leading-zero-octet":          {value: "010.0.0.0/24", wantError: true},
		"host-bits-set":               {value: "10.0.0.1/24", wantError: false},
		"full-tunnel":                 {value: "0.0.0.0/0", wantError: false},
		"max-octets-host":             {value: "255.255.255.255/32", wantError: false},
		"wide-slash-eight":            {value: "10.0.0.0/8", wantError: false},
		"prefix-128-ipv6-max":         {value: "10.99.0.0/128", wantError: true},
		"too-few-octets":              {value: "10.99.0/24", wantError: true},
		"too-many-octets":             {value: "10.99.0.0.1/24", wantError: true},
		"octet-over-255":              {value: "10.99.0.256/24", wantError: true},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := validateIPv4CIDR("field", tc.value)
			if (err != nil) != tc.wantError {
				t.Fatalf("validateIPv4CIDR(%q) error = %v, wantError %v", tc.value, err, tc.wantError)
			}
		})
	}
}
