package dashboard

import (
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/accountusage"
)

func TestBuildUsageEstimatesUsesPlanStandardForNewAccounts(t *testing.T) {
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

func TestBuildUsageEstimatesUsesRequiredPlanAnchors(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	calibrated := func(rate, weight float64) accountusage.EstimateMeta {
		return accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", DailyGrowth: 20, LastPercent: 50,
			CostPerPercent: rate, CalibrationWeight: weight, CalibratedAt: &now, ObservedAt: &now,
		}}
	}
	uncalibrated := accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
		GrowthDate: "2026-08-24", LastPercent: 50, ObservedAt: &now,
	}}

	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: calibrated(1, 10)},
		{Plan: "team", Meta: uncalibrated},
	}, now, 1)
	if len(result) != 1 || result[0].Windows[0].Status != "ready" || result[0].Windows[0].FullCost != 100 {
		t.Fatalf("team without a standard should be omitted from the conservative Plus estimate: %+v", result)
	}

	result = BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: calibrated(1, 10)},
		{Plan: "pro", Meta: uncalibrated},
	}, now, 1)
	if len(result) != 2 || result[0].Windows[0].Status != "ready" ||
		result[1].Windows[0].Status != "insufficient" {
		t.Fatalf("the Pro estimate requires both Plus and Pro standards: %+v", result)
	}

	result = BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "team", Meta: calibrated(1, 10)},
		{Plan: "pro", Meta: calibrated(2, 10)},
	}, now, 1)
	if len(result) != 2 || result[0].Windows[0].Status != "insufficient" ||
		result[1].Windows[0].Status != "ready" || result[1].Windows[0].FullCost != 300 {
		t.Fatalf("Team must not replace the left Plus anchor, while Pro remains independently usable: %+v", result)
	}

	result = BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "pro", Meta: calibrated(2, 10)},
	}, now, 1)
	if len(result) != 1 || result[0].Plan != "pro" || result[0].Windows[0].Status != "ready" ||
		result[0].Windows[0].FullCost != 200 {
		t.Fatalf("a Pro-only pool should estimate from its Pro standard: %+v", result)
	}
}

func TestBuildUsageEstimatesSharesWeightedStandardWithinPlan(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	calibrated := func(rate, weight float64) accountusage.EstimateMeta {
		return accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", DailyGrowth: 10, LastPercent: 50,
			CostPerPercent: rate, CalibrationWeight: weight, CalibratedAt: &now, ObservedAt: &now,
		}}
	}
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: calibrated(1, 10)},
		{Plan: "plus", Meta: calibrated(3, 30)},
		{Plan: "plus", Meta: accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", LastPercent: 50, ObservedAt: &now,
		}}},
	}, now, 1)
	if len(result) != 1 || len(result[0].Windows) != 1 {
		t.Fatalf("result = %+v", result)
	}
	window := result[0].Windows[0]
	// Shared Plus standard = (1*10 + 3*30) / 40 = $2.5 per percent.
	if window.Status != "ready" || window.FullCost != 750 || window.RemainingCost == nil || *window.RemainingCost != 375 {
		t.Fatalf("shared weighted standard = %+v", window)
	}
}

func TestBuildUsageEstimatesWithoutCurrentConsumptionLeavesDurationUnbounded(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	meta := accountusage.EstimateMeta{SevenDay: accountusage.WindowEstimate{
		GrowthDate: "2026-08-24", DailyGrowth: 10, LastPercent: 50,
		CostPerPercent: 1, CalibrationWeight: 10, CalibratedAt: &now, ObservedAt: &now,
	}}

	result := BuildUsageEstimates([]UsageEstimateSource{{Plan: "plus", Meta: meta}}, now, 0)
	if len(result) != 1 || len(result[0].Windows) != 1 {
		t.Fatalf("result = %+v", result)
	}
	window := result[0].Windows[0]
	if window.Status != "ready" || window.RemainingCost == nil || *window.RemainingCost != 50 ||
		window.RemainingMinutes != nil {
		t.Fatalf("zero current consumption estimate = %+v", window)
	}
}

