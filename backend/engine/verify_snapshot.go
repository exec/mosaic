package engine

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

// SnapshotStore is an optional persistence hook for fast-resume verify
// snapshots. When non-nil, the engine consults it on startup-add to decide
// whether to skip the full piece-by-piece hash. Set to nil to disable the
// optimization.
//
// The lifecycle is: write after a successful verify when the torrent is
// fully present; read on the next startup-add and compare against the
// current on-disk file state; delete on Recheck so the next verify rewrites
// it from a known-good state. All methods must be safe to call from any
// goroutine — the engine fans out verify work in parallel.
type SnapshotStore interface {
	LoadVerifySnapshot(id TorrentID) (snapshot []byte, wasComplete bool, ok bool)
	SaveVerifySnapshot(id TorrentID, snapshot []byte, wasComplete bool) error
	DeleteVerifySnapshot(id TorrentID) error

	// LoadPieceBitmap returns the file snapshot, completion flag, and the
	// per-piece completion bitmap saved for the torrent. ok=false means
	// no row exists; bitmap may still be nil even when ok=true (legacy
	// rows saved before bitmap tracking — caller should fall back to
	// the slow verify path when bitmap is nil).
	LoadPieceBitmap(id TorrentID) (snapshot []byte, wasComplete bool, bitmap []byte, ok bool)
	// SavePieceBitmap atomically persists snapshot + wasComplete + bitmap.
	// bitmap is `(NumPieces + 7) / 8` bytes; bit i is 1 iff piece i is
	// complete. Used to reconstruct anacrolix's bolt piece-completion
	// store after its per-add storage init wipes "complete" entries
	// for partial-with-.part-files torrents.
	SavePieceBitmap(id TorrentID, snapshot []byte, wasComplete bool, bitmap []byte) error
}

// computeFileSnapshot returns a SHA-256 over the torrent's on-disk file
// state. Each metainfo file contributes a `path|size|mtime_ns` line; missing
// files contribute `MISSING:<path>` so a deletion changes the digest. The
// path component is the joined relative path inside the torrent (forward-
// slash separators) so the digest is stable across OSes for the same files.
//
// `saveTo` matches anacrolix's `storage.NewFile` root: a single-file torrent
// lives at `saveTo/<info.Name>`; a multi-file torrent's files live at
// `saveTo/<info.Name>/<file.Path...>`. If info.Files is empty (single-file
// mode) we synthesize a single entry from info.Name + info.Length.
func computeFileSnapshot(info *metainfo.Info, saveTo string) ([]byte, error) {
	if info == nil {
		return nil, fmt.Errorf("computeFileSnapshot: nil info")
	}
	h := sha256.New()
	first := true
	writeLine := func(line string) {
		if !first {
			_, _ = h.Write([]byte{'\n'})
		}
		first = false
		_, _ = h.Write([]byte(line))
	}

	if len(info.Files) == 0 {
		// Single-file torrent. Path on disk is saveTo/info.Name.
		rel := info.Name
		full := filepath.Join(saveTo, info.Name)
		st, err := os.Stat(full)
		if err != nil {
			if os.IsNotExist(err) {
				writeLine("MISSING:" + rel)
			} else {
				return nil, err
			}
		} else {
			writeLine(fmt.Sprintf("%s|%d|%d", rel, st.Size(), st.ModTime().UnixNano()))
		}
	} else {
		for _, f := range info.Files {
			rel := strings.Join(f.Path, "/")
			parts := append([]string{saveTo, info.Name}, f.Path...)
			full := filepath.Join(parts...)
			st, err := os.Stat(full)
			if err != nil {
				if os.IsNotExist(err) {
					writeLine("MISSING:" + rel)
					continue
				}
				return nil, err
			}
			writeLine(fmt.Sprintf("%s|%d|%d", rel, st.Size(), st.ModTime().UnixNano()))
		}
	}
	return h.Sum(nil), nil
}
