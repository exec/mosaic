package engine

import (
	"bytes"
	"context"
	"errors"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	anacrolix_types "github.com/anacrolix/torrent/types"
	"github.com/stretchr/testify/require"
)

func waitVerified(t *testing.T, b *AnacrolixBackend, id TorrentID) {
	t.Helper()
	require.Eventually(t, func() bool { s, e := b.Snapshot(id); return e == nil && s.MetadataReady && !s.Verifying }, 3*time.Second, 10*time.Millisecond)
}

func TestPreparedMagnetGetsMetadataWithoutPayload(t *testing.T) {
	b := testAnacrolixBackend(t)
	mi, _ := testTorrent(t)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.ListenPort = 0
	cfg.DisableUTP = true
	cfg.DisableIPv6 = true
	cfg.NoDHT = true
	cfg.NoDefaultPortForwarding = true
	cfg.Seed = true
	require.NoError(t, os.WriteFile(filepath.Join(cfg.DataDir, "payload"), []byte("x"), 0600))
	seed, err := torrent.NewClient(cfg)
	require.NoError(t, err)
	defer seed.Close()
	source, err := seed.AddTorrent(mi)
	require.NoError(t, err)
	source.VerifyData()
	source.DownloadAll()
	id, err := b.Add(context.Background(), AddRequest{Magnet: "magnet:?xt=urn:btih:" + mi.HashInfoBytes().HexString(), SavePath: t.TempDir(), Paused: true})
	require.NoError(t, err)
	target, ok := b.find(id)
	require.True(t, ok)
	var addr *net.TCPAddr
	for _, a := range seed.ListenAddrs() {
		if tcp, ok := a.(*net.TCPAddr); ok {
			addr = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: tcp.Port}
			break
		}
	}
	require.NotNil(t, addr)
	connect := func() { target.AddPeers([]torrent.PeerInfo{{Addr: addr}}) }
	connect()
	waitVerified(t, b, id)
	require.NoError(t, b.SetTorrentRateLimits(id, 0, 0))
	connect()
	require.Never(t, func() bool { stats := target.Stats(); return stats.BytesReadData.Int64() > 0 }, 250*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, b.Resume(id))
	connect()
	require.Eventually(t, func() bool { return target.BytesCompleted() == 1 }, 5*time.Second, 10*time.Millisecond)
}
func TestFileChoicesSurviveVerificationAndSequentialToggle(t *testing.T) {
	b := testAnacrolixBackend(t)
	_, blob := testTorrent(t)
	id, err := b.Add(context.Background(), AddRequest{TorrentFile: blob, SavePath: t.TempDir(), Paused: true, Priorities: map[int]Priority{0: PrioritySkip}})
	require.NoError(t, err)
	waitVerified(t, b, id)
	tor, _ := b.find(id)
	b.SetSequential(id, true)
	b.SetSequential(id, false)
	require.Equal(t, anacrolix_types.PiecePriorityNone, tor.Files()[0].Priority())
	require.Equal(t, anacrolix_types.PiecePriorityNone, tor.Piece(0).State().Priority)
	require.NoError(t, b.Recheck(id))
	waitVerified(t, b, id)
	require.Equal(t, anacrolix_types.PiecePriorityNone, tor.Files()[0].Priority())
	require.Error(t, b.SetFilePriorities(id, map[int]Priority{-1: PriorityHigh}))
}
func TestMoveAndLocatePausedData(t *testing.T) {
	b := testAnacrolixBackend(t)
	_, blob := testTorrent(t)
	ctx := context.Background()
	old := t.TempDir()
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(old, "payload"), []byte("x"), 0600))
	id, err := b.Add(ctx, AddRequest{TorrentFile: blob, SavePath: old, Paused: true, Priorities: map[int]Priority{0: PriorityHigh}})
	require.NoError(t, err)
	waitVerified(t, b, id)
	require.NoError(t, b.Relocate(ctx, id, dest, true))
	waitVerified(t, b, id)
	content, err := os.ReadFile(filepath.Join(dest, "payload"))
	require.NoError(t, err)
	require.Equal(t, "x", string(content))
	_, err = os.Stat(filepath.Join(old, "payload"))
	require.True(t, os.IsNotExist(err))
	s, err := b.Snapshot(id)
	require.NoError(t, err)
	require.True(t, s.Paused)
	require.True(t, s.Completed)
	tor, _ := b.find(id)
	require.Equal(t, anacrolix_types.PiecePriorityHigh, tor.Files()[0].Priority())
	locate := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(locate, "payload"), []byte("x"), 0600))
	require.NoError(t, b.Relocate(ctx, id, locate, false))
	waitVerified(t, b, id)
	_, err = os.Stat(filepath.Join(dest, "payload"))
	require.NoError(t, err, "locate must keep the original copy")
	require.NoError(t, b.Resume(id))
	require.ErrorContains(t, b.Relocate(ctx, id, t.TempDir(), true), "pause")
}
func TestMoveDoesNotOverwriteAndCopyRejectsLinks(t *testing.T) {
	b := testAnacrolixBackend(t)
	_, blob := testTorrent(t)
	ctx := context.Background()
	old := t.TempDir()
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(old, "payload.part"), []byte("z"), 0600))
	id, err := b.Add(ctx, AddRequest{TorrentFile: blob, SavePath: old, Paused: true})
	require.NoError(t, err)
	waitVerified(t, b, id)
	require.NoError(t, os.WriteFile(filepath.Join(dest, "payload.part"), []byte("keep"), 0600))
	require.ErrorContains(t, b.Relocate(ctx, id, dest, true), "destination already")
	content, err := os.ReadFile(filepath.Join(dest, "payload.part"))
	require.NoError(t, err)
	require.Equal(t, "keep", string(content))
	require.Equal(t, old, b.saveDirFor(id))
	require.NoError(t, os.Remove(filepath.Join(dest, "payload.part")))
	require.NoError(t, b.Relocate(ctx, id, dest, true))
	waitVerified(t, b, id)
	content, err = os.ReadFile(filepath.Join(dest, "payload.part"))
	require.NoError(t, err)
	require.Equal(t, "z", string(content))
	root := t.TempDir()
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(dest, link))
	require.Error(t, copyContent(ctx, link, filepath.Join(root, "copy")))
	dir := filepath.Join(root, "dir")
	require.NoError(t, os.Mkdir(dir, 0700))
	require.NoError(t, os.Symlink(dest, filepath.Join(dir, "link")))
	failed := filepath.Join(root, "failed")
	require.Error(t, copyContent(ctx, dir, failed))
	_, err = os.Stat(failed)
	require.True(t, os.IsNotExist(err), "failed copy cleans only its own destination")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, copyContent(canceled, filepath.Join(dest, "payload.part"), filepath.Join(root, "cancel")), context.Canceled)
}

