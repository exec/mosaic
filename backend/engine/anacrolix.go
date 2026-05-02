package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	goruntime "runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	anacrolix_types "github.com/anacrolix/torrent/types"
	"golang.org/x/time/rate"
)

// ipBlocklistProxy is a swappable iplist.Ranger we install once at client
// construction. anacrolix v1.61 has no runtime setter for IPBlocklist; the
// Client copies cfg.IPBlocklist into an unexported field. By installing this
// proxy at construction and mutating its inner ranger via atomic.Pointer, we
// can swap blocklists at runtime without forking anacrolix.
type ipBlocklistProxy struct {
	inner atomic.Pointer[iplist.IPList]
}

func (p *ipBlocklistProxy) Lookup(ip net.IP) (iplist.Range, bool) {
	r := p.inner.Load()
	if r == nil {
		return iplist.Range{}, false
	}
	return r.Lookup(ip)
}

func (p *ipBlocklistProxy) NumRanges() int {
	r := p.inner.Load()
	if r == nil {
		return 0
	}
	return r.NumRanges()
}

// AnacrolixConfig configures the production Backend.
type AnacrolixConfig struct {
	DataDir          string // engine state dir (peer cache, etc.)
	ListenPort       int
	EnableDHT        bool
	EnableEncryption bool
	// MaxPeersPerTorrent is the initial per-torrent established-conn cap
	// applied to every new torrent on add and (via ApplyPerTorrentMaxPeers)
	// to all running torrents when the user changes the setting at runtime.
	// 0 = use anacrolix's per-torrent default (80). anacrolix v1.61 doesn't
	// expose a separate global cap; effective global ceiling is this value
	// multiplied by the number of active torrents.
	MaxPeersPerTorrent int
	// SnapshotStore is an optional fast-resume hook. When non-nil, the engine
	// consults it on startup-add to decide whether to skip the full
	// piece-by-piece verify when the on-disk file state is unchanged since
	// shutdown. nil-safe: the optimization just turns off.
	SnapshotStore SnapshotStore
}

// AnacrolixBackend implements Backend on top of anacrolix/torrent.
type AnacrolixBackend struct {
	client *torrent.Client

	mu       sync.Mutex
	bySaveTo map[TorrentID]string // id → save path (we set it per-torrent)

	rateMu    sync.Mutex
	prevRates map[TorrentID]rateSample

	// pausedMu guards paused, queuePos, forceStart, scheduledPause. We extend
	// the existing read-mostly mutex rather than introducing a new one — these
	// maps are all read together by snapshotFor and written through the same
	// per-torrent setters, so a single RWMutex keeps the invariants simple.
	pausedMu       sync.RWMutex
	paused         map[TorrentID]bool
	queuePos       map[TorrentID]int
	forceStart     map[TorrentID]bool
	scheduledPause map[TorrentID]bool

	// Verify state — verifying[id] is true while VerifyData is hashing pieces;
	// expectedComplete[id] is set by Service when restoring a previously-100%
	// torrent so we can flag filesMissing[id] if VerifyData turns up <100%.
	// All three guarded by verifyMu so snapshotFor reads them atomically.
	verifyMu         sync.RWMutex
	verifying        map[TorrentID]bool
	expectedComplete map[TorrentID]bool
	filesMissing     map[TorrentID]bool

	// Rate limiters owned via ClientConfig. The same *rate.Limiter pointers are
	// stashed here so SetGlobalRateLimits can mutate them in place via
	// SetLimit/SetBurst (anacrolix v1.61 has no setter on the Client itself).
	dlLim *rate.Limiter
	ulLim *rate.Limiter

	ipBlock *ipBlocklistProxy

	// snapshotStore is the optional fast-resume hook. nil disables the
	// optimization. See SnapshotStore for the lifecycle.
	snapshotStore SnapshotStore
}

type rateSample struct {
	at   time.Time
	down int64
	up   int64
}

