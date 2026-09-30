package dashboard

import (
	"math"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/accountusage"
)

func estimateTestWindow(now time.Time, rate, last float64) accountusage.WindowEstimate {
	return accountusage.WindowEstimate{LastPercent: last, ObservedAt: &now, CostPerPercent: rate, CalibrationWeight: 10, CalibratedAt: &now}
}
func assertUsageBalance(t *testing.T, got UsageEstimateWindow, cost, minutes float64) {
	t.Helper()
	if got.Status != "ready" || got.RemainingCost == nil || *got.RemainingCost != cost || got.RemainingMinutes == nil || *got.RemainingMinutes != minutes {
		t.Fatalf("estimate = %+v, want cost %v and minutes %v", got, cost, minutes)
	}
}
func TestBuildUsageEstimatesSelectsWindowPerAccount(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	w := func(rate, last float64) accountusage.WindowEstimate { return estimateTestWindow(now, rate, last) }
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: accountusage.EstimateMeta{FiveHour: w(1, 50), SevenDay: w(100, 0)}},
		{Plan: "team", Meta: accountusage.EstimateMeta{FiveHour: w(2, 50), SevenDay: w(100, 0)}},
		{Plan: "team", Meta: accountusage.EstimateMeta{SevenDay: w(10, 50)}},
		{Plan: "SELF_SERVE_BUSINESS_PRO_LITE", Meta: accountusage.EstimateMeta{SevenDay: w(20, 50)}},
		{Plan: "pro", Meta: accountusage.EstimateMeta{SevenDay: w(30, 50)}},
		{Plan: "free", Meta: accountusage.EstimateMeta{FiveHour: w(999, 0), SevenDay: w(999, 0)}},
	}, now, 10)
	if len(result) != 1 || result[0].Plan != "non_free" || len(result[0].Windows) != 2 {
		t.Fatalf("result = %+v", result)
	}
	assertUsageBalance(t, result[0].Windows[0], 3150, 315)
	assertUsageBalance(t, result[0].Windows[1], 150, 15)
}
func TestBuildUsageEstimatesUsesObservedWindowsForEveryPlan(t *testing.T) {
	now := time.Now()
	for _, plan := range []string{"plus", "team", "prolite", "pro", "k12", "enterprise", "go"} {
		for _, hasFiveHour := range []bool{false, true} {
			meta := accountusage.EstimateMeta{SevenDay: estimateTestWindow(now, 10, 50)}
			cost, fiveCost := 500.0, 0.0
			if hasFiveHour {
				meta.FiveHour = estimateTestWindow(now, 1, 50)
				cost, fiveCost = 50, 50
			}
			result := BuildUsageEstimates([]UsageEstimateSource{{Plan: plan, Meta: meta}}, now, 1)
			if len(result) != 1 || len(result[0].Windows) != 2 {
				t.Fatalf("%s = %+v", plan, result)
			}
			assertUsageBalance(t, result[0].Windows[0], cost, cost)
			assertUsageBalance(t, result[0].Windows[1], fiveCost, fiveCost)
		}
	}
}
func TestBuildUsageEstimatesUsesLatestWindowPresence(t *testing.T) {
	now := time.Now()
	w := estimateTestWindow(now, 2, 50)
	meta := accountusage.EstimateMeta{FiveHour: w, SevenDay: w}
	meta.ObserveWindows(false, now)
	result := BuildUsageEstimates([]UsageEstimateSource{{Plan: "team", Meta: meta}}, now, 1)
	assertUsageBalance(t, result[0].Windows[0], 100, 100)
	assertUsageBalance(t, result[0].Windows[1], 0, 0)
}
func TestBuildUsageEstimatesSharesCalibrationOnlyWithinPlanAndWindow(t *testing.T) {
	now := time.Now()
	first, second := estimateTestWindow(now, 1, 50), estimateTestWindow(now, 3, 50)
	second.CalibrationWeight = 30
	unknown := accountusage.WindowEstimate{ObservedAt: &now, LastPercent: 50}
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "team", Meta: accountusage.EstimateMeta{FiveHour: first}},
		{Plan: "team", Meta: accountusage.EstimateMeta{FiveHour: second}},
		{Plan: "team", Meta: accountusage.EstimateMeta{FiveHour: unknown}},
		{Plan: "team", Meta: accountusage.EstimateMeta{SevenDay: unknown}},
		{Plan: "pro", Meta: accountusage.EstimateMeta{SevenDay: unknown}},
	}, now, 1)
	assertUsageBalance(t, result[0].Windows[0], 375, 375)
	assertUsageBalance(t, result[0].Windows[1], 375, 375)
}
func TestBuildUsageEstimatesFreshnessAndZeroConsumption(t *testing.T) {
	now := time.Now()
	observedAt := now.Add(-12 * time.Hour)
	w := estimateTestWindow(now, 2, 50)
	w.ObservedAt = &observedAt
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "team", Meta: accountusage.EstimateMeta{FiveHour: w, SevenDay: w}},
		{Plan: "team", Meta: accountusage.EstimateMeta{SevenDay: w}},
	}, now, 0)
	total, five := result[0].Windows[0], result[0].Windows[1]
	if total.RemainingCost == nil || *total.RemainingCost != 100 || total.RemainingMinutes != nil {
		t.Fatalf("total = %+v", total)
	}
	if five.RemainingCost != nil {
		t.Fatalf("stale 5h must not fall back to 7d: %+v", five)
	}
	w.LastPercent = 100
	result = BuildUsageEstimates([]UsageEstimateSource{{Plan: "pro", Meta: accountusage.EstimateMeta{SevenDay: w}}}, now, 0)
	assertUsageBalance(t, result[0].Windows[0], 0, 0)
}
func TestBuildUsageEstimatesEmptyAndUnknown(t *testing.T) {
	now := time.Now()
	for _, sources := range [][]UsageEstimateSource{nil, {{Plan: "free"}}, {{Plan: ""}}} {
		if got := BuildUsageEstimates(sources, now, 1); len(got) != 0 {
			t.Fatalf("excluded = %+v", got)
		}
	}
	got := BuildUsageEstimates([]UsageEstimateSource{{Plan: "plus"}}, now, 1)
	if got[0].Windows[0].Status != "insufficient" || got[0].Windows[0].RemainingCost != nil {
		t.Fatalf("unknown = %+v", got)
	}
}

