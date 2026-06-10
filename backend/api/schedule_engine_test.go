package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// absentDayMask returns a single day-bit not present in mask.
func absentDayMask(mask int) int {
	for d := 0; d < 7; d++ {
		if mask&(1<<d) == 0 {
			return 1 << d
		}
	}
	return 0
}

func TestScheduleEngine_AppliesActiveRule(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()

	require.NoError(t, svc.SetLimits(ctx, LimitsDTO{DownKbps: 100, UpKbps: 50, AltDownKbps: 10, AltUpKbps: 5}))

	now := time.Now()
	dayBit := 1 << int(now.Weekday())
	startMin := now.Hour()*60 + now.Minute() - 1
	if startMin < 0 {
		startMin = 0
	}
	endMin := startMin + 5
	id, err := svc.CreateScheduleRule(ctx, ScheduleRuleDTO{
		DaysMask: dayBit, StartMin: startMin, EndMin: endMin,
		DownKbps: 999, UpKbps: 333, AltOnly: false, Enabled: true,
	})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	se := NewScheduleEngine(svc, svc.scheduleRules, time.Local)
	t.Cleanup(se.Close)

	require.Eventually(t, func() bool {
		d, u := fb.GlobalRateLimits()
		return d == 999*1024 && u == 333*1024
	}, 2*time.Second, 25*time.Millisecond)
}

func TestScheduleEngine_NoActiveRule_RestoresUserLimits(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()
	require.NoError(t, svc.SetLimits(ctx, LimitsDTO{DownKbps: 200, UpKbps: 100}))

	dayBitNow := 1 << int(time.Now().Weekday())
	otherDay := absentDayMask(dayBitNow)
	_, err := svc.CreateScheduleRule(ctx, ScheduleRuleDTO{
		DaysMask: otherDay, StartMin: 0, EndMin: 1440,
		DownKbps: 1, UpKbps: 1, Enabled: true,
	})
	require.NoError(t, err)

	se := NewScheduleEngine(svc, svc.scheduleRules, time.Local)
	t.Cleanup(se.Close)

	require.Eventually(t, func() bool {
		d, u := fb.GlobalRateLimits()
		return d == 200*1024 && u == 100*1024
	}, 2*time.Second, 25*time.Millisecond)
}

func TestRuleWindowContains(t *testing.T) {
	cases := []struct {
		name                      string
		startMin, endMin, minutes int
		want                      bool
	}{
		{"normal window, inside", 9 * 60, 17 * 60, 12 * 60, true},
		{"normal window, before start", 9 * 60, 17 * 60, 8 * 60, false},
		{"normal window, at start", 9 * 60, 17 * 60, 9 * 60, true},
		{"normal window, at end (exclusive)", 9 * 60, 17 * 60, 17 * 60, false},
		{"empty window never matches", 10 * 60, 10 * 60, 10 * 60, false},
		{"midnight wrap, late evening", 22 * 60, 6 * 60, 23 * 60, true},
		{"midnight wrap, at start", 22 * 60, 6 * 60, 22 * 60, true},
		{"midnight wrap, early morning", 22 * 60, 6 * 60, 3 * 60, true},
		{"midnight wrap, at end (exclusive)", 22 * 60, 6 * 60, 6 * 60, false},
		{"midnight wrap, midday outside", 22 * 60, 6 * 60, 12 * 60, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ruleWindowContains(tc.startMin, tc.endMin, tc.minutes))
		})
	}
}

// TestScheduleEngine_EditingActiveRuleReapplies covers the change-detection
// key: editing the currently-active rule's limits must be re-applied on the
// next tick, not deferred until the rule deactivates and reactivates.
func TestScheduleEngine_EditingActiveRuleReapplies(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()

	now := time.Now()
	dayBit := 1 << int(now.Weekday())
	startMin := now.Hour()*60 + now.Minute() - 1
	if startMin < 0 {
		startMin = 0
	}
	endMin := startMin + 10
	id, err := svc.CreateScheduleRule(ctx, ScheduleRuleDTO{
		DaysMask: dayBit, StartMin: startMin, EndMin: endMin,
		DownKbps: 999, UpKbps: 333, Enabled: true,
	})
	require.NoError(t, err)

	// Drive ticks directly (no goroutine) so the test is deterministic.
	se := &ScheduleEngine{svc: svc, rules: svc.scheduleRules, location: time.Local, stop: make(chan struct{})}
	se.tick(ctx)
	d, u := fb.GlobalRateLimits()
	require.Equal(t, 999*1024, d)
	require.Equal(t, 333*1024, u)

	// Edit the active rule's limits — same ID, same window.
	require.NoError(t, svc.UpdateScheduleRule(ctx, ScheduleRuleDTO{
		ID: id, DaysMask: dayBit, StartMin: startMin, EndMin: endMin,
		DownKbps: 555, UpKbps: 111, Enabled: true,
	}))
	se.tick(ctx)
	d, u = fb.GlobalRateLimits()
	require.Equal(t, 555*1024, d)
	require.Equal(t, 111*1024, u)
}

func TestService_ScheduleRuleCRUD_RoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := sysCtx()

	id, err := svc.CreateScheduleRule(ctx, ScheduleRuleDTO{
		DaysMask: 0b0111110, StartMin: 22 * 60, EndMin: 6 * 60,
		DownKbps: 500, UpKbps: 100, AltOnly: true, Enabled: true,
	})
	require.NoError(t, err)

	rules, err := svc.ListScheduleRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, id, rules[0].ID)
	require.Equal(t, 0b0111110, rules[0].DaysMask)
	require.True(t, rules[0].AltOnly)

	require.NoError(t, svc.UpdateScheduleRule(ctx, ScheduleRuleDTO{
		ID: id, DaysMask: 1, StartMin: 0, EndMin: 60,
		DownKbps: 1, UpKbps: 1, AltOnly: false, Enabled: false,
	}))
	rules, _ = svc.ListScheduleRules(ctx)
	require.Equal(t, 1, rules[0].DaysMask)
	require.False(t, rules[0].AltOnly)
	require.False(t, rules[0].Enabled)

	require.NoError(t, svc.DeleteScheduleRule(ctx, id))
	rules, _ = svc.ListScheduleRules(ctx)
	require.Len(t, rules, 0)
}