// NewAnacrolixBackend opens a torrent.Client with our config.
func NewAnacrolixBackend(cfg AnacrolixConfig) (*AnacrolixBackend, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	tcfg := torrent.NewDefaultClientConfig()
	tcfg.DataDir = cfg.DataDir
	tcfg.ListenPort = cfg.ListenPort
	tcfg.NoDHT = !cfg.EnableDHT
	// Continue uploading once a torrent finishes downloading. Anacrolix's
	// default is "altruism off": it uploads tit-for-tat WHILE downloading
	// (to encourage peers to reciprocate) but stops once we have nothing
	// to gain. For a desktop BitTorrent client that's the wrong default —
	// every other client (qBittorrent, Transmission, Deluge…) seeds by
	// default. Without this flag, completed torrents in Mosaic appear in
	// the "Seeding" tab but never actually push bytes to peers.
	// See anacrolix/torrent torrent.go's `seeding()` gate.
	tcfg.Seed = true
	if cfg.MaxPeersPerTorrent > 0 {
		tcfg.EstablishedConnsPerTorrent = cfg.MaxPeersPerTorrent
	}
	// Default is 2 — caps how many pieces hash in parallel per torrent.
	// v0.2.10 bumped this to NumCPU/2 (capped 8) but in practice anacrolix's
	// client-locker contention means 8 hashers gives the same wall-time as
	// 2 while generating noticeably more heat. Cap at NumCPU/4 with a 2..4
	// floor/ceiling — past that the outer goroutines just spin on the lock.
	// Pairs with the parallel dispatch loop in verifyAndStart.
	hashers := goruntime.NumCPU() / 4
	if hashers < 2 {
		hashers = 2
	}
	if hashers > 4 {
		hashers = 4
	}
	tcfg.PieceHashersPerTorrent = hashers
	if cfg.EnableEncryption {
		tcfg.HeaderObfuscationPolicy.Preferred = true
		tcfg.HeaderObfuscationPolicy.RequirePreferred = false
	} else {
		tcfg.HeaderObfuscationPolicy.Preferred = false
	}
	tcfg.DefaultStorage = storage.NewFile(cfg.DataDir)

	dlLim := rate.NewLimiter(rate.Inf, 256<<10)
	ulLim := rate.NewLimiter(rate.Inf, 256<<10)
	tcfg.DownloadRateLimiter = dlLim
	tcfg.UploadRateLimiter = ulLim

	ipBlock := &ipBlocklistProxy{}
	tcfg.IPBlocklist = ipBlock

	c, err := torrent.NewClient(tcfg)
	if err != nil {
		return nil, fmt.Errorf("anacrolix client: %w", err)
	}
	return &AnacrolixBackend{
		client:         c,
		bySaveTo:         make(map[TorrentID]string),
		prevRates:        make(map[TorrentID]rateSample),
		paused:           make(map[TorrentID]bool),
		queuePos:         make(map[TorrentID]int),
		forceStart:       make(map[TorrentID]bool),
		scheduledPause:   make(map[TorrentID]bool),
		verifying:        make(map[TorrentID]bool),
		expectedComplete: make(map[TorrentID]bool),
		filesMissing:     make(map[TorrentID]bool),
		dlLim:          dlLim,
		ulLim:          ulLim,
		ipBlock:        ipBlock,
		snapshotStore:  cfg.SnapshotStore,
	}, nil
}

// SetIPBlocklist parses a PeerGuardian-format reader and installs the resulting
// IPList as the active block list. Passing nil clears it.
func (a *AnacrolixBackend) SetIPBlocklist(reader io.Reader) error {
	if reader == nil {
		a.ipBlock.inner.Store(nil)
		return nil
	}
	list, err := iplist.NewFromReader(reader)
	if err != nil {
		return err
	}
	a.ipBlock.inner.Store(list)
	return nil
}

func idFor(t *torrent.Torrent) TorrentID {
	return TorrentID(t.InfoHash().HexString())
}

func (a *AnacrolixBackend) AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error) {
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil {
		return "", err
	}
	spec.Storage = storage.NewFile(savePath)
	t, _, err := a.client.AddTorrentSpec(spec)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	go a.verifyAndStart(ctx, id, t)
	return id, nil
}

