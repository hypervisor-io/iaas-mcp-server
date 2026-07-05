package tools

import (
	"context"
	"os"
	"time"

	"github.com/hypervisor-io/terraform-provider-iaas/client"
	"github.com/hypervisor-io/terraform-provider-iaas/waiter"
)

// Async convergence for the golden tools. These reuse the provider's waiter
// package and mirror the provider resources' Ready/Fail predicates exactly, so
// the MCP server and the OpenTofu provider converge on identical terminal
// states (spec 17: one behavior, two consumers).

const (
	// defaultCreateTimeout bounds instance-deploy convergence. Matches the
	// provider's iaas_instance default (deploy is a real OS install).
	defaultCreateTimeout = 30 * time.Minute
	// defaultDeleteTimeout bounds delete convergence (slave finalizes and the
	// row soft-deletes asynchronously). Matches the provider default.
	defaultDeleteTimeout = 30 * time.Minute
	// defaultPollInterval is the base poll interval for both waiters.
	defaultPollInterval = 5 * time.Second
)

// pollInterval returns the waiter poll interval. IAAS_MCP_POLL_INTERVAL (a Go
// duration such as "1ms") overrides it - a TEST-ONLY knob so mock-backed tests
// converge instantly without real sleeps. An unset/invalid value yields the 5s
// default, so production behavior is unchanged. This mirrors the provider's
// IAAS_INSTANCE_POLL_INTERVAL seam.
func pollInterval() time.Duration {
	if v := os.Getenv("IAAS_MCP_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultPollInterval
}

// waitForInstanceDeploy polls the deploy task until it reaches "completed"
// (ready) or "failed" (terminal), reusing the provider's exact predicates via
// waiter.StatePollerWithErrorTolerance. Tolerance=3 skips up to three
// consecutive transport blips during a long deploy (the client only retries
// 429/5xx internally; raw transport errors reach the waiter directly). This is
// identical to iaas_instance's Create waiter.
func waitForInstanceDeploy(ctx context.Context, cl *client.Client, instanceID, taskID string, timeout time.Duration) error {
	return waiter.WaitFor(ctx, waiter.Options{
		Interval: pollInterval(),
		Timeout:  timeout,
		Refresh: waiter.StatePollerWithErrorTolerance(
			func() (map[string]any, error) { return cl.GetInstanceTask(ctx, instanceID, taskID) },
			"status",
			[]string{"completed"},
			[]string{"failed"},
			3,
		),
	})
}

// waitForInstanceGone polls SHOW until it 404s, the convergence signal for an
// async delete. Mirrors iaas_instance's Delete waiter: an IsNotFound error is
// "done"; any other error is terminal.
func waitForInstanceGone(ctx context.Context, cl *client.Client, instanceID string, timeout time.Duration) error {
	return waiter.WaitFor(ctx, waiter.Options{
		Interval: pollInterval(),
		Timeout:  timeout,
		Refresh: func() (string, bool, error) {
			_, err := cl.GetInstance(ctx, instanceID)
			if err != nil {
				if client.IsNotFound(err) {
					return "deleted", true, nil
				}
				return "", false, err
			}
			return "deleting", false, nil
		},
	})
}
