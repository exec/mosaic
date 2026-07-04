package engine

import (
	"errors"
	"time"

	"github.com/anacrolix/torrent"
	"golang.org/x/time/rate"
)

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

// SetTorrentRateLimits sets per-torrent download/upload caps in bytes/sec.
// 0 means unlimited. Enforcement uses a background duty-cycle goroutine
// registered on verifyWg so Close() waits for it to drain. If the torrent
// already has a limiter goroutine running (prior call to SetTorrentRateLimits)
// it will pick up the new limits on its next tick — no new goroutine is
// spawned for subsequent updates.
func (a *AnacrolixBackend) SetTorrentRateLimits(id TorrentID, downBPS, upBPS int64) error {
	if downBPS < 0 {
		downBPS = 0
	}
	if upBPS < 0 {
		upBPS = 0
	}
	a.perLimitMu.Lock()
	a.perTorrentDown[id] = downBPS
	a.perTorrentUp[id] = upBPS
	// Lifecycle is tracked via limiterRunning, NOT the previous limit values:
	// a clear-to-(0,0) followed by a non-zero set within one tick leaves the
	// old goroutine alive (it never observes the transient 0,0), so spawning
	// another would race it. The goroutine clears the flag under perLimitMu
	// with a limits re-check before exiting, so set-after-clear either reuses
	// the live limiter or deterministically starts a fresh one.
	needsStart := (downBPS != 0 || upBPS != 0) && !a.limiterRunning[id]
	if needsStart {
		a.limiterRunning[id] = true
	}
	a.perLimitMu.Unlock()

	if needsStart {
		t, ok := a.find(id)
		if !ok {
			a.perLimitMu.Lock()
			delete(a.limiterRunning, id)
			a.perLimitMu.Unlock()
			return errors.New("not found")
		}
		a.verifyWg.Add(1)
		go a.runPerTorrentLimiter(id, t)
	} else if downBPS == 0 && upBPS == 0 {
		// Limits cleared — ensure download/upload are allowed
		// (the goroutine may have left them disallowed).
		t, ok := a.find(id)
		if !ok {
			return nil
		}
		t.AllowDataDownload()
		t.AllowDataUpload()
	}
	return nil
}

// runPerTorrentLimiter is a background goroutine that enforces per-torrent
// rate limits by sampling byte counters every 500ms and using
// DisallowDataDownload/AllowDataDownload (and the Upload equivalents) to
// implement a duty-cycle approximation.
//
// The approach: each 500ms tick we read how many bytes were transferred since
// the last tick. If the torrent exceeded its budget (bytes_this_tick >
// limit_per_500ms), we disallow data for the overrun fraction of the next
// interval. Otherwise we allow data. This is a coarse token-bucket; actual
// throughput will hover around the configured cap ±1 tick's worth (~one
// per-piece chunk, ~16 KB by default in anacrolix) rather than being
// byte-perfect.
//
// The goroutine exits when: (a) engineCtx is cancelled (engine shutdown), or
// (b) both down and up limits drop to 0 (user cleared the limit).
func (a *AnacrolixBackend) runPerTorrentLimiter(id TorrentID, t *torrent.Torrent) {
	defer a.verifyWg.Done()
	const interval = 500 * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prevDown, prevUp int64
	var dlBlocked, ulBlocked bool

	// Helper: read current cumulative stats from anacrolix.
	readStats := func() (down, up int64) {
		s := t.Stats()
		return s.BytesReadData.Int64(), s.BytesWrittenData.Int64()
	}

	prevDown, prevUp = readStats()

	for {
		select {
		case <-a.engineCtx.Done():
			// Engine shutting down — re-enable before exit so state is clean.
			if dlBlocked {
				t.AllowDataDownload()
			}
			if ulBlocked {
				t.AllowDataUpload()
			}
			return
		case <-ticker.C:
		}

		a.perLimitMu.RLock()
		downLimit := a.perTorrentDown[id]
		upLimit := a.perTorrentUp[id]
		a.perLimitMu.RUnlock()

		// Both limits cleared — re-enable and exit. Confirm under the write
		// lock and clear limiterRunning atomically with the re-check: a
		// SetTorrentRateLimits that re-set non-zero limits between our
		// RLock read and here must either be observed (keep running) or
		// happen strictly after the flag is cleared (spawns a fresh
		// limiter). Without the re-check this goroutine could exit just as
		// the setter saw it as still-running and skipped the spawn,
		// leaving the torrent unlimited.
		if downLimit == 0 && upLimit == 0 {
			a.perLimitMu.Lock()
			if a.perTorrentDown[id] != 0 || a.perTorrentUp[id] != 0 {
				a.perLimitMu.Unlock()
				continue
			}
			delete(a.limiterRunning, id)
			a.perLimitMu.Unlock()
			if dlBlocked {
				t.AllowDataDownload()
			}
			if ulBlocked {
				t.AllowDataUpload()
			}
			return
		}

		curDown, curUp := readStats()
		deltaDown := curDown - prevDown
		deltaUp := curUp - prevUp
		prevDown, prevUp = curDown, curUp

		// Budget per interval (bytes allowed per 500ms tick).
		budgetDown := int64(float64(downLimit) * interval.Seconds())
		budgetUp := int64(float64(upLimit) * interval.Seconds())

		// Download enforcement.
		if downLimit > 0 {
			// Check paused state — if paused we don't manage download allow/disallow
			// to avoid conflicting with Pause()'s DisallowDataDownload.
			a.pausedMu.RLock()
			isPaused := a.paused[id]
			a.pausedMu.RUnlock()
			if !isPaused {
				if deltaDown > budgetDown {
					// Over budget — disallow for next tick.
					if !dlBlocked {
						t.DisallowDataDownload()
						dlBlocked = true
					}
				} else {
					// Under budget — allow.
					if dlBlocked {
						t.AllowDataDownload()
						dlBlocked = false
					}
				}
			}
		} else if dlBlocked {
			t.AllowDataDownload()
			dlBlocked = false
		}

		// Upload enforcement.
		if upLimit > 0 {
			if deltaUp > budgetUp {
				if !ulBlocked {
					t.DisallowDataUpload()
					ulBlocked = true
				}
			} else {
				if ulBlocked {
					t.AllowDataUpload()
					ulBlocked = false
				}
			}
		} else if ulBlocked {
			t.AllowDataUpload()
			ulBlocked = false
		}
	}
}

// AddTracker adds a single tracker URL to an existing torrent in its own tier.
// anacrolix's AddTrackers takes a slice of tiers ([][]string); we pass one
// tier containing the single URL so it doesn't compete with metainfo trackers.
