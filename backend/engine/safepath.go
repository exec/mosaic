package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeRemovePath validates that joining `name` (the torrent's bencoded info.Name)
// onto `saveTo` produces a path that stays inside `saveTo`, and returns the
// absolute path safe to RemoveAll.
//
// A malicious .torrent can set name = "../../etc/passwd"; without this check,
// os.RemoveAll(filepath.Join(saveTo, name)) would walk above the user's save
// directory and delete arbitrary files when "Remove and delete files" is used.
//
// Validation rules (all rejections produce a non-nil error):
//   - empty name
//   - name contains a NUL byte (defense in depth; some filesystems treat it weird)
//   - filepath.IsAbs(name)
//   - filepath.Rel(saveTo, target) errors, or returns ".." / a path starting with "../"
//
// Symlink handling: when both saveTo and the joined target exist, we resolve
// symlinks on both ends via filepath.EvalSymlinks before the containment check,
// so a symlink planted inside saveTo can't redirect deletion outside it. When
// the target doesn't exist (files already gone), we fall back to a pure-lexical
// check on the cleaned paths — there's nothing to delete in that case anyway,
// but we still refuse paths that would have escaped.
func safeRemovePath(saveTo, name string) (string, error) {
	if name == "" {
		return "", errors.New("safeRemovePath: empty name")
	}
	if strings.ContainsRune(name, 0) {
		return "", errors.New("safeRemovePath: name contains NUL byte")
	}
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("safeRemovePath: name %q is absolute", name)
	}

	cleanSaveTo := filepath.Clean(saveTo)
	cleanTarget := filepath.Clean(filepath.Join(cleanSaveTo, name))

	// Refuse names that resolve to saveTo itself ("." / "" / "./"). Without
	// this, RemoveAll would wipe the entire user save directory.
	if cleanTarget == cleanSaveTo {
		return "", fmt.Errorf("safeRemovePath: name %q resolves to save dir itself", name)
	}

	// Lexical containment check — cheap and catches the common cases
	// (../foo, ../../foo) before we ever touch the filesystem.
	if !pathContained(cleanSaveTo, cleanTarget) {
		return "", fmt.Errorf("safeRemovePath: %q escapes %q", name, saveTo)
	}

	// Resolve symlinks if both ends exist on disk. EvalSymlinks fails when the
	// target is missing (the user's already deleted the files) — in that case
	// the lexical check above is sufficient and we return cleanTarget directly.
	evalSaveTo, err := filepath.EvalSymlinks(cleanSaveTo)
	if err != nil {
		// This is a DELETE operation, so fail closed. If saveTo doesn't exist
		// there's nothing under it to remove and the lexical check already
		// passed — degrade to the lexical result. But for any other error
		// (permission denied, a symlink loop, an I/O error) we must NOT fall
		// back to a lexical-only check: that would let a symlink we couldn't
		// resolve redirect the RemoveAll outside saveTo. Refuse instead.
		if os.IsNotExist(err) {
			return cleanTarget, nil
		}
		return "", fmt.Errorf("safeRemovePath: cannot resolve save dir %q: %w", saveTo, err)
	}
	evalTarget, err := filepath.EvalSymlinks(cleanTarget)
	if err != nil {
		// Target doesn't exist — already removed, or never written. Lexical
		// check passed, nothing to delete, but return the cleaned path so the
		// caller's RemoveAll is a no-op rather than an error.
		return cleanTarget, nil
	}
	if !pathContained(evalSaveTo, evalTarget) {
		return "", fmt.Errorf("safeRemovePath: %q resolves outside %q via symlink", name, saveTo)
	}
	return evalTarget, nil
}

// pathContained reports whether target is base or a descendant of base, using
// filepath.Rel as the source of truth. Both inputs should already be Clean'd.
func pathContained(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// sanityCheckSaveTo is the last-line-of-defense guard fired right before
// RemoveAll on a torrent's recorded save directory. By the time we reach
// here the api/ layer should already have rejected a hostile path at Add
// time via engine.ValidateSavePath — but if a legacy record predates the
// validator, or some future caller skipped it, we still refuse to fire
// RemoveAll on anything that smells dangerous.
//
// Rejections:
//   - empty
//   - contains NUL byte
//   - not absolute after Clean (so a bare "etc" or "../something" recorded
//     pre-validation can't escalate to whatever happens to be the CWD)
//   - resolves (lexically) to the filesystem root or "/" sentinel
func sanityCheckSaveTo(saveTo string) error {
	if saveTo == "" {
		return errors.New("save path empty")
	}
	if strings.ContainsRune(saveTo, 0) {
		return errors.New("save path contains NUL byte")
	}
	clean := filepath.Clean(saveTo)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("save path %q is not absolute", saveTo)
	}
	// filepath.Dir of a filesystem root returns the root itself; refuse to
	// RemoveAll on "/" or any single-component root path. Belt and braces
	// against a bug elsewhere that records SavePath="" → cleaned to ".".
	if filepath.Dir(clean) == clean {
		return fmt.Errorf("save path %q is a filesystem root", clean)
	}
	return nil
}