// verifyAndStart hashes existing files (so resume picks up partials), then
// either kicks DownloadAll OR — if Service marked this torrent as previously
// complete and verify finds <100% — flags FilesMissing and pauses so the
// app doesn't silently redownload bytes the user just deleted.
func (a *AnacrolixBackend) verifyAndStart(ctx context.Context, id TorrentID, t *torrent.Torrent) {
	// Wait for metainfo. AddFile already has it (channel is pre-closed);
	// AddMagnet has to fetch it via DHT/PEX before we can hash anything.
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return
	}

	a.setVerifying(id, true)
	defer a.setVerifying(id, false)

	// Fast-resume: if we have a snapshot from a prior session that ended
	// complete and the on-disk file state still matches, anacrolix's
	// bolt-backed piece-completion store already knows every piece is good.
	// Skip the full hash and let it serve cached completion lookups. This
	// turns a multi-minute multi-GB rehash into a stat() per file.
	if a.snapshotStore != nil {
		if snap, wasComplete, ok := a.snapshotStore.LoadVerifySnapshot(id); ok && wasComplete {
			if info := t.Info(); info != nil {
				saveTo := a.saveDirFor(id)
				if cur, err := computeFileSnapshot(info, saveTo); err == nil && bytes.Equal(snap, cur) {
					log.Printf("verify: snapshot match — skipping hash for torrent %s", id)
					if ctx.Err() == nil {
						t.DownloadAll()
					}
					return
				}
			}
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

	if ctx.Err() != nil {
		return
	}

	// Now that the torrent is fully present, persist the snapshot so the
	// next startup can take the fast-resume path. Errors are logged but not
	// propagated — a missing snapshot just means we re-verify next time.
	if a.snapshotStore != nil && t.BytesMissing() == 0 {
		if info := t.Info(); info != nil {
			saveTo := a.saveDirFor(id)
			if cur, err := computeFileSnapshot(info, saveTo); err == nil {
				if err := a.snapshotStore.SaveVerifySnapshot(id, cur, true); err != nil {
					log.Printf("verify: save snapshot for %s: %v", id, err)
				}
			}
		}
	}

	t.DownloadAll()
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
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	mi, err := metainfo.Load(bytes.NewReader(blob))
	if err != nil {
		return "", err
	}
	spec := torrent.TorrentSpecFromMetaInfo(mi)
	spec.Storage = storage.NewFile(savePath)
	t, _, err := a.client.AddTorrentSpec(spec)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	// Same verify-then-start flow as AddMagnet — the GotInfo wait inside
	// verifyAndStart is a no-op here since metainfo is already attached.
	go a.verifyAndStart(ctx, id, t)
	return id, nil
}

func (a *AnacrolixBackend) Pause(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	t.SetMaxEstablishedConns(0)
	a.pausedMu.Lock()
	a.paused[id] = true
	a.pausedMu.Unlock()
	return nil
}

func (a *AnacrolixBackend) Resume(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	t.SetMaxEstablishedConns(80)
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
	return nil
}

// ApplyPerTorrentMaxPeers updates SetMaxEstablishedConns on every running
// torrent. Used when the user changes the "max peers per torrent" setting.
// Already-paused torrents keep their cap of 0 (we set it to 0 to pause);
// resuming will pick up the new cap.
func (a *AnacrolixBackend) ApplyPerTorrentMaxPeers(n int) error {
	if n <= 0 {
		n = 80 // anacrolix default
	}
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
	a.setVerifying(id, true)
	go func() {
		verifyDataParallel(context.Background(), t)
		// Re-establish the snapshot if the torrent ended fully present.
		if a.snapshotStore != nil && t.BytesMissing() == 0 {
			if info := t.Info(); info != nil {
				saveTo := a.saveDirFor(id)
				if cur, err := computeFileSnapshot(info, saveTo); err == nil {
					if err := a.snapshotStore.SaveVerifySnapshot(id, cur, true); err != nil {
						log.Printf("verify: save snapshot post-recheck for %s: %v", id, err)
					}
				}
			}
		}
		a.setVerifying(id, false)
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
	a.mu.Unlock()
	a.pausedMu.Lock()
	delete(a.paused, id)
	delete(a.queuePos, id)
	delete(a.forceStart, id)
	delete(a.scheduledPause, id)
	a.pausedMu.Unlock()
	t.Drop()
	if deleteFiles && saveTo != "" {
		if info := t.Info(); info != nil {
			// Path-traversal defense: a malicious .torrent's info.Name can be
			// "../../something" — joining it raw would let RemoveAll walk above
			// saveTo and delete arbitrary files. safeRemovePath validates
			// containment (resolving symlinks on both ends when present) and
			// returns an error we log+skip on. The in-memory unsubscribe
			// (t.Drop, the map deletes above) has already happened — only the
			// disk delete is conditional on validation.
			path, err := safeRemovePath(saveTo, info.Name)
			if err != nil {
				log.Printf("refusing to delete files for torrent: name=%q error=%v", info.Name, err)
			} else {
				_ = os.RemoveAll(path)
			}
		}
	}
	return nil
}

func (a *AnacrolixBackend) List() []Snapshot {
	ts := a.client.Torrents()
	out := make([]Snapshot, 0, len(ts))
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	a.pausedMu.RLock()
	defer a.pausedMu.RUnlock()
	a.verifyMu.RLock()
	defer a.verifyMu.RUnlock()
	for _, t := range ts {
		id := TorrentID(t.InfoHash().HexString())
		snap, next := snapshotFor(t, a.prevRates[id], a.paused[id], a.queuePos[id], a.forceStart[id], a.scheduledPause[id], a.verifying[id], a.filesMissing[id])
		a.prevRates[id] = next
		out = append(out, snap)
	}
	return out
}

func (a *AnacrolixBackend) Snapshot(id TorrentID) (Snapshot, error) {
	t, ok := a.find(id)
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	a.rateMu.Lock()
	prev := a.prevRates[id]
	a.pausedMu.RLock()
	paused := a.paused[id]
	queuePos := a.queuePos[id]
	forceStart := a.forceStart[id]
	queued := a.scheduledPause[id]
	a.pausedMu.RUnlock()
	a.verifyMu.RLock()
	verifying := a.verifying[id]
	filesMissing := a.filesMissing[id]
	a.verifyMu.RUnlock()
	snap, next := snapshotFor(t, prev, paused, queuePos, forceStart, queued, verifying, filesMissing)
	a.prevRates[id] = next
	a.rateMu.Unlock()
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

func (a *AnacrolixBackend) Close() error {
	errs := a.client.Close()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (a *AnacrolixBackend) find(id TorrentID) (*torrent.Torrent, bool) {
	for _, t := range a.client.Torrents() {
		if idFor(t) == id {
			return t, true
		}
	}
	return nil, false
}

// DetailedSnapshot pulls files/peers/trackers from the underlying anacrolix
// Torrent based on scope. We translate to our FileEntry/PeerEntry/TrackerEntry
// domain types.
func (a *AnacrolixBackend) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	t, ok := a.find(id)
	if !ok {
		return Detail{}, errors.New("not found")
	}
	a.rateMu.Lock()
	prev := a.prevRates[id]
	a.pausedMu.RLock()
	paused := a.paused[id]
	queuePos := a.queuePos[id]
	forceStart := a.forceStart[id]
	queued := a.scheduledPause[id]
	a.pausedMu.RUnlock()
	a.verifyMu.RLock()
	verifying := a.verifying[id]
	filesMissing := a.filesMissing[id]
	a.verifyMu.RUnlock()
	snap, next := snapshotFor(t, prev, paused, queuePos, forceStart, queued, verifying, filesMissing)
	a.prevRates[id] = next
	a.rateMu.Unlock()
	d := Detail{Snapshot: snap}

	if scope.Files {
		for i, f := range t.Files() {
			d.Files = append(d.Files, FileEntry{
				Index:     i,
				Path:      f.DisplayPath(),
				Size:      f.Length(),
				BytesDone: f.BytesCompleted(),
				Priority:  prioFromAnacrolix(f.Priority()),
			})
		}
	}

	if scope.Peers {
		for _, pc := range t.PeerConns() {
			addr := pc.RemoteAddr.String()
			ip := addr
			port := 0
			if h, p, err := splitHostPort(addr); err == nil {
				ip = h
				port = p
			}
			name, _ := pc.PeerClientName.Load().(string)
			d.Peers = append(d.Peers, PeerEntry{
				IP:           ip,
				Port:         port,
				ClientName:   name,
				Flags:        peerFlagsFor(pc),
				Progress:     pieceProgressOf(t, pc),
				DownloadRate: int64(pc.DownloadRate()),
				UploadRate:   int64(pc.Stats().LastWriteUploadRate),
				CountryCode:  "",
			})
		}
	}

	if scope.Trackers {
		// anacrolix v1.61 doesn't expose per-tracker announce state; the best
		// signal we have is whether metadata has loaded (Info != nil). Before
		// metadata, no tracker has produced a useful response yet → "Updating".
		// After metadata, fall back to "OK" (best-effort). A future plan or
		// upstream PR can refine this with real per-tracker error state.
		status := "OK"
		if t.Info() == nil {
			status = "Updating"
		}
		mi := t.Metainfo()
		for _, tier := range mi.AnnounceList {
			for _, url := range tier {
				d.Trackers = append(d.Trackers, TrackerEntry{
					URL:    url,
					Status: status,
				})
			}
		}
		if len(d.Trackers) == 0 && mi.Announce != "" {
			d.Trackers = append(d.Trackers, TrackerEntry{URL: mi.Announce, Status: status})
		}
	}

	return d, nil
}

func prioFromAnacrolix(p anacrolix_types.PiecePriority) Priority {
	switch p {
	case anacrolix_types.PiecePriorityNone:
		return PrioritySkip
	case anacrolix_types.PiecePriorityNormal:
		return PriorityNormal
	case anacrolix_types.PiecePriorityHigh:
		return PriorityHigh
	case anacrolix_types.PiecePriorityNow:
		return PriorityMax
	}
	return PriorityNormal
}

func splitHostPort(addr string) (string, int, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return h, 0, err
	}
	return h, port, nil
}

// peerFlagsFor returns the BitTorrent peer flag string. anacrolix v1.61 keeps
// peerInterested/peerChoking unexported and ships no header-obfuscation
// accessor, so we cannot read the bits — Plan 4 (or an upstream PR) refines.
func peerFlagsFor(pc *torrent.PeerConn) string {
	_ = pc
	return ""
}

// pieceProgressOf returns 0..1 of pieces this peer has. *roaring.Bitmap has
// no Len() method; the denominator is the parent torrent's piece count.
func pieceProgressOf(t *torrent.Torrent, pc *torrent.PeerConn) float64 {
	pp := pc.PeerPieces()
	if pp.IsEmpty() {
		return 0
	}
	n := t.NumPieces()
	if n == 0 {
		return 0
	}
	return float64(pp.GetCardinality()) / float64(n)
}

func snapshotFor(t *torrent.Torrent, prev rateSample, paused bool, queuePos int, forceStart, queued, verifying, filesMissing bool) (Snapshot, rateSample) {
	stats := t.Stats()
	name := t.Name()
	if name == "" {
		name = t.InfoHash().HexString()
	}
	total := int64(0)
	if info := t.Info(); info != nil {
		total = info.TotalLength()
	}
	bytesDown := stats.BytesReadData.Int64()
	bytesUp := stats.BytesWrittenData.Int64()
	now := time.Now()
	var rateDown, rateUp int64
	if !prev.at.IsZero() {
		dt := now.Sub(prev.at).Seconds()
		if dt > 0 {
			rateDown = int64(float64(bytesDown-prev.down) / dt)
			rateUp = int64(float64(bytesUp-prev.up) / dt)
		}
	}
	snap := Snapshot{
		ID:            TorrentID(t.InfoHash().HexString()),
		Name:          name,
		TotalBytes:    total,
		BytesDone:     t.BytesCompleted(),
		BytesDown:     bytesDown,
		BytesUp:       bytesUp,
		RateDown:      rateDown,
		RateUp:        rateUp,
		Peers:         stats.ActivePeers,
		Seeds:         stats.ConnectedSeeders,
		Paused:        paused,
		Completed:     total > 0 && t.BytesCompleted() == total,
		AddedAt:       time.Now(), // engine wrapper does not track AddedAt; persistence does
		QueuePosition: queuePos,
		ForceStart:    forceStart,
		Queued:        queued,
		Verifying:     verifying,
		FilesMissing:  filesMissing,
	}
	return snap, rateSample{at: now, down: bytesDown, up: bytesUp}
}

// SetGlobalRateLimits mutates the existing limiter pointers in place. Passing
// 0 means unlimited (rate.Inf, with a 256 KB burst floor for peer messages).
func (a *AnacrolixBackend) SetGlobalRateLimits(downBPS, upBPS int) error {
	if downBPS <= 0 {
		a.dlLim.SetLimit(rate.Inf)
		a.dlLim.SetBurst(256 << 10)
	} else {
		a.dlLim.SetLimit(rate.Limit(downBPS))
		a.dlLim.SetBurst(max(downBPS, 256<<10))
	}
	if upBPS <= 0 {
		a.ulLim.SetLimit(rate.Inf)
		a.ulLim.SetBurst(256 << 10)
	} else {
		a.ulLim.SetLimit(rate.Limit(upBPS))
		a.ulLim.SetBurst(max(upBPS, 256<<10))
	}
	return nil
}

func (a *AnacrolixBackend) SetQueuePosition(id TorrentID, pos int) {
	a.pausedMu.Lock()
	a.queuePos[id] = pos
	a.pausedMu.Unlock()
}

func (a *AnacrolixBackend) SetForceStart(id TorrentID, force bool) {
	a.pausedMu.Lock()
	a.forceStart[id] = force
	a.pausedMu.Unlock()
}

// ScheduledPause is the scheduler's pause channel — independent from the
// user's manual Pause. It uses the same SetMaxEstablishedConns(0/80) trick
// the manual Pause uses, but writes only the scheduledPause map flag so
// snapshots can distinguish "user-paused" from "queue-held".
func (a *AnacrolixBackend) ScheduledPause(id TorrentID, paused bool) {
	t, ok := a.find(id)
	if !ok {
		return
	}
	if paused {
		t.SetMaxEstablishedConns(0)
	} else {
		t.SetMaxEstablishedConns(80)
	}
	a.pausedMu.Lock()
	a.scheduledPause[id] = paused
	a.pausedMu.Unlock()
}