func TestProShortTermEstimateUsesSevenDayWhenProHasNoFiveHour(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	window := func(rate, last float64) accountusage.WindowEstimate {
		return accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", DailyGrowth: 10, LastPercent: last,
			CostPerPercent: rate, CalibrationWeight: 10, CalibratedAt: &now, ObservedAt: &now,
		}
	}
	proSevenDay := window(2, 50)
	proObservedAt := now.Add(-12 * time.Hour)
	proSevenDay.ObservedAt = &proObservedAt
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: accountusage.EstimateMeta{
			FiveHour: window(1, 100),
			SevenDay: window(1, 50),
		}},
		{Plan: "pro", Meta: accountusage.EstimateMeta{
			SevenDay: proSevenDay,
		}},
	}, now, 1)
	if len(result) != 2 || len(result[1].Windows) != 2 {
		t.Fatalf("result = %+v", result)
	}
	shortTerm := result[1].Windows[0]
	if shortTerm.Window != "5h" || shortTerm.Status != "ready" || shortTerm.FullCost != 300 ||
		shortTerm.RemainingCost == nil || *shortTerm.RemainingCost != 100 ||
		shortTerm.RemainingMinutes == nil || *shortTerm.RemainingMinutes != 100 {
		t.Fatalf("Pro short-term estimate should combine Plus 5h and Pro 7d: %+v", shortTerm)
	}
}

func TestShortTermEstimateChoosesFiveHourOrSevenDayPerPlan(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	window := func(rate, last float64) accountusage.WindowEstimate {
		return accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", DailyGrowth: 10, LastPercent: last,
			CostPerPercent: rate, CalibrationWeight: 10, CalibratedAt: &now, ObservedAt: &now,
		}
	}
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: accountusage.EstimateMeta{
			FiveHour: window(1, 100),
			SevenDay: window(1, 50),
		}},
		{Plan: "team", Meta: accountusage.EstimateMeta{
			SevenDay: window(2, 50),
		}},
		{Plan: "k12", Meta: accountusage.EstimateMeta{
			FiveHour: window(3, 50),
			SevenDay: window(4, 50),
		}},
	}, now, 1)
	if len(result) != 1 || len(result[0].Windows) != 2 {
		t.Fatalf("result = %+v", result)
	}
	shortTerm := result[0].Windows[0]
	if shortTerm.Window != "5h" || shortTerm.Status != "ready" || shortTerm.FullCost != 600 ||
		shortTerm.RemainingCost == nil || *shortTerm.RemainingCost != 250 {
		t.Fatalf("short-term estimate should use Plus 5h + Team 7d + K12 5h: %+v", shortTerm)
	}
	sevenDay := result[0].Windows[1]
	if sevenDay.Window != "7d" || sevenDay.Status != "ready" || sevenDay.FullCost != 700 ||
		sevenDay.RemainingCost == nil || *sevenDay.RemainingCost != 350 {
		t.Fatalf("7d estimate should use every plan's 7d window: %+v", sevenDay)
	}
}

func TestFiveHourRowVisibilityDependsOnlyOnPlus(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	window := func(rate float64) accountusage.WindowEstimate {
		return accountusage.WindowEstimate{
			GrowthDate: "2026-08-24", DailyGrowth: 10, LastPercent: 50,
			CostPerPercent: rate, CalibrationWeight: 10, CalibratedAt: &now, ObservedAt: &now,
		}
	}
	result := BuildUsageEstimates([]UsageEstimateSource{
		{Plan: "plus", Meta: accountusage.EstimateMeta{SevenDay: window(1)}},
		{Plan: "team", Meta: accountusage.EstimateMeta{FiveHour: window(2), SevenDay: window(2)}},
		{Plan: "k12", Meta: accountusage.EstimateMeta{FiveHour: window(3), SevenDay: window(3)}},
		{Plan: "pro", Meta: accountusage.EstimateMeta{SevenDay: window(4)}},
	}, now, 1)
	if len(result) != 2 || len(result[0].Windows) != 1 || result[0].Windows[0].Window != "7d" ||
		len(result[1].Windows) != 1 || result[1].Windows[0].Window != "7d" {
		t.Fatalf("5h row must be omitted when Plus has no 5h window: %+v", result)
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
