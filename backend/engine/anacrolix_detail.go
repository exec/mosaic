package engine

import (
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	anacrolix_types "github.com/anacrolix/torrent/types"
)

func (a *AnacrolixBackend) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	t, ok := a.find(id)
	if !ok {
		return Detail{}, errors.New("not found")
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
		// Per-peer windowed download rate: store last-tick cumulative
		// BytesReadUsefulData per peer, divide the next tick's delta by
		// the elapsed time. Falls back to 0 on the first tick (no prior
		// sample) and on completion (we wouldn't be requesting chunks
		// anyway). The per-peer map is still guarded by rateMu, shared
		// with the rate sampler's prune of prevRates.
		complete := t.BytesMissing() == 0
		now := time.Now()
		a.rateMu.Lock()
		prevPeers := a.prevPeerRates[id]
		if prevPeers == nil {
			prevPeers = make(map[string]peerRateSample)
		}
		nextPeers := make(map[string]peerRateSample, len(prevPeers))
		for _, pc := range t.PeerConns() {
			addr := pc.RemoteAddr.String()
			ip := addr
			port := 0
			if h, p, err := splitHostPort(addr); err == nil {
				ip = h
				port = p
			}
			name, _ := pc.PeerClientName.Load().(string)
			peerKey := addr // ip:port — stable per connection
			// Assign Stats() to a local: Count.Int64 is a pointer-receiver
			// method, and a field on a non-addressable function return
			// value isn't itself addressable.
			peerStats := pc.Stats()
			cumDown := peerStats.ConnStats.BytesReadUsefulData.Int64()
			var dlRate int64
			sample := peerRateSample{at: now, down: cumDown}
			if !complete {
				if prev, ok := prevPeers[peerKey]; ok {
					if dt := now.Sub(prev.at); dt < minPeerRateSampleInterval {
						// A second consumer called back-to-back (two web users
						// focused on the same torrent each get a per-user
						// DetailForFocus tick): dt is microseconds, so a fresh
						// delta would read as 0 B/s or spike — the same
						// multi-reader hazard the torrent-level sampler fixed
						// centrally (see sampleRates). Reuse the previously
						// computed rate and keep the old sample so the next
						// real tick still has a meaningful dt.
						dlRate = prev.rate
						sample = prev
					} else if secs := dt.Seconds(); secs > 0 {
						dlRate = int64(float64(cumDown-prev.down) / secs)
						if dlRate < 0 {
							dlRate = 0
						}
						sample.rate = dlRate
					}
				}
			}
			nextPeers[peerKey] = sample
			d.Peers = append(d.Peers, PeerEntry{
				IP:           ip,
				Port:         port,
				ClientName:   name,
				Flags:        peerFlagsFor(pc),
				Progress:     pieceProgressOf(t, pc),
				DownloadRate: dlRate,
				UploadRate:   clampRate(peerStats.LastWriteUploadRate),
				CountryCode:  "",
			})
		}
		// Drop entries for peers that disconnected since the last tick
		// (they're not in `seen`); nextPeers is already filtered to
		// currently-present peers by virtue of being built from `conns`.
		a.prevPeerRates[id] = nextPeers
		a.rateMu.Unlock()
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

// bep20PeerID derives the 8-byte BEP-20 peer-id prefix from a
// "Mosaic/X.Y.Z" version string. Format is "-MSnnnn-" where nnnn is the
// 4-digit zero-padded numeric version (X*100 + Y*10 + Z, capped at 4
// digits). Returns "" if the input doesn't parse as a recognizable
// semver — anacrolix will fall back to its default Bep20 in that case.
//
// Example: "Mosaic/0.5.7" → "-MS0057-".
func bep20PeerID(clientVersion string) string {
	// Pull the digits after the slash.
	slash := strings.IndexByte(clientVersion, '/')
	if slash < 0 || slash == len(clientVersion)-1 {
		return ""
	}
	parts := strings.SplitN(clientVersion[slash+1:], ".", 4)
	if len(parts) < 2 {
		return ""
	}
	var nums [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		// Strip a leading "v" on the first part if present.
		s := parts[i]
		if i == 0 {
			s = strings.TrimPrefix(s, "v")
		}
		// Only accept the leading run of digits — allows "0.5.7-rc1" → 0,5,7.
		end := 0
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if end == 0 {
			return ""
		}
		n, err := strconv.Atoi(s[:end])
		if err != nil {
			return ""
		}
		nums[i] = n
	}
	combined := nums[0]*100 + nums[1]*10 + nums[2]
	if combined > 9999 {
		combined = 9999
	}
	return fmt.Sprintf("-MS%04d-", combined)
}

// clampRate sanitizes anacrolix's float64 per-peer rate counters before
// they reach our int64 DTO. NaN ((0 piece-data bytes)/(0 ns) for peers
// we're not actually exchanging data with), +Inf (writeDuration sub-
// nanosecond), and negative values all get pinned to safe ranges.
//
// Pre-fix the int64() cast on NaN produced math.MinInt64
// (-9223372036854775808) and the SPA rendered "-922337203685477600 B/s"
// for "majority of peers" because that's the resting state of
// LastWriteUploadRate on conns we never piece-uploaded to.
func clampRate(f float64) int64 {
	if math.IsNaN(f) || f < 0 {
		return 0
	}
	if math.IsInf(f, 1) || f > float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(f)
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

// snapshotFor builds a Snapshot for a torrent. The down/up rate is supplied
// by the caller from the centralized rate cache (see sampleRates) — this
// function no longer computes or stores rate samples, so it is a pure read
// of the torrent and is safe to call concurrently from any number of readers.
