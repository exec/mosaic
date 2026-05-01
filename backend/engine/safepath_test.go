package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeRemovePath_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		torrent string
		saveTo  string
		wantErr bool
	}{
		{name: "parent dir", torrent: "..", saveTo: "/tmp/save", wantErr: true},
		{name: "single escape", torrent: "../escape", saveTo: "/tmp/save", wantErr: true},
		{name: "double escape to passwd", torrent: "../../etc/passwd", saveTo: "/tmp/save", wantErr: true},
		{name: "absolute path", torrent: "/absolute/path", saveTo: "/tmp/save", wantErr: true},
		{name: "empty name", torrent: "", saveTo: "/tmp/save", wantErr: true},
		{name: "dot resolves to save dir", torrent: ".", saveTo: "/tmp/save", wantErr: true},
		{name: "trailing slash dot", torrent: "./", saveTo: "/tmp/save", wantErr: true},
		{name: "NUL byte", torrent: "with\x00null", saveTo: "/tmp/save", wantErr: true},
		{name: "normal name", torrent: "normal-torrent", saveTo: "/tmp/save", wantErr: false},
		{name: "subdir/file", torrent: "Subdir/file.iso", saveTo: "/tmp/save", wantErr: false},
		{name: "hidden", torrent: ".hidden", saveTo: "/tmp/save", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := safeRemovePath(tc.saveTo, tc.torrent)
			if tc.wantErr {
				require.Error(t, err, "expected error for name=%q", tc.torrent)
				return
			}
			require.NoError(t, err)
			// Returned path must be under (cleaned) saveTo lexically.
			cleanSaveTo := filepath.Clean(tc.saveTo)
			require.True(t,
				got == cleanSaveTo || strings.HasPrefix(got, cleanSaveTo+string(filepath.Separator)),
				"path %q should live under %q", got, cleanSaveTo)
		})
	}
}

// TestSafeRemovePath_TraversalDoesNotDeleteSentinel is the integration-style
// guarantee: even if a malicious .torrent set name="../sentinel.txt", the
// sentinel file in the parent of saveTo must remain on disk after we'd have
// called RemoveAll on the validated path. We assert (a) safeRemovePath errors
// and (b) the sentinel still exists on disk.
func TestSafeRemovePath_TraversalDoesNotDeleteSentinel(t *testing.T) {
	parent := t.TempDir()
	saveTo := filepath.Join(parent, "save")
	require.NoError(t, os.MkdirAll(saveTo, 0o755))

	sentinelPath := filepath.Join(parent, "sentinel.txt")
	require.NoError(t, os.WriteFile(sentinelPath, []byte("do not delete"), 0o644))

	// A malicious info.Name pointing at the sentinel one level above saveTo.
	maliciousName := filepath.Join("..", "sentinel.txt")

	path, err := safeRemovePath(saveTo, maliciousName)
	require.Error(t, err, "traversal name should be rejected")
	require.Empty(t, path, "no path should be returned on rejection")

	// Caller of safeRemovePath skips RemoveAll on error; assert the file
	// the attacker tried to reach is still on disk.
	_, statErr := os.Stat(sentinelPath)
	require.NoError(t, statErr, "sentinel must still exist after traversal attempt")
}

// TestSafeRemovePath_SymlinkInsideSaveToCannotEscape covers the EvalSymlinks
// branch: an attacker-crafted symlink inside saveTo that points outside it
// must not let RemoveAll follow the link out.
func TestSafeRemovePath_SymlinkInsideSaveToCannotEscape(t *testing.T) {
	parent := t.TempDir()
	saveTo := filepath.Join(parent, "save")
	require.NoError(t, os.MkdirAll(saveTo, 0o755))

	outside := filepath.Join(parent, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	sentinel := filepath.Join(outside, "keep.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o644))

	// Plant a symlink inside saveTo whose target is the outside directory.
	linkInside := filepath.Join(saveTo, "evil")
	require.NoError(t, os.Symlink(outside, linkInside))

	path, err := safeRemovePath(saveTo, "evil")
	require.Error(t, err, "symlink that resolves outside saveTo must be rejected")
	require.Empty(t, path)

	_, statErr := os.Stat(sentinel)
	require.NoError(t, statErr, "sentinel reachable via symlink must still exist")
}

// TestSafeRemovePath_NonExistentTargetFallsBackToLexical: if the user has
// already deleted the files (target missing) but the name was benign, the
// helper should still succeed and return the cleaned path. This guards the
// "EvalSymlinks fails because target gone" fallback branch.
func TestSafeRemovePath_NonExistentTargetFallsBackToLexical(t *testing.T) {
	saveTo := t.TempDir()
	got, err := safeRemovePath(saveTo, "already-deleted-torrent")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(filepath.Clean(saveTo), "already-deleted-torrent"), got)
}

// TestSafeRemovePath_NormalNameUnderRealSaveTo verifies the happy path with a
// real on-disk directory (so EvalSymlinks succeeds on both ends).
func TestSafeRemovePath_NormalNameUnderRealSaveTo(t *testing.T) {
	saveTo := t.TempDir()
	target := filepath.Join(saveTo, "linux-iso")
	require.NoError(t, os.MkdirAll(target, 0o755))

	got, err := safeRemovePath(saveTo, "linux-iso")
	require.NoError(t, err)

	// EvalSymlinks may resolve macOS /var → /private/var; assert containment
	// against the resolved saveTo, not the original tempdir string.
	evalSaveTo, err := filepath.EvalSymlinks(saveTo)
	require.NoError(t, err)
	require.True(t,
		got == evalSaveTo || strings.HasPrefix(got, evalSaveTo+string(filepath.Separator)),
		"got %q should live under %q", got, evalSaveTo)
}
