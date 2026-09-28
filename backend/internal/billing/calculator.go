// Package billing 提供费用计算和使用量异步记录
package billing

import (
	"github.com/DevilGenius/airgate-core/internal/pkg/ratevalue"
	"math"
)

// Calculator 费用计算器
type Calculator struct{}

// NewCalculator 创建费用计算器
func NewCalculator() *Calculator {
	return &Calculator{}
}

// CalculateInput 计算输入参数
type CalculateInput struct {
	InputCost         float64 // 插件已计算的输入费用
	OutputCost        float64 // 插件已计算的输出费用
	CachedInputCost   float64 // 插件已计算的缓存读取费用
	CacheCreationCost float64 // 插件已计算的缓存写入费用

	// BillingRate 平台真实计费倍率（已由 ResolveBillingRate 解析过的单值，不再相乘）。
	// 必须为正数；非法值兜底为 1。
	// 用于扣 reseller 的 user.balance 和写入 actual_cost。
	BillingRate float64

	// SellRate Reseller 设置的销售倍率（0 表示客户侧免费，1 表示不加价）。
	// 用于计算 billed_cost，累加到 APIKey.used_quota；基数默认取 actual_cost，
	// 设置 APIKeyBaseCostOverride 时使用单独的 Key 计费基数。
	// 平台余额扣费永远不读这个字段。
	SellRate float64

	// AccountRate 账号实际成本倍率（账号自身相对上游的成本系数，比如代购账号 1.2x）。
	// 必须为正数；非法值兜底为 1。
	// 用于计算 account_cost（账号实际消耗），写入 usage_log，仅供"账号计费"统计使用。
	// 与用户计费 (BillingRate) 完全独立，不影响 actual_cost / User.balance。
	AccountRate float64

	// BillingCostOverride 可覆盖 actual_cost 的最终计费结果。
	// 用于分组图片 1K/2K/4K 固定价：配置后整次请求按图片单张价 × 数量计费，
	// billed_cost 仍会在 override 后的 actual_cost 基础上叠加 SellRate。
	BillingCostOverride *float64

	// BillingCostAddon 会叠加到倍率后的 actual_cost。
	// 用于 Responses image_generation：文本 token 仍按 BillingRate 计费，
	// 图片按分组 1K/2K/4K 固定价单独计费后加到同一条账单里。
	BillingCostAddon *float64

	// APIKeyBaseCostOverride replaces only the pre-multiplier base for billed_cost.
	// User balance (actual_cost), upstream account cost and measured costs stay intact.
	APIKeyBaseCostOverride *float64
}

// CalculateResult 计算结果
type CalculateResult struct {
	InputCost             float64 // 输入费用
	OutputCost            float64 // 输出费用
	CachedInputCost       float64 // cached input 费用（cache read）
	CacheCreationCost     float64 // cache creation 费用（cache write）
	TotalCost             float64 // 原始基础成本 = input + cached_input + cache_creation + output（未乘任何倍率）
	ActualCost            float64 // 平台真实成本 = TotalCost × BillingRate（扣 reseller 余额）
	BilledCost            float64 // 客户账面消耗 = Key 计费基数 × SellRate，默认基数为 ActualCost
	AccountCost           float64 // 账号实际成本 = TotalCost × AccountRate（仅服务于"账号计费"统计）
	RateMultiplier        float64 // 快照：本次生效的 BillingRate
	SellRate              float64 // 快照：本次生效的 SellRate
	AccountRateMultiplier float64 // 快照：本次生效的 AccountRate
}

// Calculate 计算费用
//
// 三条独立管道：
//
//	total_cost   = input_cost + cached_input_cost + output_cost
//
//	actual_cost  = total_cost × billing_rate          → 扣 User.balance（平台真实计费）
//	billed_cost  = key_charge_base × sell_rate        → 累加 APIKey.used_quota（end customer 可见）
//	account_cost = total_cost × account_rate          → 写入 usage_log，仅服务"账号计费"统计
//
// key_charge_base 默认等于 actual_cost；Key 专用基数覆盖不改变 actual_cost。
// 三者互不影响，各自存储在独立列里。
func (c *Calculator) Calculate(input CalculateInput) CalculateResult {
	totalCost := ratevalue.SafeAddNonNegative(
		input.InputCost,
		input.OutputCost,
		input.CachedInputCost,
		input.CacheCreationCost,
	)

	billingRate := ratevalue.NormalizeMultiplier(input.BillingRate, 1.0)
	accountRate := ratevalue.NormalizeMultiplier(input.AccountRate, 1.0)
	sellRate := ratevalue.NormalizeSellMultiplier(input.SellRate, 1.0)

	actualCost := calculateChargeBase(totalCost, billingRate, input.BillingCostOverride, input.BillingCostAddon)

	keyChargeBase := actualCost
	if override := input.APIKeyBaseCostOverride; override != nil && *override >= 0 && !math.IsNaN(*override) && !math.IsInf(*override, 0) {
		keyChargeBase = calculateChargeBase(*override, billingRate, input.BillingCostOverride, input.BillingCostAddon)
	}
	billedCost := ratevalue.SafeMulNonNegative(keyChargeBase, sellRate)

	accountCost := ratevalue.SafeMulNonNegative(totalCost, accountRate)

	return CalculateResult{
		InputCost:             input.InputCost,
		OutputCost:            input.OutputCost,
		CachedInputCost:       input.CachedInputCost,
		CacheCreationCost:     input.CacheCreationCost,
		TotalCost:             totalCost,
		ActualCost:            actualCost,
		BilledCost:            billedCost,
		AccountCost:           accountCost,
		RateMultiplier:        billingRate,
		SellRate:              sellRate,
		AccountRateMultiplier: accountRate,
	}
}

func calculateChargeBase(total, rate float64, override, addon *float64) float64 {
	cost := ratevalue.SafeMulNonNegative(total, rate)
	if override != nil {
		cost = ratevalue.NormalizeNonNegative(*override)
	}
	if addon != nil {
		cost = ratevalue.SafeAddNonNegative(cost, *addon)
	}
	return cost
}
