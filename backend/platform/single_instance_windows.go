//go:build windows

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
)

// EarlyForwardLaunchArgs runs Mosaic's second-instance check BEFORE any
// heavy/exclusive init (anacrolix port bind, SQLite DB open). Returns
// true iff a running Mosaic was detected and os.Args[1:] was forwarded
// over our named pipe — the caller MUST exit immediately when this
// returns true, before touching any process-exclusive resource.
//
// Why we don't trust Wails's Windows SingleInstanceLock:
//
// Wails creates its second-instance receiver as a *message-only* window
// (CreateWindowEx with HWND_MESSAGE as parent), then tries to find it
// from second instances using FindWindowW. That's the wrong API:
// FindWindowW only enumerates top-level desktop windows and silently
// skips message-only windows. As a result Wails's IPC has been broken
// on Windows since at least v2.10.x — second instances never locate
// the receiver, the SendMessage never fires, and the second process
// continues startup until it crashes on an exclusive resource.
//
// Pre-v0.4.2 we worked around this by using FindWindowExW(HWND_MESSAGE,
// ...) ourselves and forwarding the same WM_COPYDATA wire format Wails
// expects. That was load-bearing on the receiver still being a Wails-
// managed window, which made the IPC fragile across Wails updates and
// across timing differences (Wails creates the message-only window
// late in its init sequence, so a fast second-instance launch could
// miss it). Reports of intermittent failures eventually stopped being
// intermittent.
//
// v0.4.2 replaces both directions with a Mosaic-owned named pipe:
//
//	\\.\pipe\mosaic-singleinstance-<uniqueId>
//
// Named pipes are first-class on Windows, owned by the user's session
// (no cross-user collision), and survive across Wails versions. The
// wire format is the same JSON envelope we use on Linux's Unix-socket
// implementation (see single_instance_linux.go) so the dispatch path
// in main.go is identical on both platforms.

const pipePrefix = `\\.\pipe\mosaic-singleinstance-`

const (
	// connectTimeout caps the second-instance dial. A live first
	// instance accepts immediately; if the dial hasn't connected within
	// this window the listener probably isn't running yet and the
	// caller should proceed as the first instance.
	connectTimeout = 750 * time.Millisecond
	// writeTimeout caps the JSON push to a peer that's accepted but
	// stalled. A few hundred ms is plenty for a sub-kilobyte payload.
	writeTimeout = 1 * time.Second
)

type secondInstancePayload struct {
	Args             []string `json:"args"`
	WorkingDirectory string   `json:"working_directory"`
}

// EarlyForwardLaunchArgs implements the second-instance side. See the
// package-level doc above.
func EarlyForwardLaunchArgs(uniqueId string) bool {
	if len(os.Args) <= 1 {
		// No args to forward; let the normal startup flow run. The
		// running instance won't gain anything from a notification, and
		// the second instance will exit naturally when it discovers the
		// pipe / port already in use.
		return false
	}
	pipeName := pipePrefix + uniqueId

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	conn, err := winio.DialPipeContext(ctx, pipeName)
	if err != nil {
		// No running listener (or the deadline beat us). Caller
		// proceeds as the first instance; StartSecondInstanceListener
		// will create the pipe.
		return false
	}
	defer conn.Close()

	cwd, _ := os.Getwd()
	payload := secondInstancePayload{Args: os.Args[1:], WorkingDirectory: cwd}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if _, err := conn.Write(serialized); err != nil {
		return false
	}
	// Best-effort wait for the peer to ack by closing — caps at
	// writeTimeout. Mirrors the Linux side's CloseWrite + drain pattern.
	_ = conn.SetReadDeadline(time.Now().Add(writeTimeout))
	_, _ = io.Copy(io.Discard, conn)
	return true
}

// StartSecondInstanceListener binds the named pipe and dispatches
// incoming args to onArgs. Safe to call exactly once on the first
// instance after EarlyForwardLaunchArgs returned false.
func StartSecondInstanceListener(uniqueId string, onArgs func(args []string)) error {
	if onArgs == nil {
		return errors.New("StartSecondInstanceListener: onArgs is nil")
	}
	pipeName := pipePrefix + uniqueId

	listener, err := winio.ListenPipe(pipeName, &winio.PipeConfig{
		// Default ACL — owner-only access. Multiple users on the same
		// machine each get their own pipe namespace, so we don't need
		// to widen this.
		InputBufferSize:  64 * 1024,
		OutputBufferSize: 4 * 1024,
	})
	if err != nil {
		return fmt.Errorf("listen pipe %s: %w", pipeName, err)
	}

	pending := make(chan []string, 16)
	go acceptSecondInstances(listener, pending)
	go dispatchSecondInstances(pending, onArgs)

	cleanupMu.Lock()
	registeredCleanupListener = listener
	cleanupMu.Unlock()
	return nil
}

// CleanupSingleInstance closes the listener (releasing the pipe). Safe
// to call multiple times; only the first call has any effect.
func CleanupSingleInstance() {
	cleanupMu.Lock()
	l := registeredCleanupListener
	registeredCleanupListener = nil
	cleanupMu.Unlock()
	if l != nil {
		_ = l.Close()
	}
}

var (
	cleanupMu                 sync.Mutex
	registeredCleanupListener net.Listener
)

func acceptSecondInstances(listener net.Listener, out chan<- []string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Listener closed — clean shutdown path.
			return
		}
		go handleSecondInstanceConn(conn, out)
	}
}

func handleSecondInstanceConn(conn net.Conn, out chan<- []string) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	// Cap payload at 64 KiB. Real-world args are tiny; this guards
	// against a wedged/hostile peer flooding us.
	buf, err := io.ReadAll(io.LimitReader(conn, 64*1024))
	if err != nil || len(buf) == 0 {
		return
	}
	var payload secondInstancePayload
	if err := json.Unmarshal(buf, &payload); err != nil {
		return
	}
	if len(payload.Args) == 0 {
		return
	}
	// Non-blocking send — if the consumer is wedged, drop. The
	// consumer goroutine fans each invocation out to its own goroutine,
	// so wedging is unlikely; the buffer of 16 covers any conceivable
	// human-driven launch burst.
	select {
	case out <- payload.Args:
	default:
	}
}

func dispatchSecondInstances(in <-chan []string, onArgs func(args []string)) {
	for args := range in {
		go onArgs(args)
	}
}
