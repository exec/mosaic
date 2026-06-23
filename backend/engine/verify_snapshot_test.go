package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"
)

// makeFakeTorrentTree writes a multi-file torrent tree under saveTo and
// returns the matching *metainfo.Info. Files contain one byte each by
// default; callers can override via the size map. Layout matches anacrolix's
// storage.NewFile: saveTo/info.Name/<file.Path...>.
func makeFakeTorrentTree(t *testing.T, saveTo string) *metainfo.Info {
	t.Helper()
	info := &metainfo.Info{
		Name:        "demo",
		PieceLength: 16384,
		Files: []metainfo.FileInfo{
			{Path: []string{"a.txt"}, Length: 5},
			{Path: []string{"sub", "b.bin"}, Length: 3},
		},
	}
	root := filepath.Join(saveTo, info.Name)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "b.bin"), []byte{0x01, 0x02, 0x03}, 0o644))
	return info
}

func TestComputeFileSnapshot_StableForUnchangedFiles(t *testing.T) {
	saveTo := t.TempDir()
	info := makeFakeTorrentTree(t, saveTo)

	a, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.NotEmpty(t, a)

	b, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.True(t, bytes.Equal(a, b), "snapshot must be stable for unchanged files")
}

func TestComputeFileSnapshot_ChangesOnSizeDelta(t *testing.T) {
	saveTo := t.TempDir()
	info := makeFakeTorrentTree(t, saveTo)

	before, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)

	// Append a byte to one file → size changes → snapshot changes.
	target := filepath.Join(saveTo, info.Name, "a.txt")
	require.NoError(t, os.WriteFile(target, []byte("hello!"), 0o644))

	after, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.False(t, bytes.Equal(before, after), "size change must alter snapshot")
}

func TestComputeFileSnapshot_ChangesOnMtimeDelta(t *testing.T) {
	saveTo := t.TempDir()
	info := makeFakeTorrentTree(t, saveTo)

	before, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)

	// Push mtime forward without changing content. computeFileSnapshot
	// digests stat.ModTime().UnixNano(), so this MUST change the digest.
	target := filepath.Join(saveTo, info.Name, "a.txt")
	future := time.Now().Add(1 * time.Hour)
	require.NoError(t, os.Chtimes(target, future, future))

	after, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.False(t, bytes.Equal(before, after), "mtime change must alter snapshot")
}

func TestComputeFileSnapshot_MissingFileReflected(t *testing.T) {
	saveTo := t.TempDir()
	info := makeFakeTorrentTree(t, saveTo)

	before, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)

	// Delete one of the files. The snapshot should still compute (with a
	// MISSING marker for the gone path) and differ from the pre-delete one.
	target := filepath.Join(saveTo, info.Name, "sub", "b.bin")
	require.NoError(t, os.Remove(target))

	after, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err, "snapshot must still compute when files are missing")
	require.NotEmpty(t, after)
	require.False(t, bytes.Equal(before, after), "deletion must alter snapshot")
}

func TestComputeFileSnapshot_SingleFileTorrent(t *testing.T) {
	saveTo := t.TempDir()
	info := &metainfo.Info{
		Name:        "single.bin",
		PieceLength: 16384,
		Length:      4,
		// no Files — single-file torrent
	}
	require.NoError(t, os.WriteFile(filepath.Join(saveTo, info.Name), []byte{1, 2, 3, 4}, 0o644))

	a, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.NotEmpty(t, a)

	// Missing single-file torrent file → still computes (MISSING marker)
	// and digest differs.
	require.NoError(t, os.Remove(filepath.Join(saveTo, info.Name)))
	b, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.False(t, bytes.Equal(a, b))
}

func TestComputeFileSnapshot_NilInfo(t *testing.T) {
	_, err := computeFileSnapshot(nil, t.TempDir())
	require.Error(t, err)
}

// TestComputeFileSnapshot_ChangesOnTypeSwap covers the mode/type component of
// the per-file digest line. Swapping a regular file for a directory (a
// regular↔dir swap) must change the digest even though both entries exist —
// otherwise a planted directory/symlink could masquerade as the original file
// and slip past fast-resume. computeFileSnapshot digests st.Mode() (which
// carries the type bits) precisely to catch this.
func TestComputeFileSnapshot_ChangesOnTypeSwap(t *testing.T) {
	saveTo := t.TempDir()
	info := makeFakeTorrentTree(t, saveTo)

	before, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)

	// Replace the regular file a.txt with a directory of the same name. Size
	// and even mtime may differ, but the load-bearing assertion is that the
	// type change alone is reflected in the digest.
	target := filepath.Join(saveTo, info.Name, "a.txt")
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Mkdir(target, 0o755))

	after, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.False(t, bytes.Equal(before, after), "regular→dir type swap must alter snapshot")
}

// TestComputeFileSnapshot_NotFollowSymlink verifies os.Lstat semantics: when a
// file is replaced by a symlink (even one pointing at content of the same
// size), the digest records the symlink's own mode rather than following it to
// the target — so the swap is detected. Guards against a symlink-redirection
// fast-resume bypass.
func TestComputeFileSnapshot_NotFollowSymlink(t *testing.T) {
	saveTo := t.TempDir()
	info := makeFakeTorrentTree(t, saveTo)

	before, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)

	// Replace a.txt (content "hello", 5 bytes) with a symlink to another file
	// that also holds 5 bytes. If we followed the link via os.Stat the size
	// would match and the digest could collide; with os.Lstat the symlink mode
	// bit changes the digest.
	target := filepath.Join(saveTo, info.Name, "a.txt")
	decoy := filepath.Join(saveTo, "decoy.txt")
	require.NoError(t, os.WriteFile(decoy, []byte("world"), 0o644))
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Symlink(decoy, target))

	after, err := computeFileSnapshot(info, saveTo)
	require.NoError(t, err)
	require.False(t, bytes.Equal(before, after), "regular→symlink swap must alter snapshot (Lstat, not Stat)")
}
