package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	anacrolix_types "github.com/anacrolix/torrent/types"
)

func (a *AnacrolixBackend) AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error) {
	return a.Add(ctx, AddRequest{Magnet: magnet, SavePath: savePath})
}

func (a *AnacrolixBackend) Add(ctx context.Context, req AddRequest) (TorrentID, error) {
	var spec *torrent.TorrentSpec
	var err error
	if len(req.TorrentFile) > 0 {
		mi, e := metainfo.Load(bytes.NewReader(req.TorrentFile))
		if e != nil {
			return "", e
		}
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
	} else {
		spec, err = torrent.TorrentSpecFromMagnetUri(req.Magnet)
	}
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(req.SavePath, 0755); err != nil {
		return "", err
	}
	spec.DisallowDataDownload = true // Verification and file choices precede payload requests.
	spec.DisallowDataUpload = req.Paused
	spec.Storage = newFileStorage(req.SavePath, a.pieceCompletion, a.preallocateFullFiles)
	t, added, err := a.client.AddTorrentSpec(spec)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	if !added {
		return id, nil
	}
	a.mu.Lock()
	a.bySaveTo[id] = req.SavePath
	a.byID[id] = t
	a.mu.Unlock()
	a.pausedMu.Lock()
	a.paused[id] = req.Paused
	if a.filePriorities == nil {
		a.filePriorities = make(map[TorrentID]map[int]Priority)
	}
	a.filePriorities[id] = make(map[int]Priority)
	for i, p := range req.Priorities {
		a.filePriorities[id][i] = p
	}
	a.pausedMu.Unlock()
	a.installWriteErrorHook(id, t)
	a.spawnVerify(id, t)
	return id, nil
}

// spawnVerify tracks background verification for shutdown. Once an add has
// succeeded, metadata retrieval must outlive the request that added it.
func (a *AnacrolixBackend) spawnVerify(id TorrentID, t *torrent.Torrent) {
	_ = a.startVerification(id, t, func(ctx context.Context) { a.verifyAndStart(ctx, id, t, true) })
}

