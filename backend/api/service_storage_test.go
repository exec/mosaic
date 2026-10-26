package api

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

func TestPrioritiesPersistRestoreAndValidateAtomically(t *testing.T) {
	svc, _, _ := newTestServiceWithDB(t)
	ctx := sysCtx()
	id, err := svc.AddMagnet(ctx, "magnet:persist-files", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, svc.SetFilePriorities(ctx, string(id), map[int]string{0: "skip", 1: "high"}))
	require.Error(t, svc.SetFilePriorities(ctx, string(id), map[int]string{0: "normal", 9: "skip"}))
	require.Error(t, svc.SetFilePriorities(ctx, string(id), map[int]string{1: "invalid"}))
	stored, err := svc.torrents.FilePriorities(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, map[int]int{0: 0, 1: 2}, stored)
	fresh := engine.NewEngine(engine.NewFakeBackend(), time.Second)
	defer fresh.Close()
	restored := NewService(fresh, svc.torrents, svc.categories, svc.tags, svc.settings, svc.scheduleRules, svc.feeds, svc.filters, svc.users, svc.access, svc.trackers, nil, t.TempDir())
	require.NoError(t, restored.RestoreOnStartup(ctx))
	detail, err := restored.GetTorrentDetail(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, "skip", detail.Files[0].Priority)
	require.Equal(t, "high", detail.Files[1].Priority)
	require.NoError(t, svc.Remove(ctx, id, false))
	stored, err = svc.torrents.FilePriorities(ctx, string(id))
	require.NoError(t, err)
	require.Empty(t, stored)
}
func TestStorageAndDetailAccess(t *testing.T) {
	svc, _, _ := newTestServiceWithDB(t)
	ctx := sysCtx()
	owner := mkUser(t, svc, UserInput{Username: "owner", Password: "password123", Role: persistence.RoleUser})
	viewer := mkUser(t, svc, UserInput{Username: "viewer", Password: "password123", Role: persistence.RoleUser})
	other := mkUser(t, svc, UserInput{Username: "other", Password: "password123", Role: persistence.RoleUser})
	id, err := svc.AddMagnet(ctx, "magnet:access-files", t.TempDir())
	require.NoError(t, err)
	require.NoError(t, svc.access.Grant(ctx, string(id), owner.ID, persistence.AccessOwner, nil))
	require.NoError(t, svc.access.Grant(ctx, string(id), viewer.ID, persistence.AccessViewer, nil))
	_, err = svc.GetTorrentDetail(asUser(t, svc, viewer.ID), string(id))
	require.NoError(t, err)
	_, err = svc.GetStorage(asUser(t, svc, viewer.ID), string(id))
	require.NoError(t, err)
	_, err = svc.GetTorrentDetail(asUser(t, svc, other.ID), string(id))
	require.ErrorIs(t, err, ErrForbidden)
	require.ErrorIs(t, svc.SetFilePriorities(asUser(t, svc, viewer.ID), string(id), map[int]string{0: "skip"}), ErrForbidden)
	require.ErrorIs(t, svc.RelocateTorrent(asUser(t, svc, viewer.ID), string(id), t.TempDir(), true), ErrForbidden)
	require.ErrorIs(t, svc.SetPreallocate(asUser(t, svc, viewer.ID), true), ErrForbidden)
	require.NoError(t, svc.SetPreallocate(ctx, true))
	storage, err := svc.GetStorage(ctx, string(id))
	require.NoError(t, err)
	require.True(t, storage.Preallocate)
}
func TestRelocationPersistsPath(t *testing.T) {
	svc, _, _ := newTestServiceWithDB(t)
	ctx := sysCtx()
	old := t.TempDir()
	dest := filepath.Join(t.TempDir(), "new")
	id, err := svc.AddMagnet(ctx, "magnet:move", old)
	require.NoError(t, err)
	require.NoError(t, svc.Pause(ctx, id))
	require.NoError(t, svc.RelocateTorrent(ctx, string(id), dest, true))
	rec, err := svc.torrents.Get(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, dest, rec.SavePath)
	require.True(t, rec.Paused)
	d, err := svc.GetTorrentDetail(ctx, string(id))
	require.NoError(t, err)
	require.Equal(t, dest, d.SavePath)
}
func TestPrepareRejectsMissingSourceAndNoAddPermission(t *testing.T) {
	svc, _, _ := newTestServiceWithDB(t)
	_, err := svc.PrepareTorrent(sysCtx(), PrepareTorrentRequest{SavePath: t.TempDir()})
	require.ErrorContains(t, err, "provide one")
	_, err = svc.PrepareTorrent(WithCaller(context.Background(), Caller{}), PrepareTorrentRequest{Magnet: "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", SavePath: t.TempDir()})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestPreparePersistsPausedAndRejectsDuplicateWithoutMutation(t *testing.T) {
	svc, _, _ := newTestServiceWithDB(t)
	b, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{DataDir: t.TempDir()})
	require.NoError(t, err)
	svc.engine = engine.NewEngine(b, time.Second)
	t.Cleanup(func() { require.NoError(t, svc.engine.Close()) })
	piece := sha1.Sum([]byte("x"))
	info, err := bencode.Marshal(metainfo.Info{Name: "payload", Length: 1, PieceLength: 16384, Pieces: piece[:]})
	require.NoError(t, err)
	mi := metainfo.MetaInfo{InfoBytes: info}
	var blob bytes.Buffer
	require.NoError(t, mi.Write(&blob))
	ctx := sysCtx()
	id, err := svc.PrepareTorrent(ctx, PrepareTorrentRequest{TorrentFile: blob.Bytes(), SavePath: t.TempDir()})
	require.NoError(t, err)
	rec, err := svc.torrents.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, rec.Paused)
	require.Equal(t, blob.Bytes(), rec.Metainfo)
	require.Eventually(t, func() bool { d, e := svc.GetTorrentDetail(ctx, id); return e == nil && d.MetadataReady && !d.Verifying }, time.Second, 10*time.Millisecond)
	require.NoError(t, svc.Resume(ctx, engine.TorrentID(id)))
	_, err = svc.PrepareTorrent(ctx, PrepareTorrentRequest{TorrentFile: blob.Bytes(), SavePath: t.TempDir()})
	require.ErrorContains(t, err, "already exists")
	snap, err := svc.engine.Snapshot(engine.TorrentID(id))
	require.NoError(t, err)
	require.False(t, snap.Paused)
}
func TestNativeFilePathOnlyAllowsCompleteContainedFiles(t *testing.T) {
	svc, _, _ := newTestServiceWithDB(t)
	root := t.TempDir()
	ctx := sysCtx()
	id, err := svc.AddMagnet(ctx, "magnet:filepaths", root)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(root, "fake"), 0700))
	path := filepath.Join(root, "fake", "README")
	require.NoError(t, os.WriteFile(path, []byte("readme"), 0600))
	actual, err := svc.TorrentFilePath(ctx, string(id), 1)
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
	_, err = svc.TorrentFilePath(ctx, string(id), 0)
	require.ErrorContains(t, err, "not complete")
	require.NoError(t, os.Remove(path))
	_, err = svc.TorrentFilePath(ctx, string(id), 1)
	require.ErrorContains(t, err, "Locate existing files")
	require.NotContains(t, err.Error(), root)
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("private"), 0600))
	require.NoError(t, os.Symlink(outside, path))
	_, err = svc.TorrentFilePath(ctx, string(id), 1)
	require.ErrorContains(t, err, "escapes")
}

