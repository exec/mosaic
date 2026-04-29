package api

import (
	"context"
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
	ctx := context.Background()

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
	ctx := context.Background()
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

func TestService_ScheduleRuleCRUD_RoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

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
