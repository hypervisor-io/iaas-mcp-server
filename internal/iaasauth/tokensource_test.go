package iaasauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// TestTokenSource_BuildsClientFromBearerHeader drives real HTTP requests
// through auth.RequireBearerToken(Verifier, nil) - the exact middleware
// main.go wires in front of the MCP handler - and asserts that TokenSource
// then builds a client for a valid header and fails closed otherwise. This
// is a table test over the seam end to end: header in, client (or error)
// out.
func TestTokenSource_BuildsClientFromBearerHeader(t *testing.T) {
	cases := []struct {
		name           string
		authHeader     string
		wantMiddleware int  // expected HTTP status from the auth middleware
		wantTokenErr   bool // expected error from TokenSource, when middleware lets the request through
	}{
		{
			name:           "valid bearer token",
			authHeader:     "Bearer secret-platform-token",
			wantMiddleware: http.StatusOK,
			wantTokenErr:   false,
		},
		{
			name:           "missing header",
			authHeader:     "",
			wantMiddleware: http.StatusUnauthorized,
		},
		{
			name:           "empty bearer token",
			authHeader:     "Bearer ",
			wantMiddleware: http.StatusUnauthorized,
		},
		{
			name:           "wrong scheme",
			authHeader:     "Basic dXNlcjpwYXNz",
			wantMiddleware: http.StatusUnauthorized,
		},
		{
			name:           "malformed header, no token part",
			authHeader:     "Bearer",
			wantMiddleware: http.StatusUnauthorized,
		},
	}

	cfg := Config{
		APIEndpoint:    "https://panel.example.com/api",
		RequestTimeout: 5 * time.Second,
		Insecure:       false,
	}
	tokenSource := NewTokenSource(cfg)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotStatus   int
				tokenErr    error
				clientBuilt bool
			)

			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := tokenSource(r.Context())
				tokenErr = err
				clientBuilt = c != nil
				w.WriteHeader(http.StatusOK)
			})

			handler := auth.RequireBearerToken(Verifier, nil)(inner)

			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			gotStatus = rec.Code

			if gotStatus != tc.wantMiddleware {
				t.Fatalf("middleware status = %d, want %d", gotStatus, tc.wantMiddleware)
			}

			// TokenSource only runs at all when the middleware let the request
			// through (status 200 in this test's inner handler).
			if gotStatus == http.StatusOK {
				if tc.wantTokenErr && tokenErr == nil {
					t.Fatalf("TokenSource err = nil, want an error")
				}
				if !tc.wantTokenErr {
					if tokenErr != nil {
						t.Fatalf("TokenSource err = %v, want nil", tokenErr)
					}
					if !clientBuilt {
						t.Fatalf("TokenSource returned a nil client with no error")
					}
				}
			}
		})
	}
}

// TestTokenSource_NoTokenOnContext exercises the defensive ErrNoToken path
// directly: a context that never passed through the auth middleware (so it
// carries no auth.TokenInfo at all) must fail closed rather than build a
// client with an empty token.
func TestTokenSource_NoTokenOnContext(t *testing.T) {
	tokenSource := NewTokenSource(Config{APIEndpoint: "https://panel.example.com/api"})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	_, err := tokenSource(req.Context())
	if err == nil {
		t.Fatal("expected ErrNoToken, got nil")
	}
}