// A remove cancels and drains our verifier before closing the library's storage.
func (a *AnacrolixBackend) startVerification(id TorrentID, t *torrent.Torrent, task func(context.Context)) error {
	a.verifyMu.Lock()
	if a.verifyCancel[id] != nil {
		a.verifyMu.Unlock()
		return errors.New("verification already running")
	}
	if a.verifyCancel == nil {
		a.verifyCancel = make(map[TorrentID]context.CancelFunc)
		a.verifyDone = make(map[TorrentID]chan struct{})
	}
	ctx, cancel := context.WithCancel(a.engineCtx)
	done := make(chan struct{})
	a.verifyCancel[id] = cancel
	a.verifyDone[id] = done
	a.verifying[id] = true
	a.verifyWg.Add(1)
	a.verifyMu.Unlock()
	go func() {
		defer a.verifyWg.Done()
		task(ctx)
		a.mu.Lock()
		a.verifyMu.Lock()
		if a.byID[id] == t {
			delete(a.verifying, id)
		}
		delete(a.verifyCancel, id)
		delete(a.verifyDone, id)
		a.verifyMu.Unlock()
		a.mu.Unlock()
		cancel()
		close(done)
	}()
	return nil
}
func (a *AnacrolixBackend) stopVerification(id TorrentID) {
	a.verifyMu.RLock()
	cancel, done := a.verifyCancel[id], a.verifyDone[id]
	a.verifyMu.RUnlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// verifyAndStart hashes existing files (so resume picks up partials), then
// either kicks DownloadAll OR — if Service marked this torrent as previously
// complete and verify finds <100% — flags FilesMissing and pauses so the
// app doesn't silently redownload bytes the user just deleted.
func (a *AnacrolixBackend) verifyAndStart(ctx context.Context, id TorrentID, t *torrent.Torrent, allowResume bool) {
	// Wait for metainfo. AddFile already has it (channel is pre-closed);
	// AddMagnet has to fetch it via DHT/PEX before we can hash anything.
	// Listen on engineCtx too: anacrolix's gotMetainfoC is closed by
	// onSetInfo (when metainfo IS received), NOT by t.Drop / Client.Close,
	// so a magnet that never resolves would wedge the goroutine forever
	// and the engine's verifyWg.Wait at shutdown couldn't return until
	// the 5s drain timeout (or, on macOS at least, longer — Wails's
	// shutdown order kept the X-button click feeling like the app was
	// frozen). engineCtx.Done firing on Close drops these goroutines
	// promptly so verifyWg drains in milliseconds.
	select {
	case <-t.GotInfo():
	case <-t.Closed():
		return
	case <-ctx.Done():
		return
	}

	// Fast-resume: if we have a snapshot from a prior session and the
	// on-disk file state still matches, replay the saved per-piece
	// bitmap into bolt to undo anacrolix's setCompletionFromPartFiles
	// wipe (which fired during AddTorrentSpec for any partial torrent),
	// then skip the full re-hash. For complete torrents the bitmap
	// matches what bolt already has and the replay is a no-op; for
	// partials it's the difference between a stat-per-file fast resume
	// and reading every byte of a 13 GB preallocated .part to find the
	// 200 MB we actually have.
	//
	// If file digest doesn't match (user deleted/edited bytes
	// off-Mosaic between sessions), we fall through to verifyDataParallel
	// — the bitmap would be lying about what's on disk. Same fall-through
	// applies if no snapshot was ever saved (legacy or first-Add).
	if a.snapshotStore != nil && allowResume {
		if snap, wasComplete, bitmap, ok := a.snapshotStore.LoadPieceBitmap(id); ok {
			if bitmap == nil {
				log.Printf("verify: snapshot exists but no bitmap for %s — falling through to full hash", id)
			} else if info := t.Info(); info == nil {
				log.Printf("verify: snapshot exists but t.Info() is nil for %s — falling through to full hash", id)
			} else {
				saveTo := a.saveDirFor(id)
				cur, snapErr := computeFileSnapshot(info, saveTo)
				if snapErr != nil {
					log.Printf("verify: computeFileSnapshot error for %s: %v — falling through to full hash", id, snapErr)
				} else if !bytes.Equal(snap, cur) {
					log.Printf("verify: file-state mismatch for %s — files changed off-Mosaic; running full hash", id)
				} else {
					if wasComplete {
						// Complete torrent: setCompletionFromPartFiles already set all
						// pieces to complete in bolt (files exist at the final path with
						// the right size). No need to replay the bitmap — that would just
						// be 7000+ redundant bolt View transactions, each confirming what
						// bolt already knows. Skipping the replay keeps this path at
						// ~5ms (snapshot load + file stat) vs. 200-800ms.
						log.Printf("verify: fast-resume %s — skipping hash (complete, no bolt restore needed)", id)
						a.snapshotMu.Lock()
						a.snapshotSaved[id] = true
						a.snapshotMu.Unlock()
					} else {
						// Partial torrent: setCompletionFromPartFiles marked pieces for
						// incomplete files as "not complete" because their .part file is
						// not at the final path. Replay the saved bitmap so anacrolix
						// sees the actual per-piece state without re-hashing.
						restored := a.restoreBoltFromBitmap(t, bitmap)
						log.Printf("verify: fast-resume %s — skipping hash (partial, restored=%d pieces)", id, restored)
					}
					if ctx.Err() == nil {
						a.applyPostVerifyPriorities(id, t)
					}
					return
				}
			}
		} else {
			log.Printf("verify: no snapshot for %s — first-add or recheck; running full hash", id)
		}
	}

	verifyDataParallel(ctx, t)
	if ctx.Err() != nil {
		return
	}
	a.verifyMu.RLock()
	wasComplete := a.expectedComplete[id]
	a.verifyMu.RUnlock()

	// BytesMissing is the canonical "is anything still needed" check.
	if wasComplete && t.BytesMissing() > 0 {
		a.verifyMu.Lock()
		a.filesMissing[id] = true
		a.verifyMu.Unlock()
		// Pause the torrent so it doesn't silently redownload. User can hit
		// Resume to either replace missing files or remove the entry.
		a.pausedMu.Lock()
		a.paused[id] = true
		a.pausedMu.Unlock()
		t.SetMaxEstablishedConns(0)
		a.applyPostVerifyPriorities(id, t)
		return
	}

	// Now that the torrent is fully present, persist the snapshot so the
	// next startup can take the fast-resume path.
	a.saveSnapshotIfComplete(id, t)

	a.applyPostVerifyPriorities(id, t)
}

// applyPostVerifyPriorities restores piece priorities once verify finishes.
// SetSequential may have run while the verify was in flight; blindly resetting
// every file to Normal here would silently revert the sequential gradient, so
// re-apply it instead when the flag is set.
func (a *AnacrolixBackend) applyPostVerifyPriorities(id TorrentID, t *torrent.Torrent) {
	a.pausedMu.Lock()
	setAllFilesPriority(t, anacrolix_types.PiecePriorityNormal)
	for i, p := range a.filePriorities[id] {
		if i >= 0 && i < len(t.Files()) {
			t.Files()[i].SetPriority(prioToAnacrolix(p))
		}
	}
	applySequentialPriorities(t, a.sequential[id])
	if a.paused[id] {
		t.SetMaxEstablishedConns(0)
		a.pausedMu.Unlock()
		return
	}
	a.pausedMu.Unlock()
	if err := a.checkSelectedSpace(id, t); err != nil {
		a.raiseError(id, err)
		return
	}
	a.pausedMu.RLock()
	defer a.pausedMu.RUnlock()
	if !a.paused[id] {
		t.AllowDataDownload()
		t.AllowDataUpload()
	}
}

// setAllFilesPriority is the file-aware replacement for t.DownloadAll().
// DownloadAll raises piece priorities directly via DownloadPieces() but
// never touches File.prio, which means File.Priority() keeps returning
// PiecePriorityNone (which we surface as "Skip" in the Files pane). The
// piece-level effect is the same — anacrolix's purePriority() takes the
// max across each piece's overlapping files plus the per-piece raise —
// but iterating and SetPriority'ing each file lifts BOTH file.prio AND
// the underlying pieces, so the UI displays the correct file priority
// instead of misleading the user into thinking the files are skipped.
//
// Safe before t.GotInfo only if t.Files() returns the populated list; in
// practice all callers are past GotInfo by the time they hit this.
func setAllFilesPriority(t *torrent.Torrent, prio anacrolix_types.PiecePriority) {
	for _, f := range t.Files() {
		f.SetPriority(prio)
	}
}

// saveSnapshotIfComplete is the per-tick dedup'd path for completed
// torrents — once a torrent finishes downloading and the engine ticker
// observes the transition, this fires once and snapshotSaved quiesces
// it. Bitmap is recorded too so a fully-complete torrent's restart
// avoids touching disk at all.
func (a *AnacrolixBackend) saveSnapshotIfComplete(id TorrentID, t *torrent.Torrent) {
	a.snapshotMu.Lock()
	saved := a.snapshotSaved[id]
	a.snapshotMu.Unlock()
	if saved {
		return
	}
	if t.Info() == nil || t.BytesMissing() != 0 {
		return
	}
	if !a.writeSnapshot(id, t, true) {
		return
	}
	a.snapshotMu.Lock()
	a.snapshotSaved[id] = true
	a.snapshotMu.Unlock()
}

// saveSnapshotForCheckpoint records the file digest + per-piece bitmap
// at a quiesced point in the torrent's lifetime — typically pause or
// engine shutdown. Unlike saveSnapshotIfComplete it works for partial
// torrents too: the bitmap captures exactly which pieces bolt thinks
// are complete RIGHT NOW, and the next add can replay it after
// anacrolix's storage init wipes bolt for partials. Skips while verify
// is mid-flight (capturing a half-verified state would replay
// nonsense). wasComplete is recorded honestly.
func (a *AnacrolixBackend) saveSnapshotForCheckpoint(id TorrentID, t *torrent.Torrent) {
	if t.Info() == nil {
		return // BytesMissing requires metadata, which pending magnets do not have.
	}
	a.verifyMu.RLock()
	verifying := a.verifying[id]
	a.verifyMu.RUnlock()
	if verifying {
		return
	}
	complete := t.BytesMissing() == 0
	if !a.writeSnapshot(id, t, complete) {
		return
	}
	if complete {
		a.snapshotMu.Lock()
		a.snapshotSaved[id] = true
		a.snapshotMu.Unlock()
	}
}

// writeSnapshot is the shared implementation: compute the file-state
// digest + per-piece completion bitmap, persist them via the snapshot
// store atomically. The bitmap is what lets the next launch avoid the
// brutal "verify all 13 GB to find the 200 MB we have" rehash —
// anacrolix's setCompletionFromPartFiles wipes bolt's complete entries
// for any partial-with-.part-files torrent, but if the file digest
// matches what we recorded with the bitmap, we know the disk state
// hasn't changed off-Mosaic and can replay the bitmap straight back
// into bolt to restore anacrolix's per-piece view in one cheap loop.
//
// Falls back to plain snapshot save (no bitmap) if reading per-piece
// completion fails — the legacy verify path still covers correctness.
func (a *AnacrolixBackend) writeSnapshot(id TorrentID, t *torrent.Torrent, complete bool) bool {
	if a.snapshotStore == nil {
		return false
	}
	info := t.Info()
	if info == nil {
		return false
	}
	saveTo := a.saveDirFor(id)
	cur, err := computeFileSnapshot(info, saveTo)
	if err != nil {
		log.Printf("verify: compute snapshot for %s: %v", id, err)
		return false
	}
	bitmap := piecesToBitmap(t)
	if err := a.snapshotStore.SavePieceBitmap(id, cur, complete, bitmap); err != nil {
		log.Printf("verify: save snapshot/bitmap for %s: %v", id, err)
		return false
	}
	return true
}

// piecesToBitmap reads anacrolix's per-piece completion view via
// PieceState and packs it into a `(NumPieces+7)/8`-byte bitmap. Bit i
// is set iff piece i is complete. Reading PieceState is cheap (no
// disk I/O — it's a memory read off the cached completion state).
func piecesToBitmap(t *torrent.Torrent) []byte {
	n := t.NumPieces()
	if n <= 0 {
		return nil
	}
	out := make([]byte, (n+7)/8)
	for i := 0; i < n; i++ {
		if t.PieceState(i).Complete {
			out[i/8] |= 1 << (uint(i) & 7)
		}
	}
	return out
}

// restoreBoltFromBitmap replays a saved per-piece completion bitmap
// into anacrolix's bolt store. Called after AddTorrentSpec runs (which
// invokes setCompletionFromPartFiles, wiping any "complete" bolt
// entries for partial torrents) but before verifyAndStart kicks off —
// the next time anacrolix consults bolt for piece N, it sees the
// restored truth and storageCompletionOk flips to true so the piece
// becomes eligible for requests immediately, no re-hash needed.
//
// Caller is responsible for verifying the file-state snapshot matches
// before calling this; otherwise we'd be lying to anacrolix about what
// bytes are actually on disk.
func (a *AnacrolixBackend) restoreBoltFromBitmap(t *torrent.Torrent, bitmap []byte) int {
	if a.pieceCompletion == nil || len(bitmap) == 0 {
		return 0
	}
	n := t.NumPieces()
	ih := t.InfoHash()
	restored := 0
	for i := 0; i < n; i++ {
		byteIdx := i / 8
		if byteIdx >= len(bitmap) {
			break
		}
		complete := bitmap[byteIdx]&(1<<(uint(i)&7)) != 0
		if !complete {
			// We don't restore "incomplete" — bolt's default state for
			// missing entries is already storageCompletionOk=false /
			// piece-not-complete. Setting false explicitly would just
			// add a row per piece for no benefit. Pieces marked
			// complete are the only ones worth replaying.
			continue
		}
		if err := a.pieceCompletion.Set(metainfo.PieceKey{InfoHash: ih, Index: i}, true); err != nil {
			log.Printf("verify: restore piece %d for %s: %v", i, idFor(t), err)
			continue
		}
		restored++
	}
	return restored
}

// saveDirFor returns the per-torrent save root captured at AddFile/AddMagnet
// time (same value passed to anacrolix's storage.NewFile). Files for the
// torrent live at filepath.Join(saveDir, info.Name, file.Path...).
func (a *AnacrolixBackend) saveDirFor(id TorrentID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bySaveTo[id]
}

// verifyDataParallel hashes every piece of t, but unlike t.VerifyData()
// (which iterates pieces sequentially and blocks on each one), it dispatches
// up to `workers` outer goroutines so the lib's piece-hasher worker pool
// stays saturated. anacrolix's t.VerifyData has a TODO admitting the
// sequential design; queuing-1-and-waiting-1 means PieceHashersPerTorrent
// > 1 buys us nothing. v0.2.10 used NumCPU/2 capped at 8 here, but the
// client locker contention inside anacrolix means more outer goroutines
// just spin without throughput — so v0.2.11 caps at NumCPU/4 (2..4) to
// match PieceHashersPerTorrent in NewAnacrolixBackend.
func verifyDataParallel(ctx context.Context, t *torrent.Torrent) {
	n := t.NumPieces()
	if n == 0 {
		return
	}
	workers := goruntime.NumCPU() / 4
	if workers < 2 {
		workers = 2
	}
	if workers > 4 {
		workers = 4
	}
	if workers > n {
		workers = n
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			piece := t.Piece(idx)
			if err := piece.VerifyDataContext(ctx); err != nil {
				return
			}
			// VerifyDataContext can return while MarkComplete/MarkNotComplete is still writing.
			for state := piece.State(); state.Marking || state.Hashing || state.QueuedForHash; state = piece.State() {
				select {
				case <-ctx.Done():
					return
				case <-t.Closed():
					return
				case <-time.After(time.Millisecond):
				}
			}
		}(i)
	}
	wg.Wait()
}

