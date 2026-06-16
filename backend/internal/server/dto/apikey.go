package dto

// APIKeyResp API 密钥响应
type APIKeyResp struct {
	ID                    int64    `json:"id"`
	Name                  string   `json:"name"`
	Key                   string   `json:"key,omitempty"` // 仅创建时返回完整密钥
	KeyPrefix             string   `json:"key_prefix"`    // sk-xxxx...xxxx 脱敏展示
	UserID                int64    `json:"user_id"`
	GroupID               *int64   `json:"group_id"`
	GroupRate             float64  `json:"group_rate"` // 所属分组对该用户生效的实际扣费倍率
	IPWhitelist           []string `json:"ip_whitelist,omitempty"`
	IPBlacklist           []string `json:"ip_blacklist,omitempty"`
	QuotaUSD              float64  `json:"quota_usd"`
	UsedQuota             float64  `json:"used_quota"`              // 账面已用（含 sell_rate markup）
	UsedQuotaActual       float64  `json:"used_quota_actual"`       // 真实成本已用（reseller 看板对比用，sum(actual_cost)）
	SellRate              float64  `json:"sell_rate"`               // 销售倍率，0 表示客户侧免费，1 表示不加价
	MaxConcurrency        int      `json:"max_concurrency"`         // API Key 级并发上限，0 表示不限制
	BalanceAlertEnabled   bool     `json:"balance_alert_enabled"`   // API Key 剩余额度邮件提醒开关
	BalanceAlertEmail     string   `json:"balance_alert_email"`     // API Key 剩余额度提醒接收邮箱
	BalanceAlertThreshold float64  `json:"balance_alert_threshold"` // API Key 剩余额度提醒阈值
	TodayCost             float64  `json:"today_cost"`              // 今日销售金额（sum(billed_cost)，含 sell_rate）
	TodayActualCost       float64  `json:"today_actual_cost"`       // 今日消耗金额（sum(actual_cost)，不含 sell_rate）
	ThirtyDayCost         float64  `json:"thirty_day_cost"`         // 近 30 天销售金额（sum(billed_cost)，含 sell_rate）
	ThirtyDayActualCost   float64  `json:"thirty_day_actual_cost"`  // 近 30 天消耗金额（sum(actual_cost)，不含 sell_rate）
	ExpiresAt             *string  `json:"expires_at,omitempty"`
	Status                string   `json:"status"`
	TimeMixin
}

// APIKeyListQuery API Key 列表查询参数。
type APIKeyListQuery struct {
	PageReq
	SearchScope  string `form:"search_scope"`
	IncludeUsage bool   `form:"include_usage"`
}

// CreateAPIKeyReq 创建 API 密钥请求
type CreateAPIKeyReq struct {
	Name                  string   `json:"name" binding:"required"`
	GroupID               int64    `json:"group_id" binding:"required"`
	IPWhitelist           []string `json:"ip_whitelist"`
	IPBlacklist           []string `json:"ip_blacklist"`
	QuotaUSD              float64  `json:"quota_usd"`
	SellRate              *float64 `json:"sell_rate"`                       // 可选，默认 1；0 表示客户侧免费；按 actual_cost 叠加客户侧计费
	MaxConcurrency        int      `json:"max_concurrency" binding:"gte=0"` // 0 表示不限制并发
	BalanceAlertEnabled   bool     `json:"balance_alert_enabled"`
	BalanceAlertEmail     string   `json:"balance_alert_email"`
	BalanceAlertThreshold float64  `json:"balance_alert_threshold" binding:"gte=0"`
	ExpiresAt             *string  `json:"expires_at"`
}

// UpdateAPIKeyReq 更新 API 密钥请求
type UpdateAPIKeyReq struct {
	Name                  *string  `json:"name"`
	GroupID               *int64   `json:"group_id"`
	IPWhitelist           []string `json:"ip_whitelist"`
	IPBlacklist           []string `json:"ip_blacklist"`
	QuotaUSD              *float64 `json:"quota_usd"`
	SellRate              *float64 `json:"sell_rate"`                                 // 动态调整：随时可改，不影响历史 used_quota 累加值
	MaxConcurrency        *int     `json:"max_concurrency" binding:"omitempty,gte=0"` // 0 关闭并发限制
	BalanceAlertEnabled   *bool    `json:"balance_alert_enabled"`
	BalanceAlertEmail     *string  `json:"balance_alert_email"`
	BalanceAlertThreshold *float64 `json:"balance_alert_threshold" binding:"omitempty,gte=0"`
	ExpiresAt             *string  `json:"expires_at"`
	Status                *string  `json:"status" binding:"omitempty,oneof=active disabled"`
}

// AdminUpdateAPIKeyReq 管理员更新密钥请求
type AdminUpdateAPIKeyReq struct {
	UpdateAPIKeyReq
}
