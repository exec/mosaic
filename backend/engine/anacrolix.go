package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/storage"
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
	// EnableUPnP controls automatic UPnP/NAT-PMP port forwarding. When true,
	// anacrolix discovers routers on the LAN and asks them to forward the
	// BitTorrent listen port so inbound peers can connect. Requires a restart
	// to take effect (anacrolix's forwardPort is called once at client startup;
	// there is no runtime enable/disable path).
	EnableUPnP bool
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
	// ClientVersion is "Mosaic/0.5.7"-style and is sent in three places:
	// HTTP tracker User-Agent, BitTorrent extended handshake client name,
	// and (truncated) the BEP-20 peer-id prefix. Empty falls back to
	// anacrolix's defaults ("anacrolix-torrent/<v>"), which is what
	// other peers + trackers were seeing pre-v0.5.7.
	ClientVersion string
	// PreallocateFullFiles flips anacrolix's UsePartFiles from its default
	// of true to false. Default behavior writes incomplete data into a
	// `<name>.part` file alongside the eventual final path, which keeps
	// disk commitment proportional to bytes downloaded — but means
	// anacrolix's storage init runs setCompletionFromPartFiles on every
	// open and wipes bolt's "complete" entries because the final path
	// doesn't exist. We work around the wipe with a per-piece bitmap
	// replay (see writeSnapshot + restoreBoltFromBitmap), but a user with
	// plenty of disk can opt into PreallocateFullFiles=true to dodge the
	// wipe entirely: anacrolix preallocates the full file at the final
	// path immediately, setCompletionFromPartFiles sees the right size
	// and leaves bolt alone, restart-after-pause is a stat() per file.
	// Tradeoff: full disk commitment up front; couple with the existing
	// disk-space precheck on add.
	PreallocateFullFiles bool
}