// MarkExpectedComplete marks this torrent as having been 100% on a prior
// session, so verifyAndStart can flag FilesMissing if VerifyData reveals
// the user deleted files. Service.RestoreOnStartup calls this when the
// persisted record's CompletedAt is non-nil.
func (a *AnacrolixBackend) MarkExpectedComplete(id TorrentID) {
	a.verifyMu.Lock()
	a.expectedComplete[id] = true
	a.verifyMu.Unlock()
}

func (a *AnacrolixBackend) AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error) {
	return a.Add(ctx, AddRequest{TorrentFile: blob, SavePath: savePath})
}

func (a *AnacrolixBackend) Pause(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	// Two-step pause:
	//   1. DisallowDataDownload — stops anacrolix from issuing piece
	//      requests, even if peer conns come back up. This is the
	//      load-bearing guarantee.
	//   2. SetMaxEstablishedConns(0) — drops existing peer conns and
	//      refuses new ones, so we're not accepting/uploading either.
	//
	// Pre-fix Pause only did #2, which has a known race against the
	// scheduler tick: scheduler reads List() at t=0, user clicks Pause
	// at t=10ms (paused flag flips, conns drop), scheduler iterates the
	// stale snapshot and calls ScheduledPause(id, false) →
	// SetMaxEstablishedConns(80), peers reconnect, anacrolix happily
	// resumes piece requests because the priority lift is still in place.
	// User sees a paused torrent that's still downloading. Disallow
	// closes the gap regardless of conn-cap thrashing.
	a.pausedMu.Lock()
	a.paused[id] = true
	t.DisallowDataDownload()
	t.DisallowDataUpload()
	t.SetMaxEstablishedConns(0)
	a.pausedMu.Unlock()
	// Checkpoint the file state + per-piece bitmap. The bitmap is what
	// makes fast-resume work for partial torrents post-anacrolix's
	// per-add storage init wipe — the next add will replay it back into
	// bolt after the wipe so anacrolix sees the actual disk state
	// without re-hashing every piece.
	a.saveSnapshotForCheckpoint(id, t)
	return nil
}

