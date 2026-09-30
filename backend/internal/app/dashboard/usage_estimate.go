package dashboard

import (
	"math"
	"time"

	"github.com/DevilGenius/airgate-core/internal/accountusage"
	"github.com/DevilGenius/airgate-core/internal/plantype"
)

const (
	usageCalibrationMaxAge = 7 * 24 * time.Hour
	usage5hObservationAge  = 6 * time.Hour
	usage7dObservationAge  = 24 * time.Hour
)

// UsageEstimateSource 是仪表盘估算所需的最小账号快照。
type UsageEstimateSource struct {
	Plan string
	Meta accountusage.EstimateMeta
}

type usageEstimateWindowPool struct {
	supported    bool
	observations []usageEstimateObservation
}

type usageEstimateObservation struct {
	window   accountusage.WindowEstimate
	plan     string
	day      string
	optional bool
	maxAge   time.Duration
}

// BuildUsageEstimates 聚合非 Free 账号：逐账号选择实际存在的 5h，否则使用 7d。
// 两种窗口独立校准后相加，避免同套餐的 5h/7d 标准值混用。
func BuildUsageEstimates(sources []UsageEstimateSource, now time.Time, accountCostPerMinute float64) []UsageEstimate {
	var fiveHour, sevenDay usageEstimateWindowPool
	present := false
	observedWindows := false
	day := now.In(time.Local).Format("2006-01-02")
	for _, source := range sources {
		plan := plantype.Normalize(source.Plan)
		if plan == "" || plan == plantype.Free {
			continue
		}
		present = true
		observedWindows = observedWindows || source.Meta.WindowsObservedAt != nil ||
			source.Meta.FiveHour.ObservedAt != nil || source.Meta.SevenDay.ObservedAt != nil
		if source.Meta.HasFiveHourWindow() {
			fiveHour.add(usageEstimateObservation{window: source.Meta.FiveHour, plan: plan, day: day})
		} else {
			sevenDay.add(usageEstimateObservation{window: source.Meta.SevenDay, plan: plan, day: day})
		}
	}
	if !present {
		return []UsageEstimate{}
	}
	five := fiveHour.estimate("5h", nil, nil, accountCostPerMinute, now, usage5hObservationAge)
	seven := sevenDay.estimate("7d", nil, nil, accountCostPerMinute, now, usage7dObservationAge)
	total := UsageEstimateWindow{Window: "total", Status: "insufficient", AccountCount: len(fiveHour.observations) + len(sevenDay.observations)}
	for _, part := range []UsageEstimateWindow{five, seven} {
		if part.Status != "ready" || part.RemainingCost == nil {
			continue
		}
		if total.RemainingCost == nil {
			total.RemainingCost = new(float64)
		}
		total.Status = "ready"
		*total.RemainingCost += *part.RemainingCost
	}
	if total.RemainingCost != nil {
		if *total.RemainingCost == 0 {
			total.RemainingMinutes = new(float64)
		} else if validPositive(accountCostPerMinute) {
			minutes := *total.RemainingCost / accountCostPerMinute
			total.RemainingMinutes = &minutes
		}
	}
	if !fiveHour.supported && observedWindows {
		// 已知不存在 5h 账号与存在但数据不足须区分。
		five.Status = "ready"
		five.RemainingCost = new(float64)
		five.RemainingMinutes = new(float64)
	}
	return []UsageEstimate{{Plan: "non_free", Windows: []UsageEstimateWindow{total, five}}}
}

func (p *usageEstimateWindowPool) add(observation usageEstimateObservation) {
	if !observationSupported(observation) {
		return
	}
	p.supported = true
	p.observations = append(p.observations, observation)
}

func observationSupported(observation usageEstimateObservation) bool {
	w := observation.window
	return !observation.optional || w.GrowthDate != "" || w.ObservedAt != nil || w.CostPerPercent > 0
}

