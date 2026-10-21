package engine

import (
	"bytes"
	"context"
	"crypto/sha1"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	anacrolix_types "github.com/anacrolix/torrent/types"
	"github.com/stretchr/testify/require"
)

func testTorrent(t *testing.T) (*metainfo.MetaInfo, []byte) {
	t.Helper()
	piece := sha1.Sum([]byte("x"))
	info, err := bencode.Marshal(metainfo.Info{
		Name: "payload", Length: 1, PieceLength: 16384, Pieces: piece[:],
	})
	require.NoError(t, err)
	mi := &metainfo.MetaInfo{InfoBytes: info}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return mi, buf.Bytes()
}

func testAnacrolixBackend(t *testing.T) *AnacrolixBackend {
	t.Helper()
	be, err := NewAnacrolixBackend(AnacrolixConfig{DataDir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, be.Close()) })
	return be
}

func TestMagnetMetadataOutlivesAddRequest(t *testing.T) {
	be := testAnacrolixBackend(t)
	mi, _ := testTorrent(t)
	ctx, cancel := context.WithCancel(context.Background())
	id, err := be.AddMagnet(ctx, "magnet:?xt=urn:btih:"+mi.HashInfoBytes().HexString(), t.TempDir())
	require.NoError(t, err)
	cancel()
	tor, ok := be.find(id)
	require.True(t, ok)
	// Metadata normally arrives well after the HTTP handler has returned.
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, tor.SetInfoBytes(mi.InfoBytes))
	require.Eventually(t, func() bool {
		return tor.Files()[0].Priority() == anacrolix_types.PiecePriorityNormal
	}, time.Second, 10*time.Millisecond)
}

func TestDuplicateAddPreservesStorageAndPriorities(t *testing.T) {
	be := testAnacrolixBackend(t)
	mi, blob := testTorrent(t)
	ctx := context.Background()
	original := t.TempDir()
	id, err := be.AddFile(ctx, blob, original)
	require.NoError(t, err)
	tor, ok := be.find(id)
	require.True(t, ok)
	require.Eventually(t, func() bool {
		return tor.Files()[0].Priority() == anacrolix_types.PiecePriorityNormal
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, be.SetFilePriorities(id, map[int]Priority{0: PrioritySkip}))
	for _, source := range []string{"file", "magnet"} {
		t.Run(source, func(t *testing.T) {
			var duplicate TorrentID
			if source == "file" {
				duplicate, err = be.AddFile(ctx, blob, t.TempDir())
			} else {
				duplicate, err = be.AddMagnet(ctx, "magnet:?xt=urn:btih:"+mi.HashInfoBytes().HexString(), t.TempDir())
			}
			require.NoError(t, err)
			require.Equal(t, id, duplicate)
			require.Equal(t, original, be.saveDirFor(id))
			require.Never(t, func() bool {
				return tor.Files()[0].Priority() != anacrolix_types.PiecePriorityNone
			}, 50*time.Millisecond, 10*time.Millisecond)
		})
	}
}

func TestPendingMagnetCanPauseAndShutdown(t *testing.T) {
	be := testAnacrolixBackend(t)
	id, err := be.AddMagnet(context.Background(), "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, be.Pause(id))
	// Both the regular checkpoint and the shutdown checkpoint see no metadata.
	tor, ok := be.find(id)
	require.True(t, ok)
	require.NotPanics(t, func() { be.saveSnapshotIfComplete(id, tor) })
}