func (a *AnacrolixBackend) Resume(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	a.verifyMu.RLock()
	verifying := a.verifying[id]
	a.verifyMu.RUnlock()
	if t.Info() != nil && !verifying {
		if err := a.checkSelectedSpace(id, t); err != nil {
			return err
		}
	}
	a.pausedMu.Lock()
	a.paused[id] = false
	delete(a.scheduledPause, id)
	t.SetMaxEstablishedConns(int(a.maxConnsPerTorrent.Load()))
	// Resume is explicit permission to transfer; file priorities remain gated by verification.
	t.AllowDataDownload()
	t.AllowDataUpload()
	a.pausedMu.Unlock()
	// If this resume comes after a FilesMissing flag (user wants to redownload
	// after files were deleted), clear the flag — VerifyData will rerun if
	// new pieces fail.
	a.verifyMu.Lock()
	delete(a.filesMissing, id)
	a.verifyMu.Unlock()
	// Resume is also the recovery path after a disk-full / write-error
	// stoppage: AllowDataDownload clears the dataDownloadDisallowed flag
	// our write-error hook (and anacrolix's storage-completion error
	// path) sets. Idempotent if it was never disallowed.
	return nil
}

// ApplyPerTorrentMaxPeers updates SetMaxEstablishedConns on every running
// torrent. Used when the user changes the "max peers per torrent" setting.
// Already-paused torrents keep their cap of 0 (we set it to 0 to pause);
// the new cap is stored in maxConnsPerTorrent so Resume (and the
// scheduler's re-enable path) restore it instead of a hardcoded default.
func (a *AnacrolixBackend) ApplyPerTorrentMaxPeers(n int) error {
	if n <= 0 {
		n = 80 // anacrolix default
	}
	a.maxConnsPerTorrent.Store(int64(n))
	a.pausedMu.RLock()
	paused := make(map[TorrentID]bool, len(a.paused))
	for id, v := range a.paused {
		paused[id] = v
	}
	a.pausedMu.RUnlock()
	for _, t := range a.client.Torrents() {
		id := TorrentID(t.InfoHash().HexString())
		if paused[id] {
			continue
		}
		t.SetMaxEstablishedConns(n)
	}
	return nil
}

