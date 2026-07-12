// Package adb provides a serialized wrapper around the `adb` command-line tool.
//
// Concurrent `adb shell` invocations against a single device intermittently
// fail with "request send failed: Permission denied". To avoid this, every adb
// command issued through a Client is guarded by a single mutex, so commands
// execute strictly one at a time. Each command runs under a context timeout so
// a hung device can never hang a scrape.
package adb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Client is a serialized executor for a single adb device. All exported methods
// acquire the same mutex, guaranteeing that no two adb commands ever run
// concurrently for this client.
type Client struct {
	path    string
	address string
	timeout time.Duration

	mu sync.Mutex
}

// NewClient returns a Client that targets the device at address via the adb
// binary at path. Each command is bounded by timeout.
func NewClient(path, address string, timeout time.Duration) *Client {
	return &Client{
		path:    path,
		address: address,
		timeout: timeout,
	}
}

// Address returns the host:port this client targets.
func (c *Client) Address() string { return c.address }

// run executes `adb <args...>` under the client's mutex and per-command
// timeout, returning trimmed stdout. It reports an error on non-zero exit or on
// a timeout. The caller must NOT hold the mutex.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runLocked(ctx, args...)
}

// runLocked is run's body assuming the mutex is already held.
func (c *Client) runLocked(ctx context.Context, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, c.path, args...)
	// WaitDelay bounds how long Run may block after the context is cancelled
	// (and the process killed) waiting for I/O to drain. Without it, a killed
	// process that left a child holding the stdout pipe could hang the scrape.
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if cctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("adb %s: timed out after %s", strings.Join(args, " "), c.timeout)
	}
	if err != nil {
		return "", fmt.Errorf("adb %s: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

// State returns the trimmed output of `adb -s <addr> get-state` (e.g. "device",
// "offline"). A non-nil error means adb itself failed to run.
func (c *Client) State(ctx context.Context) (string, error) {
	return c.run(ctx, "-s", c.address, "get-state")
}

// Connect issues a best-effort `adb connect <addr>`. The returned string is the
// adb message ("connected to ..." / "already connected ...").
func (c *Client) Connect(ctx context.Context) (string, error) {
	return c.run(ctx, "connect", c.address)
}

// RunShell executes `adb -s <addr> shell <cmd>` and returns trimmed stdout. The
// command string is passed as a single argument so adb's shell service receives
// it verbatim (pipelines and quoting are the caller's responsibility). An empty
// result with no error is possible and is the caller's to interpret.
func (c *Client) RunShell(ctx context.Context, cmd string) (string, error) {
	return c.run(ctx, "-s", c.address, "shell", cmd)
}

// EnsureConnected verifies the device is in the "device" state, attempting a
// single reconnect if it is not. It returns true when the device is reachable.
// All underlying adb calls are serialized under the client mutex, held for the
// whole check so the get-state / connect / get-state sequence is atomic with
// respect to other callers.
func (c *Client) EnsureConnected(ctx context.Context) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, err := c.runLocked(ctx, "-s", c.address, "get-state")
	if err == nil && state == "device" {
		return true, nil
	}

	// One best-effort reconnect attempt, then re-check.
	if _, cErr := c.runLocked(ctx, "connect", c.address); cErr != nil {
		return false, fmt.Errorf("reconnect failed: %w", cErr)
	}

	state, err = c.runLocked(ctx, "-s", c.address, "get-state")
	if err == nil && state == "device" {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get-state after reconnect: %w", err)
	}
	return false, fmt.Errorf("device not ready: state=%q", state)
}