// AnacrolixBackend implements Backend on top of anacrolix/torrent.
type AnacrolixBackend struct {
	client *torrent.Client

	mu       sync.Mutex
	bySaveTo map[TorrentID]string             // id → save path (we set it per-torrent)
	byID     map[TorrentID]*torrent.Torrent   // id → torrent handle, populated on Add, pruned on Remove
	// Pre-v0.4.3 every per-id operation (Pause/Resume/Recheck/Remove/
	// Snapshot/SetFilePriorities/ScheduledPause) called find() which
	// linear-scanned client.Torrents(). With several hundred torrents
	// the scheduler tick (2s) + inspector tick (1s) + per-call traffic
	// turned that into measurable idle CPU. byID makes find() O(1).
	// Same mutex (a.mu) as bySaveTo so the two stay consistent.

	// Rate sampling is centralized on ONE goroutine (sampleRates, started in
	// NewAnacrolixBackend). Pre-v0.7.x every caller of List/Snapshot/
	// DetailedSnapshot computed rate as (bytesNow − prevRates[id]) / dt where
	// dt was wall-clock since whichever goroutine last touched prevRates —
	// and prevRates was mutated by the engine ticker, the scheduler, AND
	// streamTicks calling List() once per connected user back-to-back. Calls
	// 2..N saw dt≈microseconds → wildly inflated rates, then near-zero next
	// tick; users saw flickering speeds. Now:
	//   - prevRates is owned EXCLUSIVELY by the sampler goroutine. rateMu only
	//     guards Remove() pruning the map between sampler ticks.
	//   - the sampler publishes an immutable map[TorrentID]rateValue via an
	//     atomic pointer swap (rateCache). List/Snapshot/DetailedSnapshot load
	//     that map and read it; they never compute or store rate samples.
	rateMu    sync.Mutex
	prevRates map[TorrentID]rateSample
	rateCache atomic.Pointer[map[TorrentID]rateValue]

	// prevPeerRates is the per-peer equivalent: anacrolix's
	// Peer.DownloadRate() is a cumulative average over the whole
	// connection lifetime, so for an active download a peer that fed
	// chunks fast for the first 30s of a 5-minute session reads as a
	// medium-rate average forever. We snapshot per-peer cumulative
	// BytesReadUsefulData on each DetailedSnapshot tick and divide the
	// delta by the elapsed time to surface a current speed.
	// Keyed (TorrentID, "ip:port"); guarded by rateMu.
	prevPeerRates map[TorrentID]map[string]peerRateSample

	// maxConnsPerTorrent is the user-configured per-torrent established-conn
	// cap (anacrolix's default of 80 when the setting is 0/unset). Initialized
	// from AnacrolixConfig.MaxPeersPerTorrent, updated by
	// ApplyPerTorrentMaxPeers, and read by Resume / ScheduledPause's re-enable
	// path — pre-fix both hardcoded SetMaxEstablishedConns(80), so any
	// pause/resume cycle or scheduler churn silently reset a user's cap to 80.
	maxConnsPerTorrent atomic.Int64

	// pausedMu guards paused, queuePos, forceStart, scheduledPause, sequential.
	// We extend the existing read-mostly mutex rather than introducing a new
	// one — these maps are all read together by snapshotFor and written through
	// the same per-torrent setters, so a single RWMutex keeps the invariants simple.
	pausedMu       sync.RWMutex
	paused         map[TorrentID]bool
	queuePos       map[TorrentID]int
	forceStart     map[TorrentID]bool
	sequential     map[TorrentID]bool
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

	// perTorrentLimits stores per-torrent download/upload caps in bytes/sec.
	// 0 means unlimited. Guarded by perLimitMu. The actual enforcement is done
	// by a background goroutine (startPerTorrentLimiter) that uses
	// DisallowDataDownload/AllowDataDownload/DisallowDataUpload/AllowDataUpload
	// to implement a duty-cycle approximation of the requested rate.
	perLimitMu     sync.RWMutex
	perTorrentDown map[TorrentID]int64 // bytes/sec, 0=unlimited
	perTorrentUp   map[TorrentID]int64 // bytes/sec, 0=unlimited
	// limiterRunning marks torrents with a live runPerTorrentLimiter
	// goroutine. SetTorrentRateLimits sets it (under perLimitMu) before
	// spawning; the goroutine clears it (under the same lock, re-checking
	// the limits) before exiting on the both-limits-zero path. Pre-fix the
	// "needs a goroutine" test compared against the previous limit values,
	// so a clear-to-(0,0) followed by a non-zero set within one 500ms tick
	// could spawn a second limiter while the first never observed the (0,0)
	// state — two goroutines then fought over Allow/DisallowDataDownload.
	limiterRunning map[TorrentID]bool

	// pieceCompletion is the shared bolt-backed piece-completion store
	// rooted in cfg.DataDir, so anacrolix's "is this piece valid"
	// metadata stays in our app data dir instead of getting sprinkled
	// into every user save path as `.torrent.bolt.db`. Closed in
	// Close(). Pre-v0.4.1 the per-torrent storage was constructed via
	// storage.NewFile(savePath), which creates a fresh bolt next to
	// the content; this field replaces that behavior.
	pieceCompletion storage.PieceCompletion

	// snapshotStore is the optional fast-resume hook. nil disables the
	// optimization. See SnapshotStore for the lifecycle.
	snapshotStore SnapshotStore

	// snapshotSaved tracks ids whose verify snapshot has been written (or
	// confirmed-still-valid) this process lifetime, so the per-tick check
	// in List() doesn't recompute and rewrite the snapshot every poll.
	// Cleared on Remove and Recheck. Pre-v0.4.4 the snapshot was saved
	// only at the end of verifyAndStart — torrents that finished
	// downloading mid-session never got a snapshot written until the
	// *first* post-completion relaunch did a full re-verify (and that
	// verify is what wrote it). Now any runtime completion triggers an
	// immediate save so that first relaunch already takes the
	// fast-resume path.
	snapshotMu    sync.Mutex
	snapshotSaved map[TorrentID]bool
	// snapshotInflight marks ids with a List()-spawned saveSnapshotIfComplete
	// goroutine currently running; snapshotRetryAt is the earliest next
	// attempt after a failed write. Pre-fix List() spawned one goroutine per
	// completed torrent per tick unconditionally — snapshotSaved is only set
	// on a SUCCESSFUL write, so a persistent failure (read-only engine dir,
	// full disk) leaked N goroutines several times a second forever, each
	// taking the client lock. Both guarded by snapshotMu.
	snapshotInflight map[TorrentID]bool
	snapshotRetryAt  map[TorrentID]time.Time

	// onError is the engine-side hook for surfacing per-torrent errors
	// (out-of-disk-space being the canonical case). Wired by Engine via
	// SetErrorHandler at construction. Without it, anacrolix's default
	// onWriteChunkErr handler silently flips the torrent into
	// dataDownloadDisallowed — peers stay connected but no piece is ever
	// requested again, with zero indication to the user.
	onErrorMu sync.RWMutex
	onError   func(TorrentID, error)

	// engineCtx / engineCancel govern the lifetime of background verify
	// goroutines. Each verifyAndStart goroutine ran with context.Background()
	// pre-v0.5.3 (we'd peeled away the caller's ctx after GotInfo to stop
	// HTTP/RSS-handler returns from cancelling verify). That fix left
	// verify with no cancellation channel at all — Close() would call
	// client.Close() and pieceCompletion.Close() while our outer
	// verifyDataParallel goroutines were still dispatching VerifyDataContext
	// calls into the now-closing client, holding the process open for as
	// long as a multi-GB hash took. Now Close() cancels engineCtx first
	// and waits on verifyWg, so verify shuts down cleanly before the
	// client + bolt come down.
	engineCtx    context.Context
	engineCancel context.CancelFunc
	verifyWg     sync.WaitGroup

	// preallocateFullFiles mirrors AnacrolixConfig.PreallocateFullFiles
	// so AddMagnet / AddFile can build per-spec storage with the same
	// UsePartFiles behavior as the default storage. See AnacrolixConfig
	// docstring for tradeoffs.
	preallocateFullFiles bool
}

