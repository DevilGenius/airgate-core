package dashboard

import (
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/accountusage"
)

func TestBuildUsageEstimatesUsesMedianForNewAccounts(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	calibrated := func(rate, growth, last float64) accountusage.EstimateMeta {
		return accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", DailyGrowth: growth, LastPercent: last,
			CostPerPercent: rate, CalibrationWeight: 20, CalibratedAt: &now, ObservedAt: &now,
		}}
	}
	sources := []UsageEstimateSource{
		{Plan: "plus", Meta: calibrated(0.5, 20, 50)},
		{Plan: "team", Meta: calibrated(0.5, 40, 60)},
		{Plan: "plus", Meta: accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{GrowthDate: "2026-08-24", ObservedAt: &now}}},
	}
	result := BuildUsageEstimates(sources, now, 1)
	if len(result) != 1 || len(result[0].Windows) != 1 {
		t.Fatalf("result = %+v", result)
	}
	window := result[0].Windows[0]
	if window.Status != "ready" || window.FullCost != 150 || window.DailyGrowthPercent != 20 || window.RemainingCost == nil || *window.RemainingCost != 95 || window.RemainingMinutes == nil || *window.RemainingMinutes != 95 {
		t.Fatalf("window = %+v", window)
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