// Recheck re-hashes every piece against the metainfo. Surfaced via the
// Recheck context-menu item in the SPA. Runs on a goroutine — large torrents
// take a while; while running the Verifying pill shows in the UI.
//
// We invalidate the fast-resume snapshot up front: a Recheck means the user
// wants the verify to actually run, and once it finishes we'll write a new
// snapshot from the just-verified state.
func (a *AnacrolixBackend) Recheck(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	if a.snapshotStore != nil {
		if err := a.snapshotStore.DeleteVerifySnapshot(id); err != nil {
			log.Printf("verify: delete snapshot for %s: %v", id, err)
		}
	}
	// Clear the in-memory dedup so saveSnapshotIfComplete re-runs after the
	// recheck finishes (it's the same write the original goroutine did
	// inline pre-v0.4.4). The retry-backoff stamp goes too — a user-driven
	// recheck should attempt the save immediately.
	a.snapshotMu.Lock()
	delete(a.snapshotSaved, id)
	delete(a.snapshotRetryAt, id)
	a.snapshotMu.Unlock()
	return a.startVerification(id, t, func(ctx context.Context) {
		verifyDataParallel(ctx, t)
		if ctx.Err() == nil {
			a.saveSnapshotIfComplete(id, t)
		}
	})
}

func (a *AnacrolixBackend) Remove(id TorrentID, deleteFiles bool) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	saveTo := a.saveDirFor(id)
	var deletePaths []string
	if deleteFiles && t.Info() != nil {
		if err := sanityCheckSaveTo(saveTo); err != nil {
			return err
		}
		for _, name := range contentPaths(t.Info().BestName(), t.Info().IsDir()) {
			path, err := safeRemovePath(saveTo, name)
			if err != nil {
				return err
			}
			deletePaths = append(deletePaths, path)
		}
	}
	a.stopVerification(id)
	if _, err := a.quiesceSnapshots(a.engineCtx, id); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.bySaveTo, id)
	delete(a.byID, id)
	a.mu.Unlock()
	a.pausedMu.Lock()
	delete(a.filePriorities, id)
	delete(a.paused, id)
	delete(a.queuePos, id)
	delete(a.forceStart, id)
	delete(a.sequential, id)
	delete(a.scheduledPause, id)
	a.pausedMu.Unlock()
	a.verifyMu.Lock()
	delete(a.verifying, id)
	delete(a.expectedComplete, id)
	delete(a.filesMissing, id)
	a.verifyMu.Unlock()
	a.snapshotMu.Lock()
	delete(a.snapshotSaved, id)
	delete(a.snapshotRetryAt, id)
	a.snapshotMu.Unlock()
	a.rateMu.Lock()
	delete(a.prevRates, id)
	delete(a.prevPeerRates, id)
	a.rateMu.Unlock()
	// Clear per-torrent limits so the limiter goroutine exits on next tick.
	a.perLimitMu.Lock()
	delete(a.perTorrentDown, id)
	delete(a.perTorrentUp, id)
	a.perLimitMu.Unlock()
	// t.Drop holds the client lock while it tears the torrent down, and
	// inside that the cleanup waits on a per-torrent wait group that
	// covers (among other things) tracker stop announces. A
	// tracker that's hanging on a TCP connect (HTTP-only trackers like
	// the checkmyiptorrent variant the user hit) can hold this for as
	// long as the OS connect timeout, blocking the UI's "Remove" call.
	// Cap at 5s and let anacrolix finish on its own — our in-memory
	// state has already been cleaned, the user has already been told
	// "removed."
	dropDone := make(chan struct{})
	go func() {
		t.Drop()
		close(dropDone)
	}()
	select {
	case <-dropDone:
	case <-time.After(5 * time.Second):
		log.Printf("remove: t.Drop timeout for %s — proceeding without waiting", id)
	}
	if deleteFiles {
		// Wait for storage closure even if tracker shutdown takes longer than five seconds.
		<-dropDone
		for _, path := range deletePaths {
			if err := os.RemoveAll(path); err != nil {
				return &RemoveFilesError{Err: err}
			}
		}
	}

	return nil
}

