package dashboard

import (
	"math"
	"sort"
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
	plan     string
	present  bool
	fiveHour usageEstimateWindowPool
	sevenDay usageEstimateWindowPool
}

type usageEstimateWindowPool struct {
	supported    bool
	observations []usageEstimateObservation
}

type usageEstimateObservation struct {
	window   accountusage.WindowEstimate
	day      string
	optional bool
}

// BuildUsageEstimates 聚合 Plus/Pro 套餐池的增长、100% 成本和剩余时间。
func BuildUsageEstimates(sources []UsageEstimateSource, now time.Time, accountCostPerMinute float64) []UsageEstimate {
	plus := usageEstimatePool{plan: "plus"}
	pro := usageEstimatePool{plan: "pro"}
	day := now.In(time.Local).Format("2006-01-02")
	for _, source := range sources {
		isPlusPool := source.Plan == "plus" || source.Plan == "team" || source.Plan == "k12"
		isProPlan := source.Plan == "pro"
		if !isPlusPool && !isProPlan {
			continue
		}
		fiveHour := usageEstimateObservation{window: source.Meta.FiveHour, day: day, optional: true}
		sevenDay := usageEstimateObservation{window: source.Meta.SevenDay, day: day}
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

	result := make([]UsageEstimate, 0, 2)
	for _, pool := range []*usageEstimatePool{&plus, &pro} {
		if !pool.present {
			continue
		}
		item := UsageEstimate{Plan: pool.plan, Windows: make([]UsageEstimateWindow, 0, 2)}
		if pool.fiveHour.supported {
			item.Windows = append(item.Windows, pool.fiveHour.estimate("5h", accountCostPerMinute, now, usage5hObservationAge))
		}
		if pool.sevenDay.supported {
			item.Windows = append(item.Windows, pool.sevenDay.estimate("7d", accountCostPerMinute, now, usage7dObservationAge))
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

func (p usageEstimateWindowPool) estimate(window string, accountCostPerMinute float64, now time.Time, observationMaxAge time.Duration) UsageEstimateWindow {
	result := UsageEstimateWindow{Window: window, Status: "insufficient"}
	validRates := make([]float64, 0, len(p.observations))
	for _, observation := range p.observations {
		if usageCalibrationValid(observation.window, now) {
			validRates = append(validRates, observation.window.CostPerPercent)
		}
	}
	if len(validRates) == 0 {
		return result
	}
	fallbackRate := median(validRates)
	weightedGrowth := 0.0
	rateSum := 0.0
	remainingCost := 0.0
	remainingAvailable := true
	for _, observation := range p.observations {
		w := observation.window
		rate := w.CostPerPercent
		if !usageCalibrationValid(w, now) {
			rate = fallbackRate
		}
		rateSum += rate
		if w.GrowthDate == observation.day && validPositive(w.DailyGrowth) {
			weightedGrowth += rate * w.DailyGrowth
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
	result.DailyGrowthPercent = weightedGrowth / rateSum
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

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func validPercent(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validPositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