type rateSample struct {
	at   time.Time
	down int64
	up   int64
}

// rateValue is the published per-torrent rate after one sampler tick. The
// sampler swaps an immutable map[TorrentID]rateValue into rateCache; readers
// (List/Snapshot/DetailedSnapshot) load it and pass the values straight into
// snapshotFor. A torrent absent from the map (just-added, not yet sampled)
// reads as the zero value — 0 B/s — which is correct for a fresh torrent.
type rateValue struct {
	down int64
	up   int64
}

// peerRateSample is a single tick's worth of per-peer cumulative data
// counters. The next tick's rate is (current - sample) / (now - at).
// rate is the rate computed AT this sample, kept so back-to-back
// DetailedSnapshot calls (dt below minPeerRateSampleInterval) can reuse
// it instead of resampling — see the scope.Peers block in DetailedSnapshot.
type peerRateSample struct {
	at   time.Time
	down int64
	rate int64
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
	// Tracker / webseed HTTP transport with sensible deadlines. Anacrolix
	// hands all tracker scrapes (announces, scrapes, AND stop-announces on
	// torrent removal) through this transport; without explicit timeouts it
	// inherits the OS connect timeout (~75s on Linux, ~21s default on
	// macOS, ~21s on Windows). A single hung HTTP-only tracker like the
	// "checkmyiptorrent" variant pinned the client lock during t.Drop and
	// wedged Remove for ages — v0.5.7 capped t.Drop at 5s on our side, but
	// the underlying goroutine still had to time out. Now the dial itself
	// caps at 10s, response-headers at 15s, idle conns at 90s — matching
	// what most browsers use for HTTP. anacrolix's MaxConnsPerHost=10
	// preserved.
	tcfg.WebTransport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxConnsPerHost:       10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
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
	// UPnP/NAT-PMP port forwarding. anacrolix's default is to run UPnP
	// discovery on startup (NoDefaultPortForwarding=false). We flip the
	// opt-out when the user has disabled the feature in Settings → Connection.
	tcfg.NoDefaultPortForwarding = !cfg.EnableUPnP
	// Client identification. The HTTP User-Agent goes to tracker scrapes;
	// ExtendedHandshakeClientVersion is what other BitTorrent clients see
	// on the BEP-10 extended handshake; Bep20 is the 8-byte peer-id
	// prefix. Convention is "-XXNNNN-" where XX is a two-letter client
	// code and NNNN is a 4-digit version. We use "MS" (Mosaic). Falls
	// back to anacrolix's defaults if ClientVersion is empty.
	if cfg.ClientVersion != "" {
		tcfg.HTTPUserAgent = cfg.ClientVersion
		tcfg.ExtendedHandshakeClientVersion = cfg.ClientVersion
		if bep20 := bep20PeerID(cfg.ClientVersion); bep20 != "" {
			tcfg.Bep20 = bep20
		}
	}
	// Single shared piece-completion store, rooted in our app data dir
	// (cfg.DataDir = paths.DataDir/engine). This is the "have I already
	// validated this piece" cache anacrolix consults at startup so it
	// doesn't re-hash everything. Pre-v0.4.1 we used storage.NewFile()
	// per-torrent, which auto-creates a `.torrent.bolt.db` next to the
	// content files in each save path — visible to users in their
	// Downloads folder. Now there's exactly one bolt file, in our
	// engine dir. (cfg.DataDir is MkdirAll-ed at the top of this func.)
	pieceCompletion, err := storage.NewBoltPieceCompletion(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("init piece completion store: %w", err)
	}
	tcfg.DefaultStorage = newFileStorage(cfg.DataDir, pieceCompletion, cfg.PreallocateFullFiles)

	dlLim := rate.NewLimiter(rate.Inf, 256<<10)
	ulLim := rate.NewLimiter(rate.Inf, 256<<10)
	tcfg.DownloadRateLimiter = dlLim
	tcfg.UploadRateLimiter = ulLim

	ipBlock := &ipBlocklistProxy{}
	tcfg.IPBlocklist = ipBlock

	// Listen-port bind with fallback. anacrolix's listenAll does NOT retry
	// a static port on EADDRINUSE — it just returns the bind error, which
	// pre-fix bubbled up to main.go's log.Fatal and killed the process.
	// On a system where another BitTorrent client (qBittorrent, Deluge,
	// Transmission, …) already holds our configured port, that meant
	// Mosaic refused to start at all and the user's torrents never seeded.
	// (See main.go's second-instance comment — same failure mode, only
	// that one was handled for "another Mosaic instance" but never for
	// "any other torrent client".)
	//
	// Behavior: if the configured port is non-zero AND fails with an
	// address-in-use error, retry with port 0 so the OS picks a free
	// ephemeral. The caller is expected to read ListenPort() afterwards
	// and persist it back to settings (see main.go) so the next launch
	// gets a stable port instead of a fresh random one each time. We do
	// NOT fall back on other errors — privilege denied, permission, or a
	// genuine config bug should still surface.
	c, err := torrent.NewClient(tcfg)
	if err != nil && cfg.ListenPort != 0 && isAddrInUseErr(err) {
		log.Printf("anacrolix: port %d unavailable (%v) — falling back to OS-picked port; persist it to keep it stable across launches", cfg.ListenPort, err)
		tcfg.ListenPort = 0
		c, err = torrent.NewClient(tcfg)
	}
	if err != nil {
		_ = pieceCompletion.Close()
		return nil, fmt.Errorf("anacrolix client: %w", err)
	}
	engineCtx, engineCancel := context.WithCancel(context.Background())
	b := &AnacrolixBackend{
		client:           c,
		pieceCompletion:  pieceCompletion,
		bySaveTo:         make(map[TorrentID]string),
		byID:             make(map[TorrentID]*torrent.Torrent),
		prevRates:        make(map[TorrentID]rateSample),
		prevPeerRates:    make(map[TorrentID]map[string]peerRateSample),
		paused:           make(map[TorrentID]bool),
		queuePos:         make(map[TorrentID]int),
		forceStart:       make(map[TorrentID]bool),
		sequential:       make(map[TorrentID]bool),
		scheduledPause:   make(map[TorrentID]bool),
		verifying:        make(map[TorrentID]bool),
		expectedComplete: make(map[TorrentID]bool),
		filesMissing:     make(map[TorrentID]bool),
		snapshotSaved:    make(map[TorrentID]bool),
		snapshotInflight: make(map[TorrentID]bool),
		snapshotRetryAt:  make(map[TorrentID]time.Time),
		perTorrentDown:   make(map[TorrentID]int64),
		perTorrentUp:     make(map[TorrentID]int64),
		limiterRunning:   make(map[TorrentID]bool),
		dlLim:          dlLim,
		ulLim:          ulLim,
		ipBlock:        ipBlock,
		snapshotStore:  cfg.SnapshotStore,
		engineCtx:            engineCtx,
		engineCancel:         engineCancel,
		preallocateFullFiles: cfg.PreallocateFullFiles,
	}
	conns := cfg.MaxPeersPerTorrent
	if conns <= 0 {
		conns = 80 // anacrolix default
	}
	b.maxConnsPerTorrent.Store(int64(conns))
	// Publish an empty cache up front so the first List() before the sampler's
	// first tick reads a non-nil map (every torrent → 0 B/s) instead of
	// nil-dereferencing.
	empty := make(map[TorrentID]rateValue)
	b.rateCache.Store(&empty)
	b.verifyWg.Add(1)
	go b.sampleRates(rateSampleInterval)
	if cfg.SnapshotStore != nil {
		b.verifyWg.Add(1)
		go b.periodicCheckpoint(snapshotCheckpointInterval)
	}
	return b, nil
}

