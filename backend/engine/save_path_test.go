package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateSavePath_AdminMode covers restrictToRoot=false: every shape
// should pass through to a cleaned absolute path. Empty / NUL still rejected.
func TestValidateSavePath_AdminMode(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "absolute path under tmp", path: "/tmp/admin/save", wantErr: false},
		{name: "absolute path under etc", path: "/etc/cron.d", wantErr: false},
		{name: "relative path gets absolute", path: "relative-dir", wantErr: false},
		{name: "empty", path: "", wantErr: true},
		{name: "whitespace only", path: "   ", wantErr: true},
		{name: "NUL byte", path: "/tmp/with\x00null", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateSavePath("", tc.path, false)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.True(t, filepath.IsAbs(got), "result %q should be absolute", got)
		})
	}
}

// TestValidateSavePath_NonAdmin_RejectsEscapes is the load-bearing case for
// the multi-user fix: every shape that lets a non-admin write outside their
// root must be rejected. Errors must NOT be wantErr=false silently.
func TestValidateSavePath_NonAdmin_RejectsEscapes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "users", "bob")
	require.NoError(t, os.MkdirAll(root, 0o700))

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Containment rejects
		{name: "dotdot escape one level", path: filepath.Join(root, ".."), wantErr: true},
		{name: "dotdot escape two levels", path: filepath.Join(root, "..", ".."), wantErr: true},
		{name: "dotdot mid-path back into siblings", path: filepath.Join(root, "..", "alice"), wantErr: true},
		{name: "absolute outside root", path: "/etc/cron.d", wantErr: true},
		{name: "absolute look-alike prefix", path: parent + "/users/bobby", wantErr: true},
		{name: "absolute parent of root", path: parent, wantErr: true},

		// Pure-evil shapes
		{name: "empty", path: "", wantErr: true},
		{name: "NUL byte in path", path: filepath.Join(root, "evil\x00"), wantErr: true},

		// Legitimate in-root paths
		{name: "exact root", path: root, wantErr: false},
		{name: "subdir of root", path: filepath.Join(root, "downloads"), wantErr: false},
		{name: "deep subdir of root", path: filepath.Join(root, "a", "b", "c"), wantErr: false},
		{name: "subdir with dotdot that resolves back inside", path: filepath.Join(root, "a", "..", "b"), wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateSavePath(root, tc.path, true)
			if tc.wantErr {
				require.Error(t, err, "expected rejection for path=%q", tc.path)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err, "got=%q", got)
			require.True(t, filepath.IsAbs(got))
			// Sanity: the returned path must be under root (after EvalSymlinks,
			// since /tmp is a symlink to /private/tmp on macOS test runners).
			evalRoot, evalErr := filepath.EvalSymlinks(root)
			require.NoError(t, evalErr)
			evalGot, evalErr := filepath.EvalSymlinks(got)
			if evalErr != nil {
				// Leaf doesn't exist yet — fall back to cleaned absolute.
				evalGot = got
			}
			require.True(t,
				evalGot == evalRoot || strings.HasPrefix(evalGot, evalRoot+string(filepath.Separator)),
				"resolved path %q should live under resolved root %q", evalGot, evalRoot)
		})
	}
}

// TestValidateSavePath_SymlinkEscape: a symlink planted somewhere inside the
// allowed root that points OUTSIDE the root must cause a rejection even when
// lexical containment of the requested path passes.
func TestValidateSavePath_SymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "users", "bob")
	require.NoError(t, os.MkdirAll(root, 0o700))

	outside := filepath.Join(parent, "secret")
	require.NoError(t, os.MkdirAll(outside, 0o700))

	// Plant a symlink inside root whose target is the outside directory.
	linkInside := filepath.Join(root, "downloads")
	require.NoError(t, os.Symlink(outside, linkInside))

	// Caller asks to save under <root>/downloads/sub — lexically inside root,
	// but the symlink redirects it to <parent>/secret/sub.
	requested := filepath.Join(root, "downloads", "sub")
	_, err := ValidateSavePath(root, requested, true)
	require.Error(t, err, "symlink-escape should be rejected after EvalSymlinks")
}

// TestValidateSavePath_SymlinkContained: a symlink inside the root whose
// target is ALSO inside the root must be accepted — we don't want to
// reject legitimate user-organised setups.
func TestValidateSavePath_SymlinkContained(t *testing.T) {
	root := t.TempDir()
	realDest := filepath.Join(root, "real")
	require.NoError(t, os.MkdirAll(realDest, 0o700))
	link := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(realDest, link))

	got, err := ValidateSavePath(root, filepath.Join(link, "sub"), true)
	require.NoError(t, err)
	require.NotEmpty(t, got)
}

// TestValidateSavePath_NonExistentLeafIsOK: MkdirAll is about to create the
// leaf, so the validator must accept a path whose deepest component doesn't
// exist yet — provided every existing ancestor is contained.
func TestValidateSavePath_NonExistentLeafIsOK(t *testing.T) {
	root := t.TempDir()
	got, err := ValidateSavePath(root, filepath.Join(root, "not-yet-created", "deep"), true)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got, filepath.Clean(root)))
}

// TestValidateSavePath_EmptyRootWhenRestricted refuses to silently allow an
// empty root when restrictToRoot is on.
func TestValidateSavePath_EmptyRootWhenRestricted(t *testing.T) {
	_, err := ValidateSavePath("", "/tmp/foo", true)
	require.Error(t, err)
}

// TestSanityCheckSaveTo covers the defense-in-depth guard fired before the
// destructive RemoveAll in the engine's Remove(delete=true) path.
func TestSanityCheckSaveTo(t *testing.T) {
	tests := []struct {
		name    string
		saveTo  string
		wantErr bool
	}{
		{name: "empty", saveTo: "", wantErr: true},
		{name: "NUL byte", saveTo: "/tmp/save\x00", wantErr: true},
		{name: "relative", saveTo: "save", wantErr: true},
		{name: "dot", saveTo: ".", wantErr: true},
		{name: "dotdot", saveTo: "..", wantErr: true},
		{name: "filesystem root", saveTo: "/", wantErr: true},
		{name: "normal absolute", saveTo: "/tmp/save", wantErr: false},
		{name: "deep absolute", saveTo: "/var/lib/mosaic/users/bob/downloads", wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := sanityCheckSaveTo(tc.saveTo)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
