package accountusage

import "time"

const EstimateMetaVersion = 1

// EstimateMeta 保存账号级 5h/7d 用量增长与滚动成本校准状态。
type EstimateMeta struct {
	Version  int            `json:"version,omitempty"`
	FiveHour WindowEstimate `json:"5h,omitempty"`
	SevenDay WindowEstimate `json:"7d,omitempty"`
}

// WindowEstimate 保存单个用量窗口的展示状态、校准值和观测游标。
type WindowEstimate struct {
	// DailyGrowth 保存正数的当日累计用量增长，即已消耗百分比的增量。
	GrowthDate  string  `json:"growth_date,omitempty"`
	DailyGrowth float64 `json:"daily_growth,omitempty"`
	LastPercent float64 `json:"last_percent,omitempty"`
	// PendingDecreasePercent 保存首次低于确认基线的观测；再次降低才确认回退。
	PendingDecreasePercent *float64   `json:"pending_decrease_percent,omitempty"`
	CostPerPercent         float64    `json:"cost_per_percent,omitempty"`
	CalibrationWeight      float64    `json:"calibration_weight,omitempty"`
	CalibratedAt           *time.Time `json:"calibrated_at,omitempty"`
	CalibrationCursorAt    *time.Time `json:"calibration_cursor_at,omitempty"`
	ObservedAt             *time.Time `json:"observed_at,omitempty"`
}

// Clone 深拷贝内部指针，避免领域对象与 Ent 实体共享可变引用。
func Clone(value EstimateMeta) EstimateMeta {
	value.FiveHour = cloneWindow(value.FiveHour)
	value.SevenDay = cloneWindow(value.SevenDay)
	return value
}

// Equal 判断两份估算状态是否表示同一个持久化快照。
// 时间使用 Time.Equal 比较，避免相同时刻因 Location 表示不同而触发无意义重试。
func Equal(left, right EstimateMeta) bool {
	return left.Version == right.Version &&
		windowEqual(left.FiveHour, right.FiveHour) &&
		windowEqual(left.SevenDay, right.SevenDay)
}

func windowEqual(left, right WindowEstimate) bool {
	return left.GrowthDate == right.GrowthDate &&
		left.DailyGrowth == right.DailyGrowth &&
		left.LastPercent == right.LastPercent &&
		float64Equal(left.PendingDecreasePercent, right.PendingDecreasePercent) &&
		left.CostPerPercent == right.CostPerPercent &&
		left.CalibrationWeight == right.CalibrationWeight &&
		timeEqual(left.CalibratedAt, right.CalibratedAt) &&
		timeEqual(left.CalibrationCursorAt, right.CalibrationCursorAt) &&
		timeEqual(left.ObservedAt, right.ObservedAt)
}

func timeEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func float64Equal(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneWindow(value WindowEstimate) WindowEstimate {
	value.PendingDecreasePercent = cloneFloat64(value.PendingDecreasePercent)
	value.CalibratedAt = cloneTime(value.CalibratedAt)
	value.CalibrationCursorAt = cloneTime(value.CalibrationCursorAt)
	value.ObservedAt = cloneTime(value.ObservedAt)
	return value
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
