package account

import (
	"strings"
)

// NormalizePlanType 将插件返回的套餐描述规范化为对外稳定的套餐标识。
//
// 该函数只用于对外展示，不应回写 credentials.plan_type；插件声明的筛选规则
// 可能依赖原始套餐值（例如 "Builder Id Pro"）。k12、prolite 与 team 保持为不同标识；
// Self Serve Business ProLite 对外统一返回 prolite。
func NormalizePlanType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	tokens := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, token := range tokens {
		switch token {
		case "free", "plus", "pro", "team", "k12", "prolite", "enterprise":
			return token
		case "professional":
			return "pro"
		}
	}

	return strings.ToLower(value)
}
