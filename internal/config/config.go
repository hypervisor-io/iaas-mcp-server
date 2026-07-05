// Package config loads the small set of environment variables the server
// needs to start. There is no config file and no flags: this process is
// meant to run as a stateless container/replica, configured entirely by its
// environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	// EnvAPIEndpoint is the base URL of the platform user API, e.g.
	// "https://panel.example.com/api". Required.
	EnvAPIEndpoint = "IAAS_API_ENDPOINT"

	// EnvListenAddr is the address the HTTP server binds to. Optional,
	// defaults to DefaultListenAddr.
	EnvListenAddr = "IAAS_MCP_LISTEN"

	// EnvRequestTimeout is the per-request HTTP client timeout, parsed with
	// time.ParseDuration (e.g. "30s"). Optional, defaults to
	// DefaultRequestTimeout.
	EnvRequestTimeout = "IAAS_API_TIMEOUT"

	// EnvInsecure, when "true", disables TLS certificate verification on
	// calls to the platform API. Optional, defaults to false. Intended only
	// for staging environments with self-signed certificates.
	EnvInsecure = "IAAS_API_INSECURE"

	// DefaultListenAddr is used when EnvListenAddr is unset.
	DefaultListenAddr = ":8080"

	// DefaultRequestTimeout is used when EnvRequestTimeout is unset.
	DefaultRequestTimeout = 30 * time.Second
)

// Config holds the process-wide settings needed to run the server and to
// build per-request API clients.
type Config struct {
	// APIEndpoint is the platform user API base URL. Passed straight through
	// to client.New for every request; it is not per-tenant, only the bearer
	// token is.
	APIEndpoint string

	// ListenAddr is the address the HTTP server listens on.
	ListenAddr string

	// RequestTimeout bounds every outbound call to the platform API.
	RequestTimeout time.Duration

	// Insecure disables TLS verification for outbound API calls.
	Insecure bool
}

// Load reads Config from the process environment, applying defaults for
// optional values. It returns an error if IAAS_API_ENDPOINT is unset, since
// the server cannot build API clients without it.
func Load() (Config, error) {
	endpoint := os.Getenv(EnvAPIEndpoint)
	if endpoint == "" {
		return Config{}, fmt.Errorf("%s is required (base URL of the platform user API)", EnvAPIEndpoint)
	}

	listen := os.Getenv(EnvListenAddr)
	if listen == "" {
		listen = DefaultListenAddr
	}

	timeout := DefaultRequestTimeout
	if raw := os.Getenv(EnvRequestTimeout); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: invalid duration %q: %w", EnvRequestTimeout, raw, err)
		}
		timeout = parsed
	}

	insecure := false
	if raw := os.Getenv(EnvInsecure); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%s: invalid bool %q: %w", EnvInsecure, raw, err)
		}
		insecure = parsed
	}

	return Config{
		APIEndpoint:    endpoint,
		ListenAddr:     listen,
		RequestTimeout: timeout,
		Insecure:       insecure,
	}, nil
}