type errorBackend struct {
	*FakeBackend
	handler func(TorrentID, error)
}

func (b *errorBackend) SetErrorHandler(f func(TorrentID, error)) { b.handler = f }
func TestErrorHistorySurvivesResumeUntilRemoval(t *testing.T) {
	b := &errorBackend{FakeBackend: NewFakeBackend()}
	e := NewEngine(b, time.Second)
	defer e.Close()
	id, err := e.AddMagnet(context.Background(), "magnet:test", t.TempDir())
	require.NoError(t, err)
	b.handler(id, errors.New("disk full"))
	d, err := e.DetailedSnapshot(id, DetailScope{})
	require.NoError(t, err)
	require.Equal(t, "disk full", d.Snapshot.LastError)
	require.False(t, d.Snapshot.ErrorAt.IsZero())
	require.True(t, d.Snapshot.ErrorActive)
	require.NoError(t, e.Resume(id))
	s, err := e.Snapshot(id)
	require.NoError(t, err)
	require.False(t, s.ErrorActive)
	require.Equal(t, "disk full", s.LastError)
	require.NoError(t, e.Remove(id, false))
	require.Empty(t, e.errors)
}

func TestRemovalKeepsOrDeletesPartialFiles(t *testing.T) {
	for _, removeFiles := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "delete"}[removeFiles], func(t *testing.T) {
			b := testAnacrolixBackend(t)
			_, blob := testTorrent(t)
			path := t.TempDir()
			partial := filepath.Join(path, "payload.part")
			require.NoError(t, os.WriteFile(partial, []byte("z"), 0600))
			id, err := b.Add(context.Background(), AddRequest{TorrentFile: blob, SavePath: path, Paused: true})
			require.NoError(t, err)
			waitVerified(t, b, id)
			require.NoError(t, b.Remove(id, removeFiles))
			_, err = os.Stat(partial)
			if removeFiles {
				require.True(t, os.IsNotExist(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLocateMismatchedCompleteDataKeepsOriginalAndExplainsMissingFiles(t *testing.T) {
	b := testAnacrolixBackend(t)
	_, blob := testTorrent(t)
	old := t.TempDir()
	dest := t.TempDir()
	ctx := context.Background()
	require.NoError(t, os.WriteFile(filepath.Join(old, "payload"), []byte("x"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "payload"), []byte("z"), 0600))
	id, err := b.Add(ctx, AddRequest{TorrentFile: blob, SavePath: old, Paused: true})
	require.NoError(t, err)
	waitVerified(t, b, id)
	before, err := b.Snapshot(id)
	require.NoError(t, err)
	require.True(t, before.Completed)
	require.NoError(t, b.Relocate(ctx, id, dest, false))
	waitVerified(t, b, id)
	snap, err := b.Snapshot(id)
	require.NoError(t, err)
	require.True(t, snap.Paused)
	require.True(t, snap.FilesMissing)
	tor, _ := b.find(id)
	require.Equal(t, anacrolix_types.PiecePriorityNormal, tor.Files()[0].Priority(), "Resume must be able to request the missing pieces")
	content, err := os.ReadFile(filepath.Join(old, "payload"))
	require.NoError(t, err)
	require.Equal(t, "x", string(content))
}
func TestDirectoryTorrentNeverMovesOrDeletesUnrelatedPartSibling(t *testing.T) {
	b := testAnacrolixBackend(t)
	mi, _ := testTorrent(t)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	info.Length = 0
	info.Files = []metainfo.FileInfo{{Length: 1, Path: []string{"file"}}}
	mi.InfoBytes, err = bencode.Marshal(info)
	require.NoError(t, err)
	var blob bytes.Buffer
	require.NoError(t, mi.Write(&blob))
	old := t.TempDir()
	dest := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(old, "payload"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(old, "payload", "file"), []byte("x"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(old, "payload.part"), []byte("unrelated"), 0600))
	id, err := b.Add(context.Background(), AddRequest{TorrentFile: blob.Bytes(), SavePath: old, Paused: true})
	require.NoError(t, err)
	waitVerified(t, b, id)
	require.NoError(t, b.Relocate(context.Background(), id, dest, true))
	waitVerified(t, b, id)
	content, err := os.ReadFile(filepath.Join(old, "payload.part"))
	require.NoError(t, err)
	require.Equal(t, "unrelated", string(content))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "payload.part"), []byte("keep"), 0600))
	require.NoError(t, b.Remove(id, true))
	content, err = os.ReadFile(filepath.Join(dest, "payload.part"))
	require.NoError(t, err)
	require.Equal(t, "keep", string(content))
}

func TestCancelPreparedVerificationCanImmediatelyReadd(t *testing.T) {
	b := testAnacrolixBackend(t)
	info, err := bencode.Marshal(metainfo.Info{Name: "payload", Length: 1 << 20, PieceLength: 16384, Pieces: make([]byte, 64*20)})
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: info}
	var blob bytes.Buffer
	require.NoError(t, mi.Write(&blob))
	for range 3 {
		id, err := b.Add(context.Background(), AddRequest{TorrentFile: blob.Bytes(), SavePath: t.TempDir(), Paused: true})
		require.NoError(t, err)
		require.NoError(t, b.Remove(id, false))
		require.Empty(t, b.List())
	}
}
