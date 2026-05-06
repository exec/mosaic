package engine

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireBackendHostable spins up a bare engine on an OS-picked port to
// make sure the test sandbox can satisfy anacrolix's listenAll across
// every network it tries (tcp4 / tcp6 / udp4 / udp6). In CI containers
// without IPv6, anacrolix's "subsequent listen" on tcp6 returns
// EAFNOSUPPORT, which its isUnsupportedNetworkError doesn't classify as
// skippable — so NewClient itself errors out before we can exercise the
// fallback. Skip in those environments rather than fail noisily on
// something orthogonal to what we're testing.
func requireBackendHostable(t *testing.T) {
	t.Helper()
	be, err := NewAnacrolixBackend(AnacrolixConfig{
		DataDir:    t.TempDir(),
		ListenPort: 0,
		EnableDHT:  false,
	})
	if err != nil {
		t.Skipf("environment can't host a vanilla anacrolix client (likely no IPv6 in sandbox): %v", err)
	}
	_ = be.Close()
}

// TestNewAnacrolixBackend_FallsBackOnPortInUse covers the "another
// BitTorrent client (qBittorrent / Deluge) is already on 6881" scenario
// that pre-fix log.Fatal'd Mosaic at startup. We claim a port from the
// kernel ourselves, hand the same port to the engine, and assert the
// engine still constructed — silently fallen back to an OS-picked
// ephemeral that ListenPort() now surfaces.
func TestNewAnacrolixBackend_FallsBackOnPortInUse(t *testing.T) {
	requireBackendHostable(t)

	// Hold both TCP and UDP on the same port so anacrolix's listenAll
	// can't claim either side. Bind 127.0.0.1:0 first so the OS gives us
	// an unused port, then pass that number to the engine.
	tcpL, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer tcpL.Close()
	taken := tcpL.Addr().(*net.TCPAddr).Port

	udpL, err := net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(taken))
	require.NoError(t, err)
	defer udpL.Close()

	dataDir := t.TempDir()
	be, err := NewAnacrolixBackend(AnacrolixConfig{
		DataDir:    dataDir,
		ListenPort: taken,
		EnableDHT:  false, // keep the test offline / quiet
	})
	require.NoError(t, err, "engine should fall back to a random port instead of failing")
	t.Cleanup(func() { _ = be.Close() })

	got := be.ListenPort()
	require.NotEqual(t, taken, got, "fallback should have picked a different port than the in-use one")
	require.Greater(t, got, 0, "fallback port should be a real OS-assigned ephemeral")
}

// TestNewAnacrolixBackend_HonorsPortWhenFree confirms the fallback only
// kicks in on a real bind failure. With a free port the configured
// number is what ends up bound, no surprise OS pick.
func TestNewAnacrolixBackend_HonorsPortWhenFree(t *testing.T) {
	requireBackendHostable(t)

	// Get a port nobody else has, then immediately release it so the
	// engine can claim it on the next syscall.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	be, err := NewAnacrolixBackend(AnacrolixConfig{
		DataDir:    t.TempDir(),
		ListenPort: port,
		EnableDHT:  false,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = be.Close() })

	require.Equal(t, port, be.ListenPort(), "engine should bind the configured port when it's free")
}

func TestIsAddrInUseErr(t *testing.T) {
	require.False(t, isAddrInUseErr(nil))
	require.False(t, isAddrInUseErr(errors.New("connection refused")))

	require.True(t, isAddrInUseErr(syscall.EADDRINUSE))
	// Wrapped form (what anacrolix / net.OpError produce in real life).
	wrapped := &net.OpError{Op: "bind", Err: &net.AddrError{Err: "address already in use"}}
	require.True(t, isAddrInUseErr(wrapped))
}