func (a *AnacrolixBackend) List() []Snapshot {
	ts := a.client.Torrents()
	out := make([]Snapshot, 0, len(ts))
	// Collect newly-completed ids inside the locked region; persist their
	// fast-resume snapshots in goroutines after the locks are released so
	// the file I/O doesn't block the tick. spawnSnapshotSave dedupes against
	// snapshotSaved / the in-flight guard, so subsequent ticks observe
	// completion as a no-op until Recheck or Remove clears the flag.
	var completed []completedFor
	// Rates come from the centralized sampler's immutable cache — this loop
	// only reads, never mutates prevRates, so concurrent List() calls (one
	// per connected user, back-to-back, every tick) all see identical rates.
	rates := a.rateCache.Load()
	a.pausedMu.RLock()
	a.verifyMu.RLock()
	for _, t := range ts {
		id := TorrentID(t.InfoHash().HexString())
		var rate rateValue
		if rates != nil {
			rate = (*rates)[id]
		}
		snap := snapshotFor(t, rate, a.paused[id], a.queuePos[id], a.forceStart[id], a.sequential[id], a.scheduledPause[id], a.verifying[id], a.filesMissing[id])
		out = append(out, snap)
		if snap.Completed && a.snapshotStore != nil {
			completed = append(completed, completedFor{id: id, t: t})
		}
	}
	a.verifyMu.RUnlock()
	a.pausedMu.RUnlock()
	for _, c := range completed {
		a.spawnSnapshotSave(c.id, c.t)
	}
	return out
}

