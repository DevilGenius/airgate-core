package account

import (
	"strings"
	"time"

	"github.com/DevilGenius/airgate-core/internal/plantype"
)

// NormalizePlanType 将插件返回的套餐描述规范化为对外稳定的套餐标识。
//
// 返回值表示套餐身份，不应回写 credentials.plan_type；插件声明的筛选规则可能依赖
// 原始值（例如 "Builder Id Pro"）。路由类别和估算路径由 plantype 的专用函数决定。
func NormalizePlanType(value string) string {
	return plantype.Normalize(value)
}

// EffectivePlanType 返回账号当前参与容量和用量估算的套餐类型。
// subscription_active_until 只对 Plus / Pro 有套餐失效语义；其它套餐始终以
// plan_type 为准，不根据该时间降级为 Free。
func EffectivePlanType(value, subscriptionActiveUntil string, now time.Time) string {
	plan := NormalizePlanType(value)
	if plan != plantype.Plus && plan != plantype.Pro {
		return plan
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(subscriptionActiveUntil))
	if err == nil && !expiresAt.After(now) {
		return plantype.Free
	}
	return plan
}
