package billing

import (
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/pkg/ratevalue"
)

// ResolveBillingRate 决定一次请求该用什么倍率扣 reseller 的真实成本（actual_cost）。
//
// 优先级链（高于者赢）：
//  1. user.group_rates[group_id]   — 用户级专属调价（VIP/折扣）
//  2. group.rate_multiplier        — 分组档位
//  3. 1.0                          — 默认（无 keyInfo 或倍率非法时兜底）
//
// 平台真实扣费倍率必须在 0.01 到 100 之间；0 或非法值会按兜底倍率处理。
//
// 注意：
//   - APIKey.sell_rate 不在这条链里。它是 reseller 对最终客户的"账面"售价倍率，
//     不影响平台真实计费，由 Calculator 在 actual_cost 基础上单独处理 BilledCost。
//   - Account.rate_multiplier 不在这条链里。它只服务于 scheduler 内部 window cost
//     追踪，从用户计费链路完全剥离，调用方需自行计算 windowCost = base × accountRate。
func ResolveBillingRate(keyInfo *auth.APIKeyInfo) float64 {
	if keyInfo == nil {
		return 1.0
	}
	return ResolveBillingRateForGroup(keyInfo.UserGroupRates, keyInfo.GroupID, keyInfo.GroupRateMultiplier)
}

// ResolveBillingRateForGroup 按指定 group 计算实际扣费倍率。
func ResolveBillingRateForGroup(userGroupRates map[int64]float64, groupID int, groupRate float64) float64 {
	if userGroupRates != nil {
		if r, ok := userGroupRates[int64(groupID)]; ok {
			if ratevalue.IsValidMultiplier(r) {
				return r
			}
		}
	}
	return ratevalue.NormalizeMultiplier(groupRate, 1.0)
}
