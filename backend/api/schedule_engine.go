package api

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/persistence"
)

// ScheduleEngine ticks once a minute, finds the active schedule rule for now,
// and applies it to the api Service (which propagates to engine rate limits).
type ScheduleEngine struct {
	svc      *Service
	rules    *persistence.ScheduleRules
	location *time.Location

	mu          sync.RWMutex
	lastApplied int // rule ID we last applied (0 = none/cleared)

	stop chan struct{}
}

func NewScheduleEngine(svc *Service, rules *persistence.ScheduleRules, loc *time.Location) *ScheduleEngine {
	if loc == nil {
		loc = time.Local
	}
	se := &ScheduleEngine{svc: svc, rules: rules, location: loc, stop: make(chan struct{})}
	go se.run()
	return se
}

func (se *ScheduleEngine) Close() { close(se.stop) }

func (se *ScheduleEngine) run() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	se.tick(context.Background()) // immediate
	for {
		select {
		case <-se.stop:
			return
		case <-t.C:
			se.tick(context.Background())
		}
	}
}

func (se *ScheduleEngine) tick(ctx context.Context) {
	rules, err := se.rules.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("schedule_engine: list rules")
		return
	}
	now := time.Now().In(se.location)
	dayBit := 1 << int(now.Weekday())
	minutes := now.Hour()*60 + now.Minute()

	var active *persistence.ScheduleRule
	for i := range rules {
		r := &rules[i]
		if !r.Enabled {
			continue
		}
		if r.DaysMask&dayBit == 0 {
			continue
		}
		if minutes < r.StartMin || minutes >= r.EndMin {
			continue
		}
		active = r
		break
	}

	se.mu.Lock()
	prevID := se.lastApplied
	nextID := 0
	if active != nil {
		nextID = active.ID
	}
	se.lastApplied = nextID
	se.mu.Unlock()

	if prevID == nextID {
		return
	}

	if active == nil {
		log.Info().Msg("schedule_engine: no active rule, restoring user-configured limits")
		_ = se.svc.applyLimits(ctx)
		return
	}

	if active.AltOnly {
		l, _ := se.svc.GetLimits(ctx)
		_ = se.svc.engine.SetGlobalRateLimits(l.AltDownKbps*1024, l.AltUpKbps*1024)
		log.Info().Int("rule_id", active.ID).Msg("schedule_engine: applied alt-only rule")
		return
	}

	_ = se.svc.engine.SetGlobalRateLimits(active.DownKbps*1024, active.UpKbps*1024)
	log.Info().Int("rule_id", active.ID).Int("down", active.DownKbps).Int("up", active.UpKbps).
		Msg("schedule_engine: applied rule")
}
