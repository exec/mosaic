package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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

	// Rate limiters owned via ClientConfig. The same *rate.Limiter pointers are
	// stashed here so SetGlobalRateLimits can mutate them in place via
	// SetLimit/SetBurst (anacrolix v1.61 has no setter on the Client itself).
	dlLim *rate.Limiter
	ulLim *rate.Limiter

	ipBlock *ipBlocklistProxy
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
		bySaveTo:       make(map[TorrentID]string),
		prevRates:      make(map[TorrentID]rateSample),
		paused:         make(map[TorrentID]bool),
		queuePos:       make(map[TorrentID]int),
		forceStart:     make(map[TorrentID]bool),
		scheduledPause: make(map[TorrentID]bool),
		dlLim:          dlLim,
		ulLim:          ulLim,
		ipBlock:        ipBlock,
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
	t, err := a.client.AddMagnet(magnet)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	go func() {
		select {
		case <-t.GotInfo():
			t.DownloadAll()
		case <-ctx.Done():
		}
	}()
	return id, nil
}

func (a *AnacrolixBackend) AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error) {
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	mi, err := metainfo.Load(bytes.NewReader(blob))
	if err != nil {
		return "", err
	}
	t, err := a.client.AddTorrent(mi)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	t.DownloadAll()
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
	a.pausedMu.Unlock()
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
			_ = os.RemoveAll(filepath.Join(saveTo, info.Name))
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
	for _, t := range ts {
		id := TorrentID(t.InfoHash().HexString())
		snap, next := snapshotFor(t, a.prevRates[id], a.paused[id], a.queuePos[id], a.forceStart[id], a.scheduledPause[id])
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
	snap, next := snapshotFor(t, prev, paused, queuePos, forceStart, queued)
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
	snap, next := snapshotFor(t, prev, paused, queuePos, forceStart, queued)
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

func snapshotFor(t *torrent.Torrent, prev rateSample, paused bool, queuePos int, forceStart, queued bool) (Snapshot, rateSample) {
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