func TestUsageEstimateWindowSkipsUnavailableObservations(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	staleAt := now.Add(-25 * time.Hour)
	futureAt := now.Add(6 * time.Minute)
	unavailable := []struct {
		name   string
		window accountusage.WindowEstimate
	}{
		{name: "missing"},
		{name: "stale", window: accountusage.WindowEstimate{LastPercent: 25, ObservedAt: &staleAt}},
		{name: "future", window: accountusage.WindowEstimate{LastPercent: 25, ObservedAt: &futureAt}},
		{name: "negative", window: accountusage.WindowEstimate{LastPercent: -1, ObservedAt: &now}},
		{name: "nan", window: accountusage.WindowEstimate{LastPercent: math.NaN(), ObservedAt: &now}},
		{name: "infinite", window: accountusage.WindowEstimate{LastPercent: math.Inf(1), ObservedAt: &now}},
	}
	for _, candidate := range unavailable {
		t.Run(candidate.name, func(t *testing.T) {
			known := accountusage.WindowEstimate{
				LastPercent: 50, ObservedAt: &now,
				CostPerPercent: 1, CalibrationWeight: 10, CalibratedAt: &now,
			}
			pool := usageEstimateWindowPool{observations: []usageEstimateObservation{
				{plan: "plus", window: known},
				{plan: "plus", window: candidate.window},
			}}
			result := pool.estimate("7d", []string{"plus"}, nil, 2, now, usage7dObservationAge)
			if result.Status != "ready" || result.FullCost != 200 || result.RemainingCost == nil || *result.RemainingCost != 50 ||
				result.RemainingMinutes == nil || *result.RemainingMinutes != 25 {
				t.Fatalf("partial estimate = %+v", result)
			}
		})
	}
}

func TestUsageEstimateWindowDistinguishesUnknownAndExhaustedRemainingCost(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	pool := usageEstimateWindowPool{observations: []usageEstimateObservation{
		{plan: "plus", window: accountusage.WindowEstimate{
			CostPerPercent: 1, CalibrationWeight: 10, CalibratedAt: &now,
		}},
		{plan: "plus"},
	}}
	result := pool.estimate("7d", []string{"plus"}, nil, 0, now, usage7dObservationAge)
	if result.RemainingCost != nil || result.RemainingMinutes != nil {
		t.Fatalf("unknown remaining cost must not be reported as zero: %+v", result)
	}
	pool.observations[0].window.ObservedAt = &now
	pool.observations[0].window.LastPercent = 100
	result = pool.estimate("7d", []string{"plus"}, nil, 0, now, usage7dObservationAge)
	if result.Status != "ready" || result.RemainingCost == nil || *result.RemainingCost != 0 ||
		result.RemainingMinutes == nil || *result.RemainingMinutes != 0 {
		t.Fatalf("known exhausted account should report zero remaining cost: %+v", result)
	}
}

func TestUsageEstimateFreshnessWindows(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	calibratedAt := now.Add(-6 * 24 * time.Hour)
	window := accountusage.WindowEstimate{CostPerPercent: 2, CalibrationWeight: 10, CalibratedAt: &calibratedAt}
	if !usageCalibrationValid(window, now) {
		t.Fatal("six-day calibration should remain usable")
	}
	expiredAt := now.Add(-8 * 24 * time.Hour)
	window.CalibratedAt = &expiredAt
	if usageCalibrationValid(window, now) {
		t.Fatal("eight-day calibration should expire")
	}
	freshObservedAt := now.Add(-5 * time.Hour)
	staleObservedAt := now.Add(-7 * time.Hour)
	if !observationFresh(&freshObservedAt, now, usage5hObservationAge) || observationFresh(&staleObservedAt, now, usage5hObservationAge) {
		t.Fatal("5h observation freshness boundary is incorrect")
	}
}