// snapshotSaveRetryBackoff is how long spawnSnapshotSave waits after a failed
// snapshot write before trying that torrent again.
const snapshotSaveRetryBackoff = 30 * time.Second

// spawnSnapshotSave runs saveSnapshotIfComplete on a goroutine, at most one
// in flight per torrent. List() used to `go saveSnapshotIfComplete(...)` for
// every completed torrent on every tick; snapshotSaved is only set on a
// successful write, so a persistently failing snapshot store spawned N
// goroutines several times per second forever, each taking the client lock.
// Failures back off for snapshotSaveRetryBackoff before the next attempt.
func (a *AnacrolixBackend) spawnSnapshotSave(id TorrentID, t *torrent.Torrent) {
	a.mu.Lock()
	if a.byID[id] != t {
		a.mu.Unlock()
		return
	}
	a.snapshotMu.Lock()
	a.mu.Unlock()
	if a.snapshotSaved[id] || a.snapshotInflight[id] || time.Now().Before(a.snapshotRetryAt[id]) {
		a.snapshotMu.Unlock()
		return
	}
	a.snapshotInflight[id] = true
	a.snapshotMu.Unlock()
	go func() {
		a.saveSnapshotIfComplete(id, t)
		a.snapshotMu.Lock()
		delete(a.snapshotInflight, id)
		if a.snapshotSaved[id] {
			delete(a.snapshotRetryAt, id)
		} else {
			a.snapshotRetryAt[id] = time.Now().Add(snapshotSaveRetryBackoff)
		}
		a.snapshotMu.Unlock()
	}()
}

