package adb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeFakeADB writes an executable shell script acting as a stand-in adb binary
// and returns its path. The script's behavior is driven by its argument list so
// tests can exercise get-state/connect/shell paths without a real device.
func writeFakeADB(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-adb")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake adb: %v", err)
	}
	return path
}

func TestClientStateAndShell(t *testing.T) {
	// Echo the last argument for shell, "device" for get-state.
	fake := writeFakeADB(t, `
case "$*" in
  *get-state) echo device ;;
  *"shell "*) shift 3; echo "ran: $*" ;;
  *) echo unknown ;;
esac
`)
	c := NewClient(fake, "10.0.0.1:5555", 2*time.Second)

	state, err := c.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state != "device" {
		t.Errorf("State = %q, want device", state)
	}

	out, err := c.RunShell(context.Background(), "cat /proc/uptime")
	if err != nil {
		t.Fatalf("RunShell: %v", err)
	}
	if !strings.Contains(out, "cat /proc/uptime") {
		t.Errorf("RunShell out = %q", out)
	}
}

func TestClientEnsureConnected(t *testing.T) {
	fake := writeFakeADB(t, `
case "$*" in
  *get-state) echo device ;;
  *) echo "ok" ;;
esac
`)
	c := NewClient(fake, "10.0.0.1:5555", 2*time.Second)
	up, err := c.EnsureConnected(context.Background())
	if err != nil || !up {
		t.Fatalf("EnsureConnected = %v, %v; want true, nil", up, err)
	}
}

func TestClientEnsureConnectedReconnectFails(t *testing.T) {
	// get-state never returns "device"; connect is best-effort but device stays down.
	fake := writeFakeADB(t, `
case "$*" in
  *get-state) echo offline ;;
  *connect*) echo "connected" ;;
esac
`)
	c := NewClient(fake, "10.0.0.1:5555", 2*time.Second)
	up, err := c.EnsureConnected(context.Background())
	if up {
		t.Errorf("EnsureConnected up = true, want false")
	}
	if err == nil {
		t.Error("expected error when device stays offline")
	}
}

func TestClientTimeout(t *testing.T) {
	// Sleep longer than the per-command timeout ⇒ DeadlineExceeded error. `exec`
	// replaces the shell with sleep so the killed process is sleep itself (no
	// lingering grandchild holding the stdout pipe).
	fake := writeFakeADB(t, "exec sleep 5\n")
	c := NewClient(fake, "10.0.0.1:5555", 100*time.Millisecond)
	start := time.Now()
	_, err := c.State(context.Background())
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("command did not honor timeout: took %s", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want timeout", err)
	}
}

func TestClientSerializes(t *testing.T) {
	// The fake adb detects overlap: it creates a lockfile on entry and records
	// "OVERLAP" if the lock already exists, then holds it briefly. If the
	// client's mutex serializes execs correctly, no invocation ever sees the
	// lock held, so the log stays empty.
	dir := t.TempDir()
	lock := filepath.Join(dir, "lock")
	logf := filepath.Join(dir, "log")
	fake := writeFakeADB(t, `
LOCK="`+lock+`"
LOG="`+logf+`"
if [ -e "$LOCK" ]; then echo OVERLAP >> "$LOG"; fi
: > "$LOCK"
sleep 0.02
rm -f "$LOCK"
echo device
`)
	c := NewClient(fake, "10.0.0.1:5555", 2*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.State(context.Background())
		}()
	}
	wg.Wait()

	if data, err := os.ReadFile(logf); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		t.Fatalf("adb execs overlapped despite mutex; log:\n%s", data)
	}
}