// rateSampleInterval is the fixed cadence of the single rate sampler. 500ms
// matches the pre-v0.7.x engine ticker that used to (racily) drive sampling,
// so observed rate granularity is unchanged.
const rateSampleInterval = 500 * time.Millisecond

// minPeerRateSampleInterval is the floor below which DetailedSnapshot reuses
// the previous per-peer sample (and its computed rate) instead of resampling.
// Per-peer rates can't go through the central sampler — they're only worth
// computing while someone has the Peers tab focused — so back-to-back calls
// from multiple consumers are de-conflicted by this minimum-dt guard instead.
const minPeerRateSampleInterval = 250 * time.Millisecond

// sampleRates is the ONE goroutine that owns prevRates. Every tick it walks
// the live torrents, computes each torrent's down/up rate from the delta
// since its previous sample, and publishes an immutable snapshot map into
// rateCache via an atomic pointer swap. Readers never compute rates — they
// load this map. Started by NewAnacrolixBackend, stopped when engineCtx is
// cancelled by Close(); registered on verifyWg so Close() drains it.
func (a *AnacrolixBackend) sampleRates(interval time.Duration) {
	defer a.verifyWg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-a.engineCtx.Done():
			return
		case <-t.C:
			ts := a.client.Torrents()
			now := time.Now()
			next := make(map[TorrentID]rateValue, len(ts))
			a.rateMu.Lock()
			for _, tor := range ts {
				id := TorrentID(tor.InfoHash().HexString())
				stats := tor.Stats()
				down := stats.BytesReadData.Int64()
				up := stats.BytesWrittenData.Int64()
				prev := a.prevRates[id]
				var rateDown, rateUp int64
				if !prev.at.IsZero() {
					if dt := now.Sub(prev.at).Seconds(); dt > 0 {
						rateDown = int64(float64(down-prev.down) / dt)
						rateUp = int64(float64(up-prev.up) / dt)
					}
				}
				a.prevRates[id] = rateSample{at: now, down: down, up: up}
				next[id] = rateValue{down: rateDown, up: rateUp}
			}
			// Prune prevRates entries for torrents that vanished between
			// ticks (removed without going through Remove, e.g. a Drop
			// elsewhere) so the map can't grow unbounded.
			if len(a.prevRates) > len(ts) {
				live := make(map[TorrentID]struct{}, len(ts))
				for _, tor := range ts {
					live[TorrentID(tor.InfoHash().HexString())] = struct{}{}
				}
				for id := range a.prevRates {
					if _, ok := live[id]; !ok {
						delete(a.prevRates, id)
					}
				}
			}
			a.rateMu.Unlock()
			a.rateCache.Store(&next)
		}
	}
}

