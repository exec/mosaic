package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	anacrolix_types "github.com/anacrolix/torrent/types"
)

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

	c, err := torrent.NewClient(tcfg)
	if err != nil {
		return nil, fmt.Errorf("anacrolix client: %w", err)
	}
	return &AnacrolixBackend{client: c, bySaveTo: make(map[TorrentID]string)}, nil
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
	return nil
}

func (a *AnacrolixBackend) Resume(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	t.SetMaxEstablishedConns(80)
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
	for _, t := range ts {
		out = append(out, snapshotFor(t))
	}
	return out
}

func (a *AnacrolixBackend) Snapshot(id TorrentID) (Snapshot, error) {
	t, ok := a.find(id)
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	return snapshotFor(t), nil
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
	d := Detail{Snapshot: snapshotFor(t)}

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
		mi := t.Metainfo()
		for _, tier := range mi.AnnounceList {
			for _, url := range tier {
				d.Trackers = append(d.Trackers, TrackerEntry{
					URL:    url,
					Status: "OK",
				})
			}
		}
		if len(d.Trackers) == 0 && mi.Announce != "" {
			d.Trackers = append(d.Trackers, TrackerEntry{URL: mi.Announce, Status: "OK"})
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

func snapshotFor(t *torrent.Torrent) Snapshot {
	stats := t.Stats()
	name := t.Name()
	if name == "" {
		name = t.InfoHash().HexString()
	}
	total := int64(0)
	if info := t.Info(); info != nil {
		total = info.TotalLength()
	}
	return Snapshot{
		ID:           TorrentID(t.InfoHash().HexString()),
		Name:         name,
		TotalBytes:   total,
		BytesDone:    t.BytesCompleted(),
		DownloadRate: int64(stats.BytesReadData.Int64()), // cumulative; UI computes rate
		UploadRate:   int64(stats.BytesWrittenData.Int64()),
		Peers:        stats.ActivePeers,
		Seeds:        stats.ConnectedSeeders,
		Paused:       false, // TODO in plan 2: distinguish via SetMaxEstablishedConns(0)
		Completed:    total > 0 && t.BytesCompleted() == total,
		AddedAt:      time.Now(), // engine wrapper does not track AddedAt; persistence does
	}
}
