package accountusage

import (
	"math"
	"testing"
	"time"
)

func TestCalibrationUsesTwentyFourHourHalfLife(t *testing.T) {
	if got := DecayWeight(100, 24*time.Hour); math.Abs(got-50) > 1e-9 {
		t.Fatalf("24h decayed weight = %v, want 50", got)
	}
}

func TestWindowEstimateTracksFreshObservationWithoutMovingCalibrationCursor(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	window := WindowEstimate{}
	first := WindowObservation{Day: "2026-08-24", ObservedAt: base, CurrentPercent: 10, MaxSampleGap: FiveHourMaxSampleGap}
	if !window.ApplyObservation(first, 0) {
		t.Fatal("first observation should initialize the window")
	}
	second := WindowObservation{Day: "2026-08-24", ObservedAt: base.Add(time.Hour), CurrentPercent: 10, MaxSampleGap: FiveHourMaxSampleGap}
	if !window.ApplyObservation(second, 0) {
		t.Fatal("same-percent observation should refresh freshness")
	}
	if window.ObservedAt == nil || !window.ObservedAt.Equal(second.ObservedAt) ||
		window.CalibrationCursorAt == nil || !window.CalibrationCursorAt.Equal(first.ObservedAt) {
		t.Fatalf("window timestamps = %+v", window)
	}
}

func TestWindowEstimateIgnoresLatePreviousDayObservation(t *testing.T) {
	base := time.Date(2026, 8, 25, 0, 5, 0, 0, time.UTC)
	window := WindowEstimate{
		GrowthDate:  "2026-08-25",
		DailyGrowth: 12,
		LastPercent: 42,
		ObservedAt:  &base,
	}
	late := WindowObservation{
		Day:            "2026-08-24",
		ObservedAt:     base.Add(time.Minute),
		CurrentPercent: 99,
		MaxSampleGap:   FiveHourMaxSampleGap,
	}
	if window.ApplyObservation(late, 100) {
		t.Fatal("late previous-day observation should be ignored")
	}
	if window.GrowthDate != "2026-08-25" || window.DailyGrowth != 12 || window.LastPercent != 42 {
		t.Fatalf("window regressed after late observation: %+v", window)
	}
}