// snapshotCheckpointInterval is how often the background checkpoint goroutine
// saves per-piece bitmaps for all active torrents. Keeps fast-resume accurate
// even after an abnormal exit — at worst, up to one interval worth of newly
// downloaded pieces are re-verified on the next startup.
const snapshotCheckpointInterval = 5 * time.Minute

// periodicCheckpoint saves fast-resume snapshots for every active torrent at
// a fixed interval. This covers the crash-recovery case: a clean exit runs
// the same loop in Close(), but an OOM kill or force-quit skips it. Without
// this, a long-running download session that ends in a crash re-verifies ALL
// data on the next launch — potentially many gigabytes — instead of just the
// small slice downloaded since the last checkpoint.
func (a *AnacrolixBackend) periodicCheckpoint(interval time.Duration) {
	defer a.verifyWg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-a.engineCtx.Done():
			return
		case <-t.C:
			for _, tor := range a.client.Torrents() {
				a.saveSnapshotForCheckpoint(idFor(tor), tor)
			}
		}
	}
}

// rateFor returns the most recently sampled down/up rate for a torrent. A
// torrent the sampler hasn't observed yet (just added) reads as 0 B/s.
func (a *AnacrolixBackend) rateFor(id TorrentID) rateValue {
	if m := a.rateCache.Load(); m != nil {
		return (*m)[id]
	}
	return rateValue{}
}

