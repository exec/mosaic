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
	// SECURITY: savePath is caller-controlled. In multi-user mosaicd a
	// non-admin must NOT be able to MkdirAll into arbitrary daemon-writable
	// paths (/etc/cron.d, ~root/.ssh, another user's home). Callers in
	// api/service.go must funnel non-admin save paths through
	// engine.ValidateSavePath(userRoot, savePath, /*restrictToRoot=*/ true)
	// BEFORE invoking AddMagnet. Admin and single-user-desktop callers
	// pass restrictToRoot=false to keep the current unrestricted behavior.
	// This function intentionally does not re-validate because the engine
	// doesn't know the caller's role/userID — that context lives in api/.
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return "", err
	}
	spec.Storage = newFileStorage(savePath, a.pieceCompletion, a.preallocateFullFiles)
	t, _, err := a.client.AddTorrentSpec(spec)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.byID[id] = t
	a.mu.Unlock()
	a.installWriteErrorHook(id, t)
	a.spawnVerify(ctx, id, t)
	return id, nil
}

// spawnVerify runs verifyAndStart on a tracked goroutine so Close() can
// wait for it to finish before tearing down the client + bolt. The
// caller's ctx still governs the pre-GotInfo wait (so a Wails shutdown
// or a slow magnet add can be cancelled during DHT lookup); past
// GotInfo verifyAndStart switches to engineCtx, which Close() cancels.
func (a *AnacrolixBackend) spawnVerify(ctx context.Context, id TorrentID, t *torrent.Torrent) {
	a.verifyWg.Add(1)
	go func() {
		defer a.verifyWg.Done()
		a.verifyAndStart(ctx, id, t)
	}()
}

// verifyAndStart hashes existing files (so resume picks up partials), then
// either kicks DownloadAll OR — if Service marked this torrent as previously
// complete and verify finds <100% — flags FilesMissing and pauses so the
// app doesn't silently redownload bytes the user just deleted.
//
// The caller's ctx is honored ONLY for the pre-GotInfo wait (so a Wails
// shutdown / HTTP-handler return / RSS-poll-cycle end cancels DHT lookup
// promptly). Past GotInfo we switch to context.Background() — verify and
// priority-lift must complete regardless of who originated the Add, or
// the torrent silently never downloads. Pre-fix: every Add path threaded
// the caller's request-scoped ctx all the way through; HTTP handlers,
// RSS PollNow, and any second-instance IPC dispatch returned before
// verify finished, the cancelled-ctx check before setAllFilesPriority
// bailed, file priorities stayed at PiecePriorityNone, and peers
// connected but no pieces were ever requested. The Wails GUI happened
// to dodge it because app.go threads the long-lived a.ctx.
// Anacrolix's own goroutines are cancelled on Client.Close (which
// Engine.Close calls), so Background ctx is shutdown-safe.
func (a *AnacrolixBackend) verifyAndStart(ctx context.Context, id TorrentID, t *torrent.Torrent) {
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
	case <-ctx.Done():
		return
	case <-a.engineCtx.Done():
		return
	}
	// Past GotInfo — switch from the caller's ctx (which an HTTP handler
	// or RSS poller will cancel as soon as the call returns) to the
	// engine's lifetime ctx (which Close() cancels). Verify must outlive
	// any specific request, but it MUST NOT outlive the engine — Close
	// closes the bolt + anacrolix client, and an in-flight verify dispatch
	// would crash or hang otherwise.
	ctx = a.engineCtx

	a.setVerifying(id, true)
	defer a.setVerifying(id, false)

	// Disk-space precheck. Once metadata is in we know how many bytes
	// the torrent will need; if the save path's filesystem can't fit
	// what's missing (BytesMissing already discounts what's on disk),
	// fail fast with a clear error rather than letting anacrolix start
	// requesting and surfacing ENOSPC chunk-write errors mid-flight.
	// 64 MB headroom for filesystem overhead and a margin against
	// concurrent writers; we'd rather error a borderline torrent than
	// silently disable it after consuming the last few hundred MB.
	if info := t.Info(); info != nil {
		if needed := t.BytesMissing(); needed > 0 {
			if free := diskFreeBytes(a.saveDirFor(id)); free >= 0 && free < needed+(64<<20) {
				t.DisallowDataDownload()
				a.raiseError(id, fmt.Errorf("insufficient disk space: %d bytes free, need %d (+64 MB headroom) for %q",
					free, needed, info.Name))
				return
			}
		}
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
	if a.snapshotStore != nil {
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
	a.pausedMu.RLock()
	sequential := a.sequential[id]
	a.pausedMu.RUnlock()
	if sequential {
		applySequentialPriorities(t, true)
		return
	}
	setAllFilesPriority(t, anacrolix_types.PiecePriorityNormal)
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
	if t.BytesMissing() != 0 {
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
			_ = t.Piece(idx).VerifyDataContext(ctx)
		}(i)
	}
	wg.Wait()
}

