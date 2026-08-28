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

func TestWindowEstimateRequiresTwoConsecutiveDecreases(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		sequence   []float64
		wantGrowth float64
		wantLast   float64
	}{
		{name: "single one-percent rollback is ignored", sequence: []float64{70, 69, 70}, wantGrowth: 0, wantLast: 70},
		{name: "confirmed one-percent rollback becomes the new baseline", sequence: []float64{70, 69, 69, 70}, wantGrowth: 1, wantLast: 70},
		{name: "confirmed two-percent rollback remains measurement error", sequence: []float64{70, 68, 68, 70}, wantGrowth: 2, wantLast: 70},
		{name: "consecutive lower values need not be equal", sequence: []float64{70, 0, 3}, wantGrowth: 3, wantLast: 3},
		{name: "confirmed reset counts from zero despite nonzero first reading", sequence: []float64{70, 2, 3}, wantGrowth: 3, wantLast: 3},
		{name: "transient zero does not create seventy-percent growth", sequence: []float64{70, 0, 70}, wantGrowth: 0, wantLast: 70},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			window := WindowEstimate{}
			for index, percent := range tt.sequence {
				observation := WindowObservation{
					Day:            "2026-08-24",
					ObservedAt:     base.Add(time.Duration(index) * time.Minute),
					CurrentPercent: percent,
					MaxSampleGap:   FiveHourMaxSampleGap,
				}
				if !window.ApplyObservation(observation, 100) {
					t.Fatalf("ApplyObservation(%v) returned false", percent)
				}
			}
			if window.DailyGrowth != tt.wantGrowth || window.LastPercent != tt.wantLast || window.PendingDecreasePercent != nil {
				t.Fatalf("window = %+v, want growth=%v last=%v pending=false", window, tt.wantGrowth, tt.wantLast)
			}
		})
	}
}

func TestWindowEstimateCountsGrowthSinceFirstLowerObservation(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	window := WindowEstimate{}
	sequence := []float64{70, 0, 1, 2}
	for index, percent := range sequence {
		if !window.ApplyObservation(WindowObservation{
			Day:            "2026-08-24",
			ObservedAt:     base.Add(time.Duration(index) * time.Minute),
			CurrentPercent: percent,
			MaxSampleGap:   FiveHourMaxSampleGap,
		}, 0) {
			t.Fatalf("ApplyObservation(%v) returned false", percent)
		}
	}
	if window.DailyGrowth != 2 || window.LastPercent != 2 || window.PendingDecreasePercent != nil {
		t.Fatalf("confirmed reset window = %+v, want growth=2 last=2 pending=false", window)
	}
}

func TestWindowEstimateTransientDecreaseAcrossDayDoesNotInflateGrowth(t *testing.T) {
	base := time.Date(2026, 8, 24, 23, 59, 0, 0, time.UTC)
	window := WindowEstimate{}
	observations := []WindowObservation{
		{Day: "2026-08-24", ObservedAt: base, CurrentPercent: 70, MaxSampleGap: FiveHourMaxSampleGap},
		{Day: "2026-08-25", ObservedAt: base.Add(time.Minute), CurrentPercent: 0, MaxSampleGap: FiveHourMaxSampleGap},
		{Day: "2026-08-25", ObservedAt: base.Add(2 * time.Minute), CurrentPercent: 70, MaxSampleGap: FiveHourMaxSampleGap},
	}
	for _, observation := range observations {
		if !window.ApplyObservation(observation, 0) {
			t.Fatalf("ApplyObservation(%+v) returned false", observation)
		}
	}
	if window.GrowthDate != "2026-08-25" || window.DailyGrowth != 0 || window.LastPercent != 70 || window.PendingDecreasePercent != nil {
		t.Fatalf("cross-day transient decrease inflated growth: %+v", window)
	}
}

func TestWindowEstimateCountsConfirmedPendingGrowthAcrossDays(t *testing.T) {
	base := time.Date(2026, 8, 24, 23, 59, 0, 0, time.UTC)
	window := WindowEstimate{}
	observations := []WindowObservation{
		{Day: "2026-08-24", ObservedAt: base, CurrentPercent: 70, MaxSampleGap: SevenDayMaxSampleGap},
		{Day: "2026-08-25", ObservedAt: base.Add(24 * time.Hour), CurrentPercent: 0, MaxSampleGap: SevenDayMaxSampleGap},
		{Day: "2026-08-26", ObservedAt: base.Add(48 * time.Hour), CurrentPercent: 40, MaxSampleGap: SevenDayMaxSampleGap},
	}
	for _, observation := range observations {
		if !window.ApplyObservation(observation, 0) {
			t.Fatalf("ApplyObservation(%+v) returned false", observation)
		}
	}
	if window.GrowthDate != "2026-08-26" || window.DailyGrowth != 40 ||
		window.LastPercent != 40 || window.PendingDecreasePercent != nil {
		t.Fatalf("cross-day confirmed decrease = %+v, want date=2026-08-26 growth=40 last=40", window)
	}
}