// isAddrInUseErr reports whether err comes from a bind() that lost the
// race to another listener on the same port. Matches the Linux/macOS
// EADDRINUSE syscall errno AND the Windows WSAEADDRINUSE-translated
// error string ("Only one usage of each socket address...") that Go's
// net package surfaces. anacrolix wraps the underlying *net.OpError /
// *os.SyscallError before returning, so errors.Is on the wrapped chain
// catches the unix path; the string contains-check covers Windows where
// the syscall numeric value isn't the same constant.
func isAddrInUseErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "address already in use") ||
		strings.Contains(s, "Only one usage of each socket address")
}

// ListenPort returns the port the underlying anacrolix client actually
// bound. After a NewAnacrolixBackend that fell back from the configured
// port (because the OS reported EADDRINUSE), this is the OS-picked
// ephemeral; otherwise it's the configured port. Callers (main.go)
// persist this back to the settings DAO so the next launch starts with
// a port that's already known-good for this machine.
func (a *AnacrolixBackend) ListenPort() int { return a.client.LocalPort() }

// newFileStorage builds anacrolix's file storage with our shared bolt
// piece-completion store, honoring the preallocateFullFiles toggle.
// PreallocateFull=true switches anacrolix's UsePartFiles to false: the
// final file gets preallocated up front instead of writing to a .part
// suffix. See AnacrolixConfig.PreallocateFullFiles for the tradeoff.
func newFileStorage(saveDir string, pc storage.PieceCompletion, preallocateFull bool) storage.ClientImplCloser {
	if !preallocateFull {
		// Default path: anacrolix's UsePartFiles=true (the implicit
		// default of NewFileWithCompletion).
		return storage.NewFileWithCompletion(saveDir, pc)
	}
	return storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir:   saveDir,
		PieceCompletion: pc,
		UsePartFiles:    g.Some(false),
	})
}

// SetErrorHandler registers a callback for per-torrent backend errors —
// chunk write failures (ENOSPC, permission denied) and storage read
// failures during piece-completion lookup. Engine wires this at
// construction via the optional-interface assertion in NewEngine; the
// callback emits EventError on the engine event bus.
func (a *AnacrolixBackend) SetErrorHandler(f func(TorrentID, error)) {
	a.onErrorMu.Lock()
	a.onError = f
	a.onErrorMu.Unlock()
}

// raiseError is the internal hook every backend-side error path goes
// through. Logs at warn level (the user-visible event is the load-bearing
// surface; the log is a debugging aid) and calls the registered handler
// if any. Safe to call before SetErrorHandler — silently dropped.
func (a *AnacrolixBackend) raiseError(id TorrentID, err error) {
	log.Printf("backend error for torrent %s: %v", id, err)
	a.onErrorMu.RLock()
	cb := a.onError
	a.onErrorMu.RUnlock()
	if cb != nil {
		cb(id, err)
	}
}

