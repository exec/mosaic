package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateSavePath checks that savePath is safe to MkdirAll into and returns
// a cleaned absolute path the caller should use in place of the input.
//
// Multi-user mosaicd lets non-admins add torrents; an unconstrained save_path
// in a request body would let those callers target arbitrary directories the
// daemon process can write to — `/etc/cron.d`, `~root/.ssh`, another user's
// home — because the engine simply os.MkdirAll(savePath, 0o755)s whatever it
// gets handed. This validator is the chokepoint that callers must funnel
// non-admin save paths through before either the engine MkdirAll or the
// engine's Add* methods see them.
//
// Behavior:
//
//   - restrictToRoot=false: any caller-supplied path is accepted after a
//     basic sanity pass (non-empty, no NUL, resolvable to an absolute path).
//     This is the admin / single-user-desktop mode — the operator already has
//     unrestricted shell access to the box, restricting save paths buys
//     nothing.
//
//   - restrictToRoot=true: the cleaned absolute savePath MUST be a descendant
//     of (or equal to) the cleaned absolute root. The check uses
//     filepath.Abs + filepath.Rel — string-prefix checks would miss legitimate
//     edge cases ("/data/userA" vs "/data/userAB") and string-contains checks
//     for ".." miss absolute-path escapes entirely. Symlinks are resolved on
//     whichever ancestor of savePath already exists on disk (the leaf usually
//     does not — MkdirAll is about to create it), then re-checked for
//     containment after resolution, so a symlink-escape planted inside the
//     allowed root can't redirect the eventual write outside it.
//
// Returns the cleaned absolute path to use, or an error describing the
// rejection. The error is intentionally generic in its leak surface
// ("save path escapes user root") so it can be surfaced to a non-admin
// caller without revealing other users' directory layout.
func ValidateSavePath(root, savePath string, restrictToRoot bool) (string, error) {
	if strings.TrimSpace(savePath) == "" {
		return "", errors.New("save path is empty")
	}
	if strings.ContainsRune(savePath, 0) {
		return "", errors.New("save path contains NUL byte")
	}

	absSave, err := filepath.Abs(savePath)
	if err != nil {
		return "", fmt.Errorf("resolve save path: %w", err)
	}
	absSave = filepath.Clean(absSave)

	if !restrictToRoot {
		return absSave, nil
	}

	if strings.TrimSpace(root) == "" {
		// restrictToRoot was asked for but no root supplied — refuse rather
		// than silently degrade to "no restriction".
		return "", errors.New("save path validation: empty root")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve user root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)

	// Lexical containment first — cheap and covers the dominant attack
	// shapes (absolute paths outside root, ../ traversal).
	if !pathContained(absRoot, absSave) {
		return "", fmt.Errorf("save path %q escapes user root %q", savePath, absRoot)
	}

	// Symlink resolution. The leaf typically doesn't exist yet (MkdirAll
	// is about to create it), so EvalSymlinks on absSave would error out
	// and never run on the malicious symlink that already exists somewhere
	// in the middle. Walk up to the first ancestor that exists, resolve
	// THAT, then re-check containment using the resolved ancestor in place
	// of absSave's existing prefix. The same trick is applied to absRoot
	// so a symlink-resolved root (e.g. /var → /private/var on macOS) lines
	// up.
	resolvedRoot, err := evalAncestorSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve user root symlinks: %w", err)
	}
	resolvedSave, err := evalAncestorSymlinks(absSave)
	if err != nil {
		return "", fmt.Errorf("resolve save path symlinks: %w", err)
	}
	if !pathContained(resolvedRoot, resolvedSave) {
		return "", fmt.Errorf("save path %q escapes user root via symlink", savePath)
	}

	return absSave, nil
}

// evalAncestorSymlinks resolves symlinks on the deepest ancestor of p that
// exists on disk, then re-joins any non-existent leaf components onto the
// resolved prefix. This is the standard "validate a path that's about to be
// MkdirAll'd" pattern: filepath.EvalSymlinks(p) errors when p doesn't exist,
// which would defeat us because the whole point is that MkdirAll is about
// to create p. By walking up to the first existing ancestor we still catch
// a malicious symlink anywhere in the existing prefix.
func evalAncestorSymlinks(p string) (string, error) {
	p = filepath.Clean(p)
	// Walk up until lstat succeeds, accumulating the missing suffix.
	var missing []string
	cur := p
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		} else if !os.IsNotExist(err) {
			// Permission denied or other I/O error — treat as not-resolvable.
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root and still not found — return as is.
			break
		}
		missing = append([]string{filepath.Base(cur)}, missing...)
		cur = parent
	}
	resolved, err := filepath.EvalSymlinks(cur)
	if err != nil {
		// `cur` was the existing ancestor; if EvalSymlinks fails here it's
		// a genuine error (permissions, race, deleted between Lstat and
		// EvalSymlinks). Surface it.
		if os.IsNotExist(err) {
			// Race: ancestor disappeared between Lstat and EvalSymlinks.
			// Fall back to the lexical cleaned path.
			return p, nil
		}
		return "", err
	}
	if len(missing) == 0 {
		return resolved, nil
	}
	return filepath.Join(append([]string{resolved}, missing...)...), nil
}
