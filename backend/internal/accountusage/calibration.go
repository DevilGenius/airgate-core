package accountusage

import (
	"math"
	"time"
)

const (
	CalibrationHalfLife  = 24 * time.Hour
	FiveHourMaxSampleGap = 6 * time.Hour
	SevenDayMaxSampleGap = 24 * time.Hour
)

// WindowObservation 是一次基础用量窗口观测。
type WindowObservation struct {
	Day            string
	ObservedAt     time.Time
	CurrentPercent float64
	MaxSampleGap   time.Duration
}

// CalibrationInterval 表示可以与百分比增量配对的账号成本区间。
type CalibrationInterval struct {
	Start        time.Time
	End          time.Time
	PercentDelta float64
}

// CostInterval 判断本次观测是否可以形成滚动成本校准样本。
func (w WindowEstimate) CostInterval(observation WindowObservation) (CalibrationInterval, bool) {
	if !validPercent(observation.CurrentPercent) || observation.ObservedAt.IsZero() ||
		(w.ObservedAt != nil && !observation.ObservedAt.After(*w.ObservedAt)) ||
		w.CalibrationCursorAt == nil {
		return CalibrationInterval{}, false
	}
	gap := observation.ObservedAt.Sub(*w.CalibrationCursorAt)
	if gap <= 0 || gap > observation.MaxSampleGap {
		return CalibrationInterval{}, false
	}
	delta := percentDelta(w.LastPercent, observation.CurrentPercent)
	if delta <= 0 {
		return CalibrationInterval{}, false
	}
	return CalibrationInterval{
		Start:        *w.CalibrationCursorAt,
		End:          observation.ObservedAt,
		PercentDelta: delta,
	}, true
}

// ApplyObservation 更新当日累计用量增长、观测游标和滚动成本校准。
// costDelta 仅在 CostInterval 返回有效区间时传入；没有有效成本样本时传 0。
func (w *WindowEstimate) ApplyObservation(observation WindowObservation, costDelta float64) bool {
	if w == nil || !validPercent(observation.CurrentPercent) || observation.ObservedAt.IsZero() ||
		(w.ObservedAt != nil && !observation.ObservedAt.After(*w.ObservedAt)) {
		return false
	}
	if w.GrowthDate != "" && observation.Day != "" && observation.Day < w.GrowthDate {
		return false
	}
	interval, canCalibrate := w.CostInterval(observation)
	w.ObservedAt = cloneTime(&observation.ObservedAt)

	date, consumed, last, changed := nextDailyConsumption(
		w.GrowthDate,
		w.DailyGrowth,
		w.LastPercent,
		observation.CurrentPercent,
		observation.Day,
	)
	if !changed {
		if w.CalibrationCursorAt == nil {
			w.CalibrationCursorAt = cloneTime(&observation.ObservedAt)
		}
		return true
	}

	w.GrowthDate = date
	w.DailyGrowth = consumed
	w.LastPercent = last
	w.CalibrationCursorAt = cloneTime(&observation.ObservedAt)
	if canCalibrate && validPositive(costDelta) {
		sample := costDelta / interval.PercentDelta
		if validPositive(sample) {
			w.applyCalibrationSample(sample, interval.PercentDelta, observation.ObservedAt)
		}
	}
	return true
}

func (w *WindowEstimate) applyCalibrationSample(sample, percentWeight float64, observedAt time.Time) {
	decayedWeight := w.CalibrationWeight
	if w.CalibratedAt != nil && observedAt.After(*w.CalibratedAt) {
		decayedWeight = DecayWeight(decayedWeight, observedAt.Sub(*w.CalibratedAt))
	}
	if !validPositive(w.CostPerPercent) || !validPositive(decayedWeight) {
		decayedWeight = 0
		w.CostPerPercent = 0
	}
	sampleWeight := math.Min(percentWeight, 100)
	totalWeight := decayedWeight + sampleWeight
	w.CostPerPercent = (w.CostPerPercent*decayedWeight + sample*sampleWeight) / totalWeight
	w.CalibrationWeight = totalWeight
	w.CalibratedAt = cloneTime(&observedAt)
}

// DecayWeight 按 24 小时半衰期衰减历史证据权重。
func DecayWeight(weight float64, elapsed time.Duration) float64 {
	if weight <= 0 || elapsed <= 0 {
		return weight
	}
	return weight * math.Exp(-math.Ln2*float64(elapsed)/float64(CalibrationHalfLife))
}

func nextDailyConsumption(storedDate string, consumed, last, current float64, day string) (string, float64, float64, bool) {
	if day == "" || !validPercent(current) {
		return storedDate, consumed, last, false
	}
	if storedDate != "" && day < storedDate {
		return storedDate, consumed, last, false
	}
	if storedDate != day {
		return day, 0, current, true
	}
	if current == last {
		return storedDate, consumed, last, false
	}
	delta := current - last
	if delta < 0 {
		delta = current
	}
	return storedDate, consumed + delta, current, true
}

func percentDelta(previous, current float64) float64 {
	if current >= previous {
		return current - previous
	}
	return current
}

func validPercent(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validPositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
