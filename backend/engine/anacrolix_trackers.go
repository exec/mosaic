package engine

import (
	"github.com/anacrolix/torrent"
	anacrolix_types "github.com/anacrolix/torrent/types"
)

func (a *AnacrolixBackend) AddTracker(id TorrentID, url string) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	t.AddTrackers([][]string{{url}})
	return nil
}

// RemoveTracker removes a single tracker URL from an existing torrent.
// anacrolix v1.61 exposes ModifyTrackers which replaces the entire announce
// list atomically — it stops existing announcers and rebuilds from the
// provided list. We reconstruct the announce list without the target URL and
// pass it to ModifyTrackers. The call is a no-op if the URL doesn't appear.
func (a *AnacrolixBackend) RemoveTracker(id TorrentID, url string) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	mi := t.Metainfo()
	// Build filtered announce list, excluding the target URL from every tier.
	var newList [][]string
	for _, tier := range mi.AnnounceList {
		var newTier []string
		for _, u := range tier {
			if u != url {
				newTier = append(newTier, u)
			}
		}
		if len(newTier) > 0 {
			newList = append(newList, newTier)
		}
	}
	// Also handle the legacy single-announce field by treating it as a tier.
	if mi.Announce != "" && mi.Announce != url {
		// Only add it if it isn't already covered by AnnounceList to avoid dups.
		covered := false
		for _, tier := range newList {
			for _, u := range tier {
				if u == mi.Announce {
					covered = true
					break
				}
			}
		}
		if !covered {
			newList = append([][]string{{mi.Announce}}, newList...)
		}
	}
	// ModifyTrackers stops all existing tracker goroutines and replaces the
	// announce list atomically. This is the correct removal mechanism in
	// anacrolix v1.61.
	t.ModifyTrackers(newList)
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

// SetSequential enables or disables sequential piece download for a torrent.
// When enabled, pieces are assigned descending priorities so that earlier
// pieces have higher urgency — anacrolix then requests them in index order,
// allowing users to preview/stream media while the download is in progress.
// When disabled, piece priorities are reset to Normal (rarest-first default).
//
// The gradient uses all non-None priority levels (Normal..Now, values 1–5).
// Pieces are divided evenly into up to 5 buckets; the first bucket gets the
// highest priority (PiecePriorityNow) and the last gets PiecePriorityNormal.
// This ensures that even with a large number of pieces the request strategy
// strongly prefers lower-indexed pieces for the whole file while still allowing
// anacrolix's internal tie-breaking to handle unavailable / already-complete
// pieces gracefully.
func (a *AnacrolixBackend) SetSequential(id TorrentID, enabled bool) {
	a.pausedMu.Lock()
	a.sequential[id] = enabled
	a.pausedMu.Unlock()
	t, ok := a.find(id)
	if !ok {
		return
	}
	applySequentialPriorities(t, enabled)
}

// applySequentialPriorities adjusts per-piece priorities on t to implement
// sequential (in-order) downloading. It must be called after GotInfo (piece
// count is valid). Safe to call from any goroutine — Piece.SetPriority takes
// the client lock internally.
func applySequentialPriorities(t *torrent.Torrent, sequential bool) {
	n := t.NumPieces()
	if n == 0 {
		return
	}
	if !sequential {
		// Reset to Normal so the default rarest-first strategy takes over.
		for i := 0; i < n; i++ {
			t.Piece(i).SetPriority(anacrolix_types.PiecePriorityNormal)
		}
		return
	}
	// Divide pieces into up to 5 buckets mapped to priority levels
	// PiecePriorityNow (5) → PiecePriorityNormal (1).
	const levels = 5 // PiecePriorityNow=5 down to PiecePriorityNormal=1
	for i := 0; i < n; i++ {
		// bucket: 0 = earliest (highest priority), levels-1 = latest (lowest)
		bucket := (i * levels) / n
		if bucket >= levels {
			bucket = levels - 1
		}
		// prio: levels (Now) for bucket 0, 1 (Normal) for bucket levels-1
		prio := anacrolix_types.PiecePriority(levels - bucket)
		t.Piece(i).SetPriority(prio)
	}
}

// ScheduledPause is the scheduler's pause channel — independent from the
// user's manual Pause. It uses the same SetMaxEstablishedConns(0/cap) trick
// the manual Pause uses, but writes only the scheduledPause map flag so
// snapshots can distinguish "user-paused" from "queue-held".
//
// CRITICAL: ScheduledPause(id, false) MUST refuse to re-enable a
// user-paused torrent. The scheduler reads a List() snapshot once per
// tick; if the user clicks Pause between the List() read and the
// per-id apply loop, the scheduler will try to ScheduledPause(false)
// the now-paused torrent. Without this guard, SetMaxEstablishedConns(80)
// fires and peers reconnect to a "paused" torrent. Pause's
// DisallowDataDownload prevents new piece requests but uploads + half-
// open conns can still leak through — and the UX bug ("paused torrent
// is still downloading") is exactly this race.
func (a *AnacrolixBackend) ScheduledPause(id TorrentID, paused bool) {
	t, ok := a.find(id)
	if !ok {
		return
	}
	if !paused {
		// Re-enable path — refuse if the user has paused this torrent.
		a.pausedMu.RLock()
		userPaused := a.paused[id]
		a.pausedMu.RUnlock()
		if userPaused {
			return
		}
	}
	if paused {
		t.SetMaxEstablishedConns(0)
	} else {
		t.SetMaxEstablishedConns(int(a.maxConnsPerTorrent.Load()))
	}
	a.pausedMu.Lock()
	a.scheduledPause[id] = paused
	a.pausedMu.Unlock()
}
