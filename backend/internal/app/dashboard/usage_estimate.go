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

type usageEstimatePool struct {
	plan             string
	requiredPlans    []string
	requiredAnyPlans []string
	present          bool
	fiveHour         usageEstimateWindowPool
	sevenDay         usageEstimateWindowPool
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

// BuildUsageEstimates 聚合 Plus/Pro 套餐池的用量增长、100% 成本和剩余时间。
// 分组路由类别与估算路径彼此独立：只有 Plus 进入 Plus 路径；Team/K12、
// ProLite 和 Pro 均进入 Pro 路径。Pro 汇总继续叠加 Plus，表示整个付费账号池。
func BuildUsageEstimates(sources []UsageEstimateSource, now time.Time, accountCostPerMinute float64) []UsageEstimate {
	plus := usageEstimatePool{plan: plantype.Plus, requiredPlans: []string{plantype.Plus}}
	pro := usageEstimatePool{plan: plantype.Pro}
	hasPlusAccounts := false
	proPlans := make(map[string]struct{})
	planHasFiveHour := make(map[string]bool)
	for _, source := range sources {
		plan := plantype.Normalize(source.Plan)
		if plantype.EstimatePool(plan) == "" {
			continue
		}
		planHasFiveHour[plan] = planHasFiveHour[plan] || observationSupported(usageEstimateObservation{
			window:   source.Meta.FiveHour,
			optional: true,
		})
	}
	plusShortTerm := make([]usageEstimateObservation, 0, len(sources))
	proShortTerm := make([]usageEstimateObservation, 0, len(sources))
	day := now.In(time.Local).Format("2006-01-02")
	for _, source := range sources {
		plan := plantype.Normalize(source.Plan)
		estimatePool := plantype.EstimatePool(plan)
		if estimatePool == "" {
			continue
		}
		if plan == plantype.Plus {
			hasPlusAccounts = true
		}
		fiveHour := usageEstimateObservation{
			window: source.Meta.FiveHour, plan: plan, day: day, optional: true, maxAge: usage5hObservationAge,
		}
		sevenDay := usageEstimateObservation{
			window: source.Meta.SevenDay, plan: plan, day: day, maxAge: usage7dObservationAge,
		}
		shortTerm := sevenDay
		if planHasFiveHour[plan] {
			shortTerm = fiveHour
			// 5h 能力按 plan_type 判定；同套餐的新账号也必须计入，并共享该套餐标准值。
			shortTerm.optional = false
		}
		if estimatePool == plantype.EstimatePoolPlus {
			plus.present = true
			plus.sevenDay.add(sevenDay)
			pro.sevenDay.add(sevenDay)
			plusShortTerm = append(plusShortTerm, shortTerm)
			proShortTerm = append(proShortTerm, shortTerm)
		}
		if estimatePool == plantype.EstimatePoolPro {
			pro.present = true
			pro.sevenDay.add(sevenDay)
			proShortTerm = append(proShortTerm, shortTerm)
			proPlans[plan] = struct{}{}
		}
	}
	// 短期行按 plan_type 选择有效限制：有 5h 则用 5h，否则使用 7d。
	// 是否显示整行只由 Plus 决定；Plus 没有 5h 时，短期余量与 7d 相同，省略重复行。
	if planHasFiveHour[plantype.Plus] {
		for _, observation := range plusShortTerm {
			plus.fiveHour.add(observation)
		}
		for _, observation := range proShortTerm {
			pro.fiveHour.add(observation)
		}
	}
	if hasPlusAccounts {
		pro.requiredPlans = append(pro.requiredPlans, plantype.Plus)
	}
	for _, plan := range []string{plantype.Pro, plantype.Team, plantype.K12, plantype.ProLite} {
		if _, present := proPlans[plan]; present {
			pro.requiredAnyPlans = append(pro.requiredAnyPlans, plan)
		}
	}

	result := make([]UsageEstimate, 0, 2)
	for _, pool := range []*usageEstimatePool{&plus, &pro} {
		if !pool.present {
			continue
		}
		item := UsageEstimate{Plan: pool.plan, Windows: make([]UsageEstimateWindow, 0, 2)}
		if pool.fiveHour.supported {
			item.Windows = append(item.Windows, pool.fiveHour.estimate("5h", pool.requiredPlans, pool.requiredAnyPlans, accountCostPerMinute, now, usage5hObservationAge))
		}
		if pool.sevenDay.supported {
			item.Windows = append(item.Windows, pool.sevenDay.estimate("7d", pool.requiredPlans, pool.requiredAnyPlans, accountCostPerMinute, now, usage7dObservationAge))
		}
		if len(item.Windows) > 0 {
			result = append(result, item)
		}
	}
	return result
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
	result := UsageEstimateWindow{Window: window, Status: "insufficient"}
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
