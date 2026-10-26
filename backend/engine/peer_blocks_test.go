package engine

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/stretchr/testify/require"
)

func TestManualPeerBlocksAreIndependentOfDownloadedList(t *testing.T) {
	p := &ipBlocklistProxy{}
	list, err := iplist.NewFromReader(strings.NewReader("test:198.51.100.1-198.51.100.2\n"))
	require.NoError(t, err)
	p.inner.Store(list)
	blocked := map[string]struct{}{"192.0.2.1": {}, "2001:db8::1": {}}
	p.blocked.Store(&blocked)
	require.Equal(t, 3, p.NumRanges())
	for _, ip := range []string{"192.0.2.1", "::ffff:192.0.2.1", "2001:db8::1", "198.51.100.2"} {
		_, ok := p.Lookup(net.ParseIP(ip))
		require.True(t, ok, ip)
	}
	_, ok := p.Lookup(net.ParseIP("192.0.2.2"))
	require.False(t, ok)
	p.inner.Store(nil)
	_, ok = p.Lookup(net.ParseIP("2001:db8::1"))
	require.True(t, ok, "disabling downloaded blocklist must preserve manual blocks")
	p.inner.Store(list)
	empty := map[string]struct{}{}
	p.blocked.Store(&empty)
	_, ok = p.Lookup(net.ParseIP("192.0.2.1"))
	require.False(t, ok)
	_, ok = p.Lookup(net.ParseIP("198.51.100.1"))
	require.True(t, ok, "unblocking must preserve downloaded blocklist")
}

func TestBlockingDisconnectsConnectedPeer(t *testing.T) {
	proxy := &ipBlocklistProxy{}
	newClient := func() *torrent.Client {
		cfg := torrent.NewDefaultClientConfig()
		cfg.DataDir = t.TempDir()
		cfg.ListenPort = 0
		cfg.DisableUTP = true
		cfg.DisableIPv6 = true
		cfg.NoDHT = true
		cfg.NoDefaultPortForwarding = true
		cfg.IPBlocklist = proxy
		client, err := torrent.NewClient(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { client.Close() })
		return client
	}
	left, right := newClient(), newClient()
	mi, _ := testTorrent(t)
	tor, err := left.AddTorrent(mi)
	require.NoError(t, err)
	remote, err := right.AddTorrent(mi)
	require.NoError(t, err)
	tor.DownloadAll()
	remote.DownloadAll()
	var addr *net.TCPAddr
	for _, a := range right.ListenAddrs() {
		if tcp, ok := a.(*net.TCPAddr); ok {
			addr = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: tcp.Port}
			break
		}
	}
	require.NotNil(t, addr)
	connect := func() { tor.AddPeers([]torrent.PeerInfo{{Addr: addr}}) }
	connect()
	require.Eventually(t, func() bool { return len(tor.PeerConns()) > 0 }, 3*time.Second, 10*time.Millisecond)
	backend := &AnacrolixBackend{client: left, ipBlock: proxy}
	backend.SetBlockedPeers([]string{"127.0.0.1"})
	require.Eventually(t, func() bool { return len(tor.PeerConns()) == 0 }, time.Second, 10*time.Millisecond)
	connect()
	require.Never(t, func() bool { return len(tor.PeerConns()) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
}
