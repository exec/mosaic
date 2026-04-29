package engine

import (
	"sort"
	"sync"
	"time"
)

// Scheduler enforces the active-torrent-slot limits by ScheduledPause-ing
// overflow torrents and resuming them when slots free up. It does NOT
// touch torrents that the user has manually paused.
type Scheduler struct {
	engine *Engine

	mu                 sync.RWMutex
	maxActiveDownloads int // 0 = unlimited
	maxActiveSeeds     int

	stop chan struct{}
}

func NewScheduler(eng *Engine, maxDL, maxSeeds int, tickEvery time.Duration) *Scheduler {
	s := &Scheduler{engine: eng, maxActiveDownloads: maxDL, maxActiveSeeds: maxSeeds, stop: make(chan struct{})}
	go s.run(tickEvery)
	return s
}

func (s *Scheduler) SetLimits(maxDL, maxSeeds int) {
	s.mu.Lock()
	s.maxActiveDownloads = maxDL
	s.maxActiveSeeds = maxSeeds
	s.mu.Unlock()
}

func (s *Scheduler) Limits() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxActiveDownloads, s.maxActiveSeeds
}

func (s *Scheduler) Close() { close(s.stop) }

func (s *Scheduler) run(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	s.mu.RLock()
	maxDL, maxSeeds := s.maxActiveDownloads, s.maxActiveSeeds
	s.mu.RUnlock()

	all := s.engine.List()

	// Partition: downloading vs seeding (completed). Skip user-paused torrents.
	var downloading, seeding []Snapshot
	for _, snap := range all {
		if snap.Paused {
			continue // user-paused, leave alone (also clear any scheduler hold)
		}
		if snap.Completed {
			seeding = append(seeding, snap)
		} else {
			downloading = append(downloading, snap)
		}
	}

	apply := func(group []Snapshot, max int) {
		sort.Slice(group, func(i, j int) bool {
			// Force-started first, then by queue_position ascending
			if group[i].ForceStart != group[j].ForceStart {
				return group[i].ForceStart
			}
			return group[i].QueuePosition < group[j].QueuePosition
		})
		// Active count includes force-starts; max=0 means unlimited
		active := 0
		for _, snap := range group {
			shouldRun := snap.ForceStart || max == 0 || active < max
			if shouldRun {
				if snap.Queued {
					s.engine.ScheduledPause(snap.ID, false)
				}
				active++
			} else {
				if !snap.Queued {
					s.engine.ScheduledPause(snap.ID, true)
				}
			}
		}
	}

	apply(downloading, maxDL)
	apply(seeding, maxSeeds)
}
