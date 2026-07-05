// Command iaas-mcp-server runs a stateless Streamable-HTTP MCP server for
// the Hypervisor.io platform user API. See specs/17-opentofu-mcp-api-trisync.md
// (in the Master repo) and docs/plans/2026-07-06-mcp-server-build.md for the
// design this scaffolds.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hypervisor-io/iaas-mcp-server/internal/config"
	"github.com/hypervisor-io/iaas-mcp-server/internal/iaasauth"
	"github.com/hypervisor-io/iaas-mcp-server/internal/mcpserver"
)

// version is overridable at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	tokenSource := iaasauth.NewTokenSource(iaasauth.Config{
		APIEndpoint:    cfg.APIEndpoint,
		RequestTimeout: cfg.RequestTimeout,
		Insecure:       cfg.Insecure,
	})

	handler := mcpserver.New(mcpserver.Options{
		Version:     version,
		TokenSource: tokenSource,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("mcp server listening", "addr", cfg.ListenAddr, "api_endpoint", cfg.APIEndpoint)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		logger.Info("shutdown complete")
		return nil
	}
}
