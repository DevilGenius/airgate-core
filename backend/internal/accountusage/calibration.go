package accountusage

import (
	"math"
	"time"
)

const (
	CalibrationHalfLife           = 24 * time.Hour
	FiveHourMaxSampleGap          = 6 * time.Hour
	SevenDayMaxSampleGap          = 24 * time.Hour
	usageRollbackTolerancePercent = 2.0
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
		(w.ObservedAt != nil && !observation.ObservedAt.After(*w.ObservedAt)) {
		return CalibrationInterval{}, false
	}
	if w.PendingDecreasePercent != nil && observation.CurrentPercent >= w.LastPercent {
		// 待确认下降恢复到旧基线或更高时，无法区分瞬时异常与快速 reset，丢弃歧义区间。
		return CalibrationInterval{}, false
	}
	start := w.CalibrationCursorAt
	previousPercent := w.LastPercent
	if w.PendingDecreasePercent != nil && observation.CurrentPercent < w.LastPercent {
		// 第二个低值确认回退时，只校准首次低值之后的增长；异常恢复则继续使用正式 cursor。
		start = w.ObservedAt
		previousPercent = *w.PendingDecreasePercent
	}
	if start == nil {
		return CalibrationInterval{}, false
	}
	gap := observation.ObservedAt.Sub(*start)
	if gap <= 0 || gap > observation.MaxSampleGap {
		return CalibrationInterval{}, false
	}
	delta := observation.CurrentPercent - previousPercent
	if delta <= 0 {
		return CalibrationInterval{}, false
	}
	return CalibrationInterval{
		Start:        *start,
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

	date, consumed, last, pendingDecreasePercent, moveCursor, changed := nextDailyConsumption(
		w.GrowthDate,
		w.DailyGrowth,
		w.LastPercent,
		w.PendingDecreasePercent,
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
	w.PendingDecreasePercent = pendingDecreasePercent
	if moveCursor {
		w.CalibrationCursorAt = cloneTime(&observation.ObservedAt)
	}
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

func nextDailyConsumption(
	storedDate string,
	consumed float64,
	last float64,
	pendingDecreasePercent *float64,
	current float64,
	day string,
) (date string, nextConsumed float64, nextLast float64, nextPendingDecreasePercent *float64, moveCursor bool, changed bool) {
	if day == "" || !validPercent(current) {
		return storedDate, consumed, last, pendingDecreasePercent, false, false
	}
	if storedDate != "" && day < storedDate {
		return storedDate, consumed, last, pendingDecreasePercent, false, false
	}
	if storedDate == "" {
		return day, 0, current, nil, true, true
	}

	date = storedDate
	nextConsumed = consumed
	nextLast = last
	nextPendingDecreasePercent = pendingDecreasePercent
	newDay := storedDate != day
	if newDay {
		date = day
		nextConsumed = 0
		changed = true
	}

	if current >= last {
		nextPendingDecreasePercent = nil
		if current > last {
			if !newDay {
				nextConsumed += current - last
			}
			nextLast = current
			return date, nextConsumed, nextLast, nextPendingDecreasePercent, true, true
		}
		if pendingDecreasePercent != nil {
			changed = true
		}
		return date, nextConsumed, nextLast, nextPendingDecreasePercent, newDay, changed
	}

	if pendingDecreasePercent == nil {
		return date, nextConsumed, nextLast, cloneFloat64(&current), false, true
	}

	// 连续第二次低于已确认基线才确认下降。超过 2% 视为窗口从 0 重置；
	// 2% 以内视为读数回拨，只累计待确认期间能够直接观测到的正增长。
	lowestObserved := math.Min(*pendingDecreasePercent, current)
	if last-lowestObserved > usageRollbackTolerancePercent {
		nextConsumed += current
	} else if current > *pendingDecreasePercent {
		nextConsumed += current - *pendingDecreasePercent
	}
	return date, nextConsumed, current, nil, true, true
}

func validPercent(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validPositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
