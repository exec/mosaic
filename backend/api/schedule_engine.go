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
	lastApplied appliedRule // rule + limits we last applied (zero = none/cleared)

	stop chan struct{}
}

// appliedRule is the change-detection key for tick: the active rule's ID plus
// the limit values we applied for it. Keying on ID alone meant editing the
// currently-active rule's limits did nothing until the rule deactivated and
// reactivated — the edited values weren't re-applied because "same rule".
type appliedRule struct {
	id       int
	downKbps int
	upKbps   int
	altOnly  bool
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
	ctx := WithCaller(context.Background(), SystemCaller)
	se.tick(ctx) // immediate
	for {
		select {
		case <-se.stop:
			return
		case <-t.C:
			se.tick(ctx)
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
		if !ruleWindowContains(r.StartMin, r.EndMin, minutes) {
			continue
		}
		active = r
		break
	}

	se.mu.Lock()
	prev := se.lastApplied
	var next appliedRule
	if active != nil {
		next = appliedRule{id: active.ID, downKbps: active.DownKbps, upKbps: active.UpKbps, altOnly: active.AltOnly}
	}
	se.lastApplied = next
	se.mu.Unlock()

	if prev == next {
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

// ruleWindowContains reports whether a rule's [startMin, endMin) window
// contains the given minutes-since-midnight. startMin > endMin means the
// window wraps midnight (e.g. 22:00–06:00, StartMin=1320 EndMin=360): the
// rule is active past the start OR before the end. startMin == endMin is an
// empty window, matching the pre-wrap behavior.
func ruleWindowContains(startMin, endMin, minutes int) bool {
	if startMin <= endMin {
		return minutes >= startMin && minutes < endMin
	}
	return minutes >= startMin || minutes < endMin
}
