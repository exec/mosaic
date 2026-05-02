//go:build linux

package platform

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStartSecondInstanceListener_DispatchesArgs(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	uniqueId := "test-" + t.Name()

	var (
		mu       sync.Mutex
		received [][]string
	)
	if err := StartSecondInstanceListener(uniqueId, func(args []string) {
		mu.Lock()
		received = append(received, append([]string{}, args...))
		mu.Unlock()
	}); err != nil {
		t.Fatalf("StartSecondInstanceListener: %v", err)
	}
	t.Cleanup(CleanupSingleInstance)

	// Simulate a second-instance launch by hijacking os.Args and calling
	// EarlyForwardLaunchArgs. Must restore os.Args after.
	origArgs := os.Args
	os.Args = []string{"mosaic", "/tmp/example.torrent", "magnet:?xt=urn:btih:abc"}
	t.Cleanup(func() { os.Args = origArgs })

	if !EarlyForwardLaunchArgs(uniqueId) {
		t.Fatal("EarlyForwardLaunchArgs returned false (expected true — listener was bound)")
	}

	// The listener handles the connection in a goroutine; poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(received)
		mu.Unlock()
		if got == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 dispatched call, got %d (received=%v)", len(received), received)
	}
	want := []string{"/tmp/example.torrent", "magnet:?xt=urn:btih:abc"}
	if len(received[0]) != len(want) {
		t.Fatalf("dispatched arg count mismatch: got %v, want %v", received[0], want)
	}
	for i, a := range want {
		if received[0][i] != a {
			t.Errorf("arg[%d]: got %q, want %q", i, received[0][i], a)
		}
	}
}

func TestEarlyForwardLaunchArgs_NoArgsReturnsFalse(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	origArgs := os.Args
	os.Args = []string{"mosaic"}
	t.Cleanup(func() { os.Args = origArgs })

	if EarlyForwardLaunchArgs("test-noargs") {
		t.Fatal("EarlyForwardLaunchArgs returned true with no args; expected false to fall through to normal flow")
	}
}

func TestEarlyForwardLaunchArgs_NoListenerReturnsFalse(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	origArgs := os.Args
	os.Args = []string{"mosaic", "/tmp/foo.torrent"}
	t.Cleanup(func() { os.Args = origArgs })

	if EarlyForwardLaunchArgs("test-no-listener") {
		t.Fatal("EarlyForwardLaunchArgs returned true with no listener; expected false (caller becomes first instance)")
	}
}

func TestBindSingleInstanceSocket_StaleSocketRecovered(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "stale.sock")
	// Create a stale socket file: bind, close listener (without unlinking).
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("seed listen: %v", err)
	}
	// Close the underlying socket but leave the inode behind to mimic a
	// crashed-without-cleanup first instance. net.UnixListener.Close
	// removes the socket file by default; we explicitly avoid that.
	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("stale socket file should still exist: %v", err)
	}

	listener, err := bindSingleInstanceSocket(socketPath)
	if err != nil {
		t.Fatalf("bindSingleInstanceSocket should recover from a stale socket: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
}
