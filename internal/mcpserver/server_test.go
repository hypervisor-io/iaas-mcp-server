package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
)

func testOptions() Options {
	return Options{
		Version: "test",
		TokenSource: iaasauth.NewTokenSource(iaasauth.Config{
			APIEndpoint:    "https://panel.example.com/api",
			RequestTimeout: 5 * time.Second,
		}),
	}
}

func TestHealthz_Returns200(t *testing.T) {
	handler := New(testOptions())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestMCPEndpoint_RequiresBearerToken asserts the seam is actually wired: a
// request to the MCP endpoint with no Authorization header must be rejected
// with the MCP auth error (HTTP 401) before it ever reaches the MCP
// handler/getServer, and a request that does carry a bearer token must get
// past that gate (it need not succeed at the JSON-RPC layer for this test -
// only that auth did not block it).
func TestMCPEndpoint_RequiresBearerToken(t *testing.T) {
	handler := New(testOptions())

	t.Run("no token is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST /mcp with no token: status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("bearer token passes the auth gate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer some-platform-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusUnauthorized {
			t.Fatalf("POST /mcp with a bearer token: status = %d, want anything but %d", rec.Code, http.StatusUnauthorized)
		}
	})
}
