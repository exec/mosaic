package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
)

func FreeSpace(path string) int64 {
	for {
		if _, err := os.Stat(path); err == nil {
			return diskFreeBytes(path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return -1
		}
		path = parent
	}
}

// A failed copy cleans only a destination it created exclusively.
func copyContent(ctx context.Context, src, dst string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		if err = os.Mkdir(dst, st.Mode().Perm()); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = os.RemoveAll(dst)
			}
		}()
		entries, e := os.ReadDir(src)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if err = copyContent(ctx, filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("cannot move non-regular file: %s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func contentSize(paths []string) (size int64, found bool, err error) {
	for _, p := range paths {
		err = filepath.Walk(p, func(path string, st os.FileInfo, e error) error {
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if !st.IsDir() && !st.Mode().IsRegular() {
				return fmt.Errorf("cannot use non-regular torrent data: %s", path)
			}
			found = true
			if st.Mode().IsRegular() {
				size += st.Size()
			}
			return nil
		})
		if err != nil {
			return
		}
	}
	return
}

// Relocate leaves the torrent paused and verifies the replacement storage.
func (a *AnacrolixBackend) Relocate(ctx context.Context, id TorrentID, savePath string, move bool) (err error) {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	snap, err := a.Snapshot(id)
	if err != nil {
		return err
	}
	if !snap.Paused || snap.Verifying || t.Info() == nil {
		return errors.New("pause the torrent and wait for verification before changing storage")
	}
	old := a.saveDirFor(id)
	if err = sanityCheckSaveTo(old); err != nil {
		return err
	}
	if err = sanityCheckSaveTo(savePath); err != nil {
		return err
	}
	old, err = filepath.EvalSymlinks(old)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(savePath, 0755); err != nil {
		return err
	}
	savePath, err = filepath.EvalSymlinks(savePath)
	if err != nil {
		return err
	}
	if old == savePath {
		return errors.New("choose a different folder")
	}
	// Check lexical roots as well: safeRemovePath resolves links, which moving must reject.
	sourceRoot := filepath.Join(old, t.Info().BestName())
	src, err := safeRemovePath(old, t.Info().BestName())
	if err != nil {
		return err
	}
	if _, _, err = contentSize(contentPaths(sourceRoot, t.Info().IsDir())); err != nil {
		return err
	}
	dst, err := safeRemovePath(savePath, t.Info().BestName())
	if err != nil {
		return err
	}
	if pathContained(src, dst) || pathContained(dst, src) {
		return errors.New("source and destination overlap")
	}
	sources := contentPaths(src, t.Info().IsDir())
	targets := contentPaths(dst, t.Info().IsDir())
	if move {
		needed, _, e := contentSize(sources)
		if e != nil {
			return e
		}
		if free := FreeSpace(savePath); free >= 0 && free < needed+(64<<20) {
			return errors.New("not enough free space to move data safely")
		}
		// Refuse either name even if the source currently uses only the other (.part/final).
		for _, p := range targets {
			if _, e := os.Lstat(p); !os.IsNotExist(e) {
				return errors.New("destination already contains torrent data; use Locate existing instead")
			}
		}
	} else {
		lexical := filepath.Join(savePath, t.Info().BestName())
		_, found, e := contentSize(contentPaths(lexical, t.Info().IsDir()))
		if e != nil {
			return e
		}
		if !found {
			return errors.New("no matching torrent data found in that folder")
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	mi := t.Metainfo()
	spec, err := torrent.TorrentSpecFromMetaInfoErr(&mi)
	if err != nil {
		return err
	}
	spec.DisallowDataDownload = true
	spec.DisallowDataUpload = true
	// Drain pending checkpoints before changing the root used to compute their digest.
	wasSaved, err := a.quiesceSnapshots(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			a.snapshotMu.Lock()
			a.snapshotSaved[id] = wasSaved
			a.snapshotMu.Unlock()
		}
	}()
	// Pause can leave a chunk write in flight. Drop must finish before copying.
	t.Drop()
	copied := []string{}
	defer func() {
		if err == nil {
			return
		}
		for _, p := range copied {
			_ = os.RemoveAll(p)
		}
		spec.Storage = newFileStorage(old, a.pieceCompletion, a.preallocateFullFiles)
		restored, _, restoreErr := a.client.AddTorrentSpec(spec)
		if restoreErr != nil {
			err = fmt.Errorf("%w; restore original storage: %v", err, restoreErr)
			return
		}
		a.mu.Lock()
		a.byID[id] = restored
		a.mu.Unlock()
		a.installWriteErrorHook(id, restored)
		a.spawnVerify(id, restored)
	}()
	if move {
		for i, s := range sources {
			if _, e := os.Lstat(s); os.IsNotExist(e) {
				continue
			}
			if err = copyContent(ctx, s, targets[i]); err != nil {
				return err
			}
			copied = append(copied, targets[i])
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	spec.Storage = newFileStorage(savePath, a.pieceCompletion, a.preallocateFullFiles)
	replacement, _, err := a.client.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.byID[id] = replacement
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	a.installWriteErrorHook(id, replacement)
	if a.snapshotStore != nil {
		_ = a.snapshotStore.DeleteVerifySnapshot(id)
	}
	a.snapshotMu.Lock()
	delete(a.snapshotSaved, id)
	a.snapshotMu.Unlock()
	if snap.Completed {
		a.MarkExpectedComplete(id)
	}
	_ = a.startVerification(id, replacement, func(ctx context.Context) { a.verifyAndStart(ctx, id, replacement, false) })
	if move {
		for _, p := range sources {
			if e := os.RemoveAll(p); e != nil {
				a.raiseError(id, fmt.Errorf("data moved, but original could not be removed: %w", e))
			}
		}
	}
	return nil
}

func contentPaths(root string, isDir bool) []string {
	if isDir {
		return []string{root}
	}
	return []string{root, root + ".part"}
}