func (p usageEstimateWindowPool) estimate(window string, requiredPlans, requiredAnyPlans []string, accountCostPerMinute float64, now time.Time, observationMaxAge time.Duration) UsageEstimateWindow {
	result := UsageEstimateWindow{Window: window, Status: "insufficient", AccountCount: len(p.observations)}
	planRates := sharedPlanRates(p.observations, now)
	for _, plan := range requiredPlans {
		if _, available := planRates[plan]; !available {
			return result
		}
	}
	if len(requiredAnyPlans) > 0 {
		available := false
		for _, plan := range requiredAnyPlans {
			if _, available = planRates[plan]; available {
				break
			}
		}
		if !available {
			return result
		}
	}
	weightedConsumed := 0.0
	rateSum := 0.0
	remainingCost := 0.0
	remainingAvailable := false
	for _, observation := range p.observations {
		w := observation.window
		rate, calibrated := planRates[observation.plan]
		if !calibrated {
			// 未校准套餐不借用其它 plan_type 的标准值，也不阻断已知套餐的保守估算。
			continue
		}
		rateSum += rate
		if w.GrowthDate == observation.day && validPositive(w.DailyGrowth) {
			weightedConsumed += rate * w.DailyGrowth
		}
		maxAge := observation.maxAge
		if maxAge <= 0 {
			maxAge = observationMaxAge
		}
		if observationFresh(w.ObservedAt, now, maxAge) && validPercent(w.LastPercent) {
			remainingCost += rate * math.Max(0, 100-w.LastPercent)
			remainingAvailable = true
		}
	}
	if rateSum <= 0 {
		return result
	}
	result.Status = "ready"
	// DailyGrowthPercent 是正数的当日累计用量增长，即已消耗百分比的增量。
	result.DailyGrowthPercent = weightedConsumed / rateSum
	result.FullCost = rateSum * 100
	if remainingAvailable {
		result.RemainingCost = &remainingCost
		if remainingCost <= 0 {
			minutes := 0.0
			result.RemainingMinutes = &minutes
		} else if accountCostPerMinute > 0 {
			minutes := remainingCost / accountCostPerMinute
			result.RemainingMinutes = &minutes
		}
	}
	return result
}

// sharedPlanRates 为每个精确 plan_type 生成 5h/7d 独立的标准消耗值。
// 同套餐账号共享标准值；不同套餐之间绝不借用校准样本。
func sharedPlanRates(observations []usageEstimateObservation, now time.Time) map[string]float64 {
	type aggregate struct {
		weightedCost float64
		weight       float64
	}
	aggregates := make(map[string]aggregate, len(observations))
	for _, observation := range observations {
		window := observation.window
		if !usageCalibrationValid(window, now) {
			continue
		}
		weight := window.CalibrationWeight
		if window.CalibratedAt != nil && now.After(*window.CalibratedAt) {
			weight = accountusage.DecayWeight(weight, now.Sub(*window.CalibratedAt))
		}
		if !validPositive(weight) {
			continue
		}
		aggregate := aggregates[observation.plan]
		aggregate.weightedCost += window.CostPerPercent * weight
		aggregate.weight += weight
		aggregates[observation.plan] = aggregate
	}

	rates := make(map[string]float64, len(aggregates))
	for plan, aggregate := range aggregates {
		if !validPositive(aggregate.weight) {
			continue
		}
		rate := aggregate.weightedCost / aggregate.weight
		if !validPositive(rate) {
			continue
		}
		rates[plan] = rate
	}
	return rates
}

func usageCalibrationValid(window accountusage.WindowEstimate, now time.Time) bool {
	if !validPositive(window.CostPerPercent) || !validPositive(window.CalibrationWeight) || window.CalibratedAt == nil {
		return false
	}
	age := now.Sub(*window.CalibratedAt)
	return age >= -5*time.Minute && age <= usageCalibrationMaxAge
}

func observationFresh(observedAt *time.Time, now time.Time, maxAge time.Duration) bool {
	if observedAt == nil {
		return false
	}
	age := now.Sub(*observedAt)
	return age >= -5*time.Minute && age <= maxAge
}

func validPercent(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validPositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