func (a *AnacrolixBackend) setVerifying(id TorrentID, v bool) {
	a.verifyMu.Lock()
	if v {
		a.verifying[id] = true
	} else {
		delete(a.verifying, id)
	}
	a.verifyMu.Unlock()
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
	// SECURITY: same caller-controlled-path concern as AddMagnet. See the
	// note there. The api/service.go AddTorrentFile / AddTorrentBytes paths
	// are expected to ValidateSavePath before calling in.
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	mi, err := metainfo.Load(bytes.NewReader(blob))
	if err != nil {
		return "", err
	}
	spec := torrent.TorrentSpecFromMetaInfo(mi)
	spec.Storage = newFileStorage(savePath, a.pieceCompletion, a.preallocateFullFiles)
	t, _, err := a.client.AddTorrentSpec(spec)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.byID[id] = t
	a.mu.Unlock()
	a.installWriteErrorHook(id, t)
	// Same verify-then-start flow as AddMagnet — the GotInfo wait inside
	// verifyAndStart is a no-op here since metainfo is already attached.
	a.spawnVerify(ctx, id, t)
	return id, nil
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
	t.DisallowDataDownload()
	t.SetMaxEstablishedConns(0)
	a.pausedMu.Lock()
	a.paused[id] = true
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
	t.SetMaxEstablishedConns(int(a.maxConnsPerTorrent.Load()))
	a.pausedMu.Lock()
	a.paused[id] = false
	delete(a.scheduledPause, id)
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
	t.AllowDataDownload()
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
	a.setVerifying(id, true)
	a.verifyWg.Add(1)
	go func() {
		defer a.verifyWg.Done()
		defer a.setVerifying(id, false)
		verifyDataParallel(a.engineCtx, t)
		// Don't snapshot if the engine is shutting down — saveSnapshotIfComplete
		// writes to the snapshot store, which Close will tear down right after
		// it cancels engineCtx and waits on verifyWg.
		if a.engineCtx.Err() != nil {
			return
		}
		a.saveSnapshotIfComplete(id, t)
	}()
	return nil
}

func (a *AnacrolixBackend) Remove(id TorrentID, deleteFiles bool) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	a.mu.Lock()
	saveTo := a.bySaveTo[id]
	delete(a.bySaveTo, id)
	delete(a.byID, id)
	a.mu.Unlock()
	a.pausedMu.Lock()
	delete(a.paused, id)
	delete(a.queuePos, id)
	delete(a.forceStart, id)
	delete(a.sequential, id)
	delete(a.scheduledPause, id)
	a.pausedMu.Unlock()
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
	if deleteFiles && saveTo != "" {
		if info := t.Info(); info != nil {
			// Path-traversal defense: a malicious .torrent's info.Name can be
			// "../../something" — joining it raw would let RemoveAll walk above
			// saveTo and delete arbitrary files. safeRemovePath validates
			// containment (resolving symlinks on both ends when present) and
			// returns an error we log+skip on. The in-memory unsubscribe
			// (t.Drop, the map deletes above) has already happened — only the
			// disk delete is conditional on validation.
			//
			// Additional defense-in-depth: saveTo itself was caller-controlled
			// at Add time. In multi-user mode the api/ layer is expected to
			// validate it via engine.ValidateSavePath before AddMagnet/AddFile,
			// but we sanity-check here too — a saveTo that's empty, relative,
			// contains NUL, or resolves to a filesystem root must not have
			// RemoveAll fired on it under any circumstances.
			if err := sanityCheckSaveTo(saveTo); err != nil {
				log.Printf("refusing to delete files for torrent: saveTo=%q error=%v", saveTo, err)
			} else {
				path, err := safeRemovePath(saveTo, info.Name)
				if err != nil {
					log.Printf("refusing to delete files for torrent: name=%q error=%v", info.Name, err)
				} else {
					_ = os.RemoveAll(path)
				}
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
	a.snapshotMu.Lock()
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
		if idx < 0 || idx >= len(files) {
			continue
		}
		files[idx].SetPriority(prioToAnacrolix(p))
	}
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

