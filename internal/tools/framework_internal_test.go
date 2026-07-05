package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
)

// White-box tests for the framework's cross-cutting helpers that the black-box
// tool tests exercise only indirectly.

func TestMapError_ByStatus(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantPrefix string // "" means: expect the error returned unchanged
	}{
		{"nil", nil, ""},
		{"404", &client.APIError{Status: 404, Message: "gone"}, "not found:"},
		{"401", &client.APIError{Status: 401, Message: "nope"}, "access denied:"},
		{"403", &client.APIError{Status: 403, Message: "nope"}, "access denied:"},
		{"422", &client.APIError{Status: 422, Message: "bad"}, "validation failed:"},
		{"429", &client.APIError{Status: 429, Message: "slow down"}, "rate limited (retryable):"},
		{"500 passthrough", &client.APIError{Status: 500, Message: "boom"}, ""},
		{"non-api passthrough", errors.New("dial tcp: refused"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapError(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("MapError(nil) = %v, want nil", got)
				}
				return
			}
			if tc.wantPrefix == "" {
				// Passthrough: the original error (or its message) is preserved.
				if !strings.Contains(got.Error(), tc.err.Error()) {
					t.Fatalf("MapError(%v) = %q, want it to preserve the original", tc.err, got)
				}
				return
			}
			if !strings.HasPrefix(got.Error(), tc.wantPrefix) {
				t.Fatalf("MapError(%v) = %q, want prefix %q", tc.err, got, tc.wantPrefix)
			}
			// The wrapped APIError must remain unwrappable for callers using errors.As.
			var apiErr *client.APIError
			if !errors.As(got, &apiErr) {
				t.Fatalf("mapped error no longer unwraps to *client.APIError")
			}
		})
	}
}

// TestConfirmed covers the destructive-op gate's type assertion: an input that
// embeds Confirmation reports its flag; an input that does not embed it always
// reads as not-confirmed (fail closed).
func TestConfirmed(t *testing.T) {
	type gated struct {
		Confirmation
	}
	type ungated struct{ ID string }

	if confirmed(gated{Confirmation{Confirm: true}}) != true {
		t.Errorf("confirmed(gated{true}) = false, want true")
	}
	if confirmed(gated{Confirmation{Confirm: false}}) != false {
		t.Errorf("confirmed(gated{false}) = true, want false")
	}
	if confirmed(ungated{ID: "x"}) != false {
		t.Errorf("confirmed(ungated) = true, want false (fail closed)")
	}
}

// TestIdempotencySeam covers the idempotency plumbing end to end at the helper
// level: an input embedding Idempotent surfaces its key, the framework threads
// it onto ctx, and IdempotencyKeyFromContext reads it back - the exact path a
// future client method (e.g. Kubernetes create) would use.
func TestIdempotencySeam(t *testing.T) {
	type mutating struct {
		Name string
		Idempotent
	}

	in := mutating{Name: "x", Idempotent: Idempotent{IdempotencyKey: "abc-123"}}
	if got := idempotencyKeyOf(in); got != "abc-123" {
		t.Fatalf("idempotencyKeyOf = %q, want abc-123", got)
	}

	// An input without the embed carries no key.
	type plain struct{ Name string }
	if got := idempotencyKeyOf(plain{Name: "y"}); got != "" {
		t.Fatalf("idempotencyKeyOf(plain) = %q, want empty", got)
	}

	// Round-trip through context.
	ctx := withIdempotencyKey(context.Background(), "abc-123")
	if got := IdempotencyKeyFromContext(ctx); got != "abc-123" {
		t.Fatalf("IdempotencyKeyFromContext = %q, want abc-123", got)
	}
	if got := IdempotencyKeyFromContext(context.Background()); got != "" {
		t.Fatalf("IdempotencyKeyFromContext(empty) = %q, want empty", got)
	}
}
