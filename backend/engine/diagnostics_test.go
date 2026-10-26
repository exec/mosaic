package engine

import (
	"bytes"
	"context"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLiveDiagnosticsAndExportMetadata(t *testing.T) {
	b := testAnacrolixBackend(t)
	observed := b.ConnectionDiagnostics()
	require.True(t, observed.Available)
	require.NotEmpty(t, observed.Listeners)
	require.Zero(t, observed.DHTServers)
	require.Zero(t, observed.InboundPeers)
	require.Zero(t, observed.OutboundPeers)
	original, blob := testTorrent(t)
	id, err := b.Add(context.Background(), AddRequest{TorrentFile: blob, SavePath: t.TempDir(), Paused: true})
	require.NoError(t, err)
	exported, err := b.ExportTorrent(id)
	require.NoError(t, err)
	parsed, err := metainfo.Load(bytes.NewReader(exported))
	require.NoError(t, err)
	require.Equal(t, original.HashInfoBytes(), parsed.HashInfoBytes())
	missing, err := b.AddMagnet(context.Background(), "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", t.TempDir())
	require.NoError(t, err)
	_, err = b.ExportTorrent(missing)
	require.ErrorContains(t, err, "metadata is unavailable")
}