func TestWindowEstimateCalibratesGrowthAfterConfirmedDecrease(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	window := WindowEstimate{}
	initial := WindowObservation{
		Day: "2026-08-24", ObservedAt: base, CurrentPercent: 70, MaxSampleGap: FiveHourMaxSampleGap,
	}
	if !window.ApplyObservation(initial, 0) {
		t.Fatal("initial observation returned false")
	}
	firstLower := WindowObservation{
		Day: "2026-08-24", ObservedAt: base.Add(time.Minute), CurrentPercent: 2, MaxSampleGap: FiveHourMaxSampleGap,
	}
	if !window.ApplyObservation(firstLower, 0) {
		t.Fatal("first lower observation returned false")
	}
	confirmed := WindowObservation{
		Day: "2026-08-24", ObservedAt: base.Add(2 * time.Minute), CurrentPercent: 3, MaxSampleGap: FiveHourMaxSampleGap,
	}
	interval, ok := window.CostInterval(confirmed)
	if !ok || !interval.Start.Equal(firstLower.ObservedAt) || !interval.End.Equal(confirmed.ObservedAt) || interval.PercentDelta != 1 {
		t.Fatalf("confirmed decrease interval = %+v/%v, want first-lower..confirmed delta=1", interval, ok)
	}
	if !window.ApplyObservation(confirmed, 2) {
		t.Fatal("confirmed lower observation returned false")
	}
	if window.DailyGrowth != 3 || window.LastPercent != 3 || window.PendingDecreasePercent != nil ||
		window.CostPerPercent != 2 || window.CalibrationWeight != 1 ||
		window.CalibratedAt == nil || !window.CalibratedAt.Equal(confirmed.ObservedAt) {
		t.Fatalf("confirmed decrease calibration = %+v", window)
	}
}

func TestWindowEstimateTransientDecreaseDoesNotPolluteCalibration(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	window := WindowEstimate{}
	if !window.ApplyObservation(WindowObservation{
		Day: "2026-08-24", ObservedAt: base, CurrentPercent: 70, MaxSampleGap: FiveHourMaxSampleGap,
	}, 0) {
		t.Fatal("initial observation returned false")
	}
	window.CostPerPercent = 2
	window.CalibrationWeight = 10
	window.CalibratedAt = cloneTime(&base)

	for index, percent := range []float64{0, 70} {
		if !window.ApplyObservation(WindowObservation{
			Day:            "2026-08-24",
			ObservedAt:     base.Add(time.Duration(index+1) * time.Minute),
			CurrentPercent: percent,
			MaxSampleGap:   FiveHourMaxSampleGap,
		}, 100) {
			t.Fatalf("ApplyObservation(%v) returned false", percent)
		}
	}
	if window.CostPerPercent != 2 || window.CalibrationWeight != 10 ||
		window.CalibratedAt == nil || !window.CalibratedAt.Equal(base) {
		t.Fatalf("transient decrease polluted calibration: %+v", window)
	}
}

func TestWindowEstimateRecoveryAboveBaselineDoesNotPolluteCalibration(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	window := WindowEstimate{}
	if !window.ApplyObservation(WindowObservation{
		Day: "2026-08-24", ObservedAt: base, CurrentPercent: 70, MaxSampleGap: FiveHourMaxSampleGap,
	}, 0) {
		t.Fatal("initial observation returned false")
	}
	window.CostPerPercent = 2
	window.CalibrationWeight = 10
	window.CalibratedAt = cloneTime(&base)
	if !window.ApplyObservation(WindowObservation{
		Day: "2026-08-24", ObservedAt: base.Add(time.Minute), CurrentPercent: 0, MaxSampleGap: FiveHourMaxSampleGap,
	}, 0) {
		t.Fatal("lower observation returned false")
	}
	recovered := WindowObservation{
		Day: "2026-08-24", ObservedAt: base.Add(2 * time.Minute), CurrentPercent: 71, MaxSampleGap: FiveHourMaxSampleGap,
	}
	if interval, ok := window.CostInterval(recovered); ok {
		t.Fatalf("ambiguous recovery produced calibration interval: %+v", interval)
	}
	if !window.ApplyObservation(recovered, 140) {
		t.Fatal("recovery observation returned false")
	}
	if window.DailyGrowth != 1 || window.LastPercent != 71 || window.PendingDecreasePercent != nil ||
		window.CostPerPercent != 2 || window.CalibrationWeight != 10 ||
		window.CalibratedAt == nil || !window.CalibratedAt.Equal(base) ||
		window.CalibrationCursorAt == nil || !window.CalibrationCursorAt.Equal(recovered.ObservedAt) {
		t.Fatalf("recovery above baseline polluted calibration: %+v", window)
	}
}
