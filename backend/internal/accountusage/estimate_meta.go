package accountusage

import "time"

const EstimateMetaVersion = 1

// EstimateMeta 保存账号级 5h/7d 已消耗百分比与滚动成本校准状态。
type EstimateMeta struct {
	Version  int            `json:"version,omitempty"`
	FiveHour WindowEstimate `json:"5h,omitempty"`
	SevenDay WindowEstimate `json:"7d,omitempty"`
}

// WindowEstimate 保存单个用量窗口的展示状态、校准值和观测游标。
type WindowEstimate struct {
	// GrowthDate / DailyGrowth 是历史 JSON 兼容命名；实际保存正数的当日累计已消耗百分比。
	GrowthDate          string     `json:"growth_date,omitempty"`
	DailyGrowth         float64    `json:"daily_growth,omitempty"`
	LastPercent         float64    `json:"last_percent,omitempty"`
	CostPerPercent      float64    `json:"cost_per_percent,omitempty"`
	CalibrationWeight   float64    `json:"calibration_weight,omitempty"`
	CalibratedAt        *time.Time `json:"calibrated_at,omitempty"`
	CalibrationCursorAt *time.Time `json:"calibration_cursor_at,omitempty"`
	ObservedAt          *time.Time `json:"observed_at,omitempty"`
}

// Clone 深拷贝时间指针，避免领域对象与 Ent 实体共享可变引用。
func Clone(value EstimateMeta) EstimateMeta {
	value.FiveHour = cloneWindow(value.FiveHour)
	value.SevenDay = cloneWindow(value.SevenDay)
	return value
}

func cloneWindow(value WindowEstimate) WindowEstimate {
	value.CalibratedAt = cloneTime(value.CalibratedAt)
	value.CalibrationCursorAt = cloneTime(value.CalibrationCursorAt)
	value.ObservedAt = cloneTime(value.ObservedAt)
	return value
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