type failedRelocation struct {
	*engine.FakeBackend
	cancel context.CancelFunc
}

func (b *failedRelocation) Relocate(_ context.Context, _ engine.TorrentID, _ string, _ bool) error {
	b.cancel()
	return context.Canceled
}
func TestRelocationFailureRestoresSavedPathDespiteCanceledRequest(t *testing.T) {
	svc, fb, _ := newTestServiceWithDB(t)
	old := t.TempDir()
	id, err := svc.AddMagnet(sysCtx(), "magnet:rollback-move", old)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(sysCtx())
	defer cancel()
	svc.engine = engine.NewEngine(&failedRelocation{FakeBackend: fb, cancel: cancel}, time.Second)
	defer svc.engine.Close()
	require.ErrorIs(t, svc.RelocateTorrent(ctx, string(id), t.TempDir(), true), context.Canceled)
	rec, err := svc.torrents.Get(sysCtx(), string(id))
	require.NoError(t, err)
	require.Equal(t, old, rec.SavePath)
}

func TestNativeFilePathMatchesRealTorrentStorage(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprint("multi=", multi), func(t *testing.T) {
			svc, _, _ := newTestServiceWithDB(t)
			be, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{DataDir: t.TempDir()})
			require.NoError(t, err)
			svc.engine = engine.NewEngine(be, time.Second)
			defer svc.engine.Close()
			root := t.TempDir()
			name := "Movie folder"
			relative := name
			piece := sha1.Sum([]byte("x"))
			info := metainfo.Info{Name: name, Length: 1, PieceLength: 16384, Pieces: piece[:]}
			if multi {
				info.Length = 0
				info.Files = []metainfo.FileInfo{{Path: []string{"Nested folder", "Film café #1 %.mkv"}, Length: 1}}
				relative = filepath.Join(name, "Nested folder", "Film café #1 %.mkv")
			}
			path := filepath.Join(root, relative)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
			require.NoError(t, os.WriteFile(path, []byte("x"), 0600))
			encoded, err := bencode.Marshal(info)
			require.NoError(t, err)
			mi := metainfo.MetaInfo{InfoBytes: encoded}
			var blob bytes.Buffer
			require.NoError(t, mi.Write(&blob))
			id, err := svc.PrepareTorrent(sysCtx(), PrepareTorrentRequest{TorrentFile: blob.Bytes(), SavePath: root})
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				d, err := svc.GetTorrentDetail(sysCtx(), id)
				return err == nil && len(d.Files) == 1 && d.Files[0].BytesDone == 1
			}, 5*time.Second, 10*time.Millisecond)
			actual, err := svc.TorrentFilePath(sysCtx(), id, 0)
			require.NoError(t, err)
			expected, err := filepath.EvalSymlinks(path)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		})
	}
}