// installWriteErrorHook wires anacrolix's per-torrent userOnWriteChunkErr
// callback so chunk write failures surface to the user instead of
// silently disabling the torrent. Setting userOnWriteChunkErr bypasses
// anacrolix's default handler (which calls disallowDataDownloadLocked
// on its own), so we have to call DisallowDataDownload ourselves to
// match — otherwise anacrolix would keep retrying the doomed write and
// peers would stay actively requesting pieces we can't store.
//
// On user Resume we'll call AllowDataDownload to clear the disallow
// flag — that's the recovery path once the user frees disk space.
func (a *AnacrolixBackend) installWriteErrorHook(id TorrentID, t *torrent.Torrent) {
	t.SetOnWriteChunkError(func(err error) {
		t.DisallowDataDownload()
		a.raiseError(id, fmt.Errorf("write failed (likely insufficient disk space): %w", err))
	})
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

func (a *AnacrolixBackend) Close() error {
	// Cancel engineCtx FIRST so verify goroutines stop dispatching new
	// piece hashes; then wait for in-flight ones to drain. Without this,
	// client.Close() races against verifyDataParallel still calling
	// VerifyDataContext into a closing client — on Windows that surfaces
	// as a lingering "mosaic.exe" in Task Manager, because the verify
	// goroutines can hold the bolt mmap and a couple of anacrolix locks
	// past the main process's last visible event.
	//
	// 5s timeout caps shutdown latency: anacrolix's VerifyDataContext
	// respects ctx cancellation and our outer dispatch loop checks
	// ctx.Done() between piece queues, so under normal conditions Wait
	// returns within milliseconds. The timeout only matters if a piece
	// hasher is wedged in disk I/O — better to drop it on the floor
	// than block the whole UX of "X-button to fully exited" forever.
	if a.engineCancel != nil {
		a.engineCancel()
	}
	done := make(chan struct{})
	go func() {
		a.verifyWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Printf("anacrolix close: verify drain timeout (5s); forcing client close")
	}
	// Checkpoint every torrent before tearing down — partial AND complete.
	// The per-piece bitmap saved here is replayed into bolt on the next
	// add to skip the brutal re-hash that would otherwise scan every
	// preallocated .part file end-to-end. Capped overall: each
	// computeFileSnapshot stats every file in the torrent and we don't
	// want a slow disk to wedge shutdown.
	checkpointDone := make(chan struct{})
	go func() {
		for _, t := range a.client.Torrents() {
			a.saveSnapshotForCheckpoint(idFor(t), t)
		}
		close(checkpointDone)
	}()
	select {
	case <-checkpointDone:
	case <-time.After(5 * time.Second):
		log.Printf("anacrolix close: checkpoint loop timeout (5s); some snapshots may not have persisted")
	}
	errs := a.client.Close()
	// Always close the bolt piece-completion store, even if the client
	// shutdown returned errors — leaving the bolt open would leak its
	// file lock and break the next process startup.
	if a.pieceCompletion != nil {
		_ = a.pieceCompletion.Close()
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (a *AnacrolixBackend) find(id TorrentID) (*torrent.Torrent, bool) {
	a.mu.Lock()
	t, ok := a.byID[id]
	a.mu.Unlock()
	if ok {
		return t, true
	}
	// Fallback to a linear scan if byID hasn't been populated yet
	// (e.g. a torrent restored at startup before our Add path mapped
	// it). Self-heals by populating byID on the way out so subsequent
	// lookups are O(1). Removed-and-not-yet-pruned entries are caught
	// here too: we'd find no matching id and return false, the caller
	// gets "not found", and the stale a.byID entry (if any) was
	// already cleared in Remove() above.
	for _, candidate := range a.client.Torrents() {
		if idFor(candidate) == id {
			a.mu.Lock()
			a.byID[id] = candidate
			a.mu.Unlock()
			return candidate, true
		}
	}
	return nil, false
}

// snapshotFor builds a Snapshot for a torrent. The down/up rate is supplied
// by the caller from the centralized rate cache (see sampleRates) — this
// function no longer computes or stores rate samples, so it is a pure read
// of the torrent and is safe to call concurrently from any number of readers.
func snapshotFor(t *torrent.Torrent, rate rateValue, paused bool, queuePos int, forceStart, sequential, queued, verifying, filesMissing bool) Snapshot {
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
	rateDown, rateUp := rate.down, rate.up
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
		Sequential:    sequential,
		Queued:        queued,
		Verifying:     verifying,
		FilesMissing:  filesMissing,
	}
	return snap
}

// SetGlobalRateLimits mutates the existing limiter pointers in place. Passing
// 0 means unlimited (rate.Inf, with a 256 KB burst floor for peer messages).