type completedFor struct {
	id TorrentID
	t  *torrent.Torrent
}

func (a *AnacrolixBackend) Snapshot(id TorrentID) (Snapshot, error) {
	t, ok := a.find(id)
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	a.pausedMu.RLock()
	paused := a.paused[id]
	queuePos := a.queuePos[id]
	forceStart := a.forceStart[id]
	sequential := a.sequential[id]
	queued := a.scheduledPause[id]
	a.pausedMu.RUnlock()
	a.verifyMu.RLock()
	verifying := a.verifying[id]
	filesMissing := a.filesMissing[id]
	a.verifyMu.RUnlock()
	snap := snapshotFor(t, a.rateFor(id), paused, queuePos, forceStart, sequential, queued, verifying, filesMissing)
	return snap, nil
}

func (a *AnacrolixBackend) SetFilePriorities(id TorrentID, prios map[int]Priority) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	files := t.Files()
	for idx, p := range prios {
		if idx < 0 || idx >= len(files) || p < PrioritySkip || p > PriorityMax {
			return errors.New("invalid file priority or index")
		}
	}
	a.pausedMu.Lock()
	if a.filePriorities[id] == nil {
		a.filePriorities[id] = make(map[int]Priority)
	}
	for idx, p := range prios {
		a.filePriorities[id][idx] = p
		files[idx].SetPriority(prioToAnacrolix(p))
	}
	applySequentialPriorities(t, a.sequential[id])
	a.pausedMu.Unlock()
	return nil
}

func prioToAnacrolix(p Priority) anacrolix_types.PiecePriority {
	switch p {
	case PrioritySkip:
		return anacrolix_types.PiecePriorityNone
	case PriorityHigh:
		return anacrolix_types.PiecePriorityHigh
	case PriorityMax:
		return anacrolix_types.PiecePriorityNow
	}
	return anacrolix_types.PiecePriorityNormal
}

// RemoveFilesError means the torrent stopped, but some downloaded data remains.
type RemoveFilesError struct{ Err error }

func (e *RemoveFilesError) Error() string {
	return "torrent removed, but deleting files failed: " + e.Err.Error()
}
func (e *RemoveFilesError) Unwrap() error { return e.Err }

func (a *AnacrolixBackend) checkSelectedSpace(id TorrentID, t *torrent.Torrent) error {
	var needed int64
	for _, f := range t.Files() {
		if f.Priority() != anacrolix_types.PiecePriorityNone {
			needed += f.Length() - f.BytesCompleted()
		}
	}
	if free := FreeSpace(a.saveDirFor(id)); needed > 0 && free >= 0 && free < needed+(64<<20) {
		return fmt.Errorf("insufficient disk space: %d bytes free, need %d (+64 MB headroom)", free, needed)
	}
	return nil
}

func (a *AnacrolixBackend) quiesceSnapshots(ctx context.Context, id TorrentID) (wasSaved bool, err error) {
	a.snapshotMu.Lock()
	wasSaved = a.snapshotSaved[id]
	a.snapshotSaved[id] = true
	a.snapshotMu.Unlock()
	for {
		a.snapshotMu.Lock()
		busy := a.snapshotInflight[id]
		a.snapshotMu.Unlock()
		if !busy {
			return wasSaved, nil
		}
		select {
		case <-ctx.Done():
			a.snapshotMu.Lock()
			a.snapshotSaved[id] = wasSaved
			a.snapshotMu.Unlock()
			return wasSaved, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
