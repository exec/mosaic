package engine

import (
	"net"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/iplist"
	"github.com/stretchr/testify/require"
)

func TestIPBlocklistProxy_LookupAndSwap(t *testing.T) {
	p := &ipBlocklistProxy{}

	require.Equal(t, 0, p.NumRanges())
	_, ok := p.Lookup(net.ParseIP("10.0.0.1"))
	require.False(t, ok)

	list, err := iplist.NewFromReader(strings.NewReader("evil corp:10.0.0.0-10.0.0.255\n"))
	require.NoError(t, err)
	p.inner.Store(list)

	require.Equal(t, 1, p.NumRanges())

	r, ok := p.Lookup(net.ParseIP("10.0.0.42"))
	require.True(t, ok)
	require.Equal(t, "evil corp", r.Description)

	_, ok = p.Lookup(net.ParseIP("10.0.1.1"))
	require.False(t, ok)

	p.inner.Store(nil)
	_, ok = p.Lookup(net.ParseIP("10.0.0.42"))
	require.False(t, ok)
	require.Equal(t, 0, p.NumRanges())
}
