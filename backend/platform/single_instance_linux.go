//go:build linux

package platform

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// EarlyForwardLaunchArgs runs a Mosaic-owned single-instance check BEFORE
// any heavy/exclusive init (anacrolix port bind, SQLite DB open). Returns
// true iff a running Mosaic was detected and os.Args[1:] was forwarded over
// our Unix socket — the caller MUST exit immediately when this returns
// true, before touching any process-exclusive resource.
//
// Why we don't trust Wails's Linux SingleInstanceLock:
//
// Wails uses a session-bus D-Bus name + RequestName/Export pattern. In
// practice it silently fails on a number of common setups (Wayland +
// session bus quirks, flatpak/podman sandboxes, custom .desktop launchers
// that drop XDG_RUNTIME_DIR, plus its non-returning error paths swallow
// every failure). Symptom: clicking a .torrent while Mosaic is running
// spawns a fresh broken instance whose port-bind fails silently and the
// user sees nothing happen.
//
// We sidestep all of that with a Unix domain socket at
// $XDG_RUNTIME_DIR/mosaic-singleinstance-<uniqueId>.sock (falling back
// to /tmp keyed on UID). The socket only exists on the running instance;
// connectivity is the lock. If we can connect, we're a second instance —
// we send JSON-encoded args and exit. If we can't, we're first.

const (
	// connectTimeout is how long the second-instance side will wait when
	// dialing the socket. Tight bound — if a real first instance exists,
	// the kernel accepts the connection immediately. A stale socket file
	// (owner crashed without unlink) returns ECONNREFUSED instantly. We
	// don't need a long timeout here.
	connectTimeout = 500 * time.Millisecond
	// writeTimeout caps the JSON push to a peer that's accepted but not
	// reading (deadlocked first instance). A few hundred ms is plenty for
	// a sub-kilobyte payload.
	writeTimeout = 1 * time.Second
)

type secondInstancePayload struct {
	Args             []string `json:"args"`
	WorkingDirectory string   `json:"working_directory"`
}

// EarlyForwardLaunchArgs implements the second-instance side of our Linux
// single-instance contract. See the package-level doc above.
func EarlyForwardLaunchArgs(uniqueId string) bool {
	if len(os.Args) <= 1 {
		// No args to forward. The launcher probably just opened Mosaic
		// without a torrent path — let the normal flow run and Wails will
		// handle "raise the running window" via its own SingleInstanceLock
		// (which works for the no-args case since it doesn't need to
		// transmit anything).
		return false
	}

	socketPath := singleInstanceSocketPath(uniqueId)
	conn, err := net.DialTimeout("unix", socketPath, connectTimeout)
	if err != nil {
		// No running instance, OR the socket file is stale (owner crashed
		// without unlink). Either way, the caller should proceed as the
		// first instance; StartSecondInstanceListener will clean up any
		// stale socket file at bind time.
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
	// Half-close write so the peer sees EOF and processes the message.
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	// Best-effort wait for the peer to ack by closing — caps at writeTimeout.
	_ = conn.SetReadDeadline(time.Now().Add(writeTimeout))
	_, _ = io.Copy(io.Discard, conn)
	return true
}

// StartSecondInstanceListener binds the single-instance Unix socket and
// dispatches incoming args to onArgs in a goroutine. Safe to call exactly
// once on the first instance after EarlyForwardLaunchArgs returned false.
//
// The listener uses a buffered channel + a single consumer goroutine so a
// burst of second-instance launches won't block accept() and won't drop
// args. onArgs may safely run synchronously; the consumer dispatches them
// in its own goroutine.
func StartSecondInstanceListener(uniqueId string, onArgs func(args []string)) error {
	if onArgs == nil {
		return errors.New("StartSecondInstanceListener: onArgs is nil")
	}
	socketPath := singleInstanceSocketPath(uniqueId)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	// When XDG_RUNTIME_DIR is unset, the socket dir is the world-writable
	// /tmp fallback (/tmp/mosaic-<uid>) — MkdirAll happily accepts a
	// pre-existing directory, which another local user could have planted
	// (symlink, wrong owner, loose mode) to hijack or eavesdrop on the
	// socket. Verify before binding. XDG_RUNTIME_DIR itself is created by
	// systemd-logind with the right ownership, so it's left alone.
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		if err := verifyPrivateDir(filepath.Dir(socketPath)); err != nil {
			return fmt.Errorf("socket dir %s: %w", filepath.Dir(socketPath), err)
		}
	}

	listener, err := bindSingleInstanceSocket(socketPath)
	if err != nil {
		return fmt.Errorf("bind single-instance socket %s: %w", socketPath, err)
	}

	pending := make(chan []string, 16)
	go acceptSecondInstances(listener, pending)
	go dispatchSecondInstances(pending, onArgs)

	// Best-effort: remember the socket path so CleanupSingleInstance can
	// unlink it on normal exit. If we crash, the next first-instance bind
	// recovers the stale file via the connect-probe in
	// bindSingleInstanceSocket.
	cleanupMu.Lock()
	registeredCleanupSocket = socketPath
	cleanupMu.Unlock()
	return nil
}

