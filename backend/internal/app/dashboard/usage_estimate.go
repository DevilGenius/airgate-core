package dashboard

import (
	"math"
	"time"

	"github.com/DevilGenius/airgate-core/internal/accountusage"
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

type usageEstimatePool struct {
	plan          string
	requiredPlans []string
	present       bool
	fiveHour      usageEstimateWindowPool
	sevenDay      usageEstimateWindowPool
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
}

// BuildUsageEstimates 聚合 Plus/Pro 套餐池的增长、100% 成本和剩余时间。
func BuildUsageEstimates(sources []UsageEstimateSource, now time.Time, accountCostPerMinute float64) []UsageEstimate {
	plus := usageEstimatePool{plan: "plus", requiredPlans: []string{"plus"}}
	pro := usageEstimatePool{plan: "pro", requiredPlans: []string{"pro"}}
	hasPlusAccounts := false
	day := now.In(time.Local).Format("2006-01-02")
	for _, source := range sources {
		isPlusPool := source.Plan == "plus" || source.Plan == "team" || source.Plan == "k12"
		isProPlan := source.Plan == "pro"
		if !isPlusPool && !isProPlan {
			continue
		}
		if source.Plan == "plus" {
			hasPlusAccounts = true
		}
		fiveHour := usageEstimateObservation{window: source.Meta.FiveHour, plan: source.Plan, day: day, optional: true}
		sevenDay := usageEstimateObservation{window: source.Meta.SevenDay, plan: source.Plan, day: day}
		if isPlusPool {
			plus.present = true
			plus.add(fiveHour, sevenDay)
			pro.add(fiveHour, sevenDay)
		}
		if isProPlan {
			pro.present = true
			pro.add(fiveHour, sevenDay)
		}
	}
	if hasPlusAccounts {
		pro.requiredPlans = append(pro.requiredPlans, "plus")
	}

	result := make([]UsageEstimate, 0, 2)
	for _, pool := range []*usageEstimatePool{&plus, &pro} {
		if !pool.present {
			continue
		}
		item := UsageEstimate{Plan: pool.plan, Windows: make([]UsageEstimateWindow, 0, 2)}
		if pool.fiveHour.supported {
			item.Windows = append(item.Windows, pool.fiveHour.estimate("5h", pool.requiredPlans, accountCostPerMinute, now, usage5hObservationAge))
		}
		if pool.sevenDay.supported {
			item.Windows = append(item.Windows, pool.sevenDay.estimate("7d", pool.requiredPlans, accountCostPerMinute, now, usage7dObservationAge))
		}
		if len(item.Windows) > 0 {
			result = append(result, item)
		}
	}
	return result
}

func (p *usageEstimatePool) add(fiveHour, sevenDay usageEstimateObservation) {
	p.fiveHour.add(fiveHour)
	p.sevenDay.add(sevenDay)
}

func (p *usageEstimateWindowPool) add(observation usageEstimateObservation) {
	w := observation.window
	if observation.optional && w.GrowthDate == "" && w.ObservedAt == nil && w.CostPerPercent <= 0 {
		return
	}
	p.supported = true
	p.observations = append(p.observations, observation)
}

func (p usageEstimateWindowPool) estimate(window string, requiredPlans []string, accountCostPerMinute float64, now time.Time, observationMaxAge time.Duration) UsageEstimateWindow {
	result := UsageEstimateWindow{Window: window, Status: "insufficient"}
	planRates := sharedPlanRates(p.observations, now)
	for _, plan := range requiredPlans {
		if _, available := planRates[plan]; !available {
			return result
		}
	}
	weightedConsumed := 0.0
	rateSum := 0.0
	remainingCost := 0.0
	remainingAvailable := true
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
		if observationFresh(w.ObservedAt, now, observationMaxAge) && validPercent(w.LastPercent) {
			remainingCost += rate * math.Max(0, 100-w.LastPercent)
		} else {
			remainingAvailable = false
		}
	}
	if rateSum <= 0 {
		return result
	}
	result.Status = "ready"
	// DailyGrowthPercent 是兼容字段名，实际语义为正数的“当日累计已消耗百分比”。
	result.DailyGrowthPercent = weightedConsumed / rateSum
	result.FullCost = rateSum * 100
	if remainingAvailable {
		result.RemainingCost = &remainingCost
		if accountCostPerMinute > 0 {
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