// CleanupSingleInstance unlinks the socket file. Call from your shutdown
// path (or rely on the bind-time stale check on next launch).
func CleanupSingleInstance() {
	cleanupMu.Lock()
	path := registeredCleanupSocket
	registeredCleanupSocket = ""
	cleanupMu.Unlock()
	if path != "" {
		_ = os.Remove(path)
	}
}

var (
	cleanupMu               sync.Mutex
	registeredCleanupSocket string
)

// verifyPrivateDir checks that path is a real directory (not a symlink)
// owned by the current user with no group/other permission bits. Guards the
// /tmp fallback socket dir against a pre-planted attacker-owned path.
func verifyPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory (mode %v) — possible squatting, refusing to use it", info.Mode())
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot determine directory owner")
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("owned by uid %d, not us (uid %d) — refusing to use it", st.Uid, os.Getuid())
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("mode %#o is accessible to other users (want 0700) — refusing to use it", perm)
	}
	return nil
}

// bindSingleInstanceSocket binds the socket, recovering from a stale socket
// file left by a crashed previous owner. Returns the listener.
//
// The probe → remove → re-bind sequence is raced when two instances start
// simultaneously: both can probe-fail the same stale socket, both Remove,
// and the bind loser would give up even though the winner is now live (or
// the path is simply free again). Loop a few times so the loser re-probes —
// on the next pass it either connects to the winner (clean "owned by other
// instance" error) or binds the freed path itself.
func bindSingleInstanceSocket(socketPath string) (net.Listener, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		listener, err := net.Listen("unix", socketPath)
		if err == nil {
			return listener, nil
		}
		// EADDRINUSE: someone bound this path. Probe by connecting — if we can,
		// a real first instance exists (this code path means EarlyForwardLaunchArgs
		// missed it, e.g. no args were passed). If we can't, the socket is stale.
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
		lastErr = err
		probe, probeErr := net.DialTimeout("unix", socketPath, connectTimeout)
		if probeErr == nil {
			_ = probe.Close()
			// A live owner exists. The caller can still run as a "logical first
			// instance" of its own UI, but we won't be able to receive forwarded
			// args. Surface the conflict so the caller can decide.
			return nil, fmt.Errorf("another mosaic instance owns %s", socketPath)
		}
		// Stale socket — remove and loop back to retry the bind. A concurrent
		// starter may have removed it first; that's fine.
		if rmErr := os.Remove(socketPath); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("remove stale socket %s: %w", socketPath, rmErr)
		}
	}
	return nil, fmt.Errorf("gave up after repeated bind races: %w", lastErr)
}

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
	// Cap payload at 64 KiB. Real-world args are tiny; this guards against
	// a wedged/hostile peer flooding us.
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
	// Non-blocking send — if the consumer is wedged, we drop. The consumer
	// goroutine should never wedge because it spawns its callback in a sub-
	// goroutine. 16 buffer slots is plenty for human-driven launches.
	select {
	case out <- payload.Args:
	default:
	}
}

func dispatchSecondInstances(in <-chan []string, onArgs func(args []string)) {
	for args := range in {
		// Spawn each invocation so a slow onArgs (waiting on a context, etc.)
		// can't backpressure the queue.
		go onArgs(args)
	}
}

// singleInstanceSocketPath returns the per-user socket path. Prefers
// XDG_RUNTIME_DIR (correctly cleaned up by systemd-logind on logout);
// falls back to /tmp keyed on UID so two users on the same host don't
// collide.
//
// The uniqueId is collapsed to an 8-char hash slug because Linux Unix
// socket paths are capped at 108 chars (sun_path); a verbose uniqueId
// plus a long XDG_RUNTIME_DIR (e.g. test temp dirs) easily blows past
// that and net.Listen returns "bind: invalid argument".
func singleInstanceSocketPath(uniqueId string) string {
	sum := sha1.Sum([]byte(uniqueId))
	slug := hex.EncodeToString(sum[:4])
	name := "mosaic-" + slug + ".sock"
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, name)
	}
	uid := strconv.Itoa(os.Getuid())
	return filepath.Join("/tmp", "mosaic-"+uid, name)
}
