package dto

import (
	"encoding/json"

	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
)

// FamilyCooldownDTO 家族级限流冷却（Redis 侧），AccountResp.FamilyCooldowns 元素。
//
// 与 state=rate_limited 的账号级状态区别：账号级是 DB 字段、影响整账号；
// 家族级落 Redis、TTL 自然回收，只影响该 family 的请求。两者会同时存在或独立存在。
type FamilyCooldownDTO struct {
	Family string `json:"family"`
	Until  string `json:"until"` // RFC3339 UTC
	Reason string `json:"reason,omitempty"`
}

// ModelDemotionDTO 账号当前 30 分钟桶内因成功率低于阈值而被降级的调度模型（内存态）。
type ModelDemotionDTO struct {
	Model         string  `json:"model"`
	SuccessRate   float64 `json:"success_rate"`
	ValidRequests uint64  `json:"valid_requests"`
}

// AccountResp 账号响应。
//
// state 枚举：active / rate_limited / degraded / disabled
// state_until 仅 rate_limited / degraded 有值（到期自动恢复 active）
// family_cooldowns 当前在 Redis 上仍生效的家族级冷却列表（gpt-image 撞 4000/min 等），
// state=active 也可能非空。
// today_image_count / total_image_count 仅 OpenAI 平台账号在列表接口下填充；
// 用户期望"账号管理页一眼看到今天/累计生了几张图"。
type AccountResp struct {
	ID                      int64               `json:"id"`
	Name                    string              `json:"name"`
	Email                   *string             `json:"email"`
	Platform                string              `json:"platform"`
	Type                    string              `json:"type"`
	Credentials             map[string]string   `json:"credentials"`
	ModelPolicy             modelpolicy.Policy  `json:"model_policy"`
	State                   string              `json:"state"`
	StateUntil              *string             `json:"state_until,omitempty"`
	Priority                int                 `json:"priority"`
	MaxConcurrency          int                 `json:"max_concurrency"`
	CurrentConcurrency      int                 `json:"current_concurrency"`
	ProxyID                 *int64              `json:"proxy_id,omitempty"`
	RateMultiplier          float64             `json:"rate_multiplier"`
	ModelDowngradeThreshold float64             `json:"model_downgrade_threshold"`
	ErrorMsg                string              `json:"error_msg,omitempty"`
	UpstreamIsPool          bool                `json:"upstream_is_pool"`
	Extra                   map[string]any      `json:"extra,omitempty"`
	LastUsedAt              *string             `json:"last_used_at,omitempty"`
	LastProbeAt             *string             `json:"last_probe_at,omitempty"`
	Usage5hGrowthDate       string              `json:"usage_5h_growth_date,omitempty"`
	Usage5hDailyGrowth      float64             `json:"usage_5h_daily_growth"`
	Usage5hObservedAt       *string             `json:"usage_5h_observed_at,omitempty"`
	Usage7dGrowthDate       string              `json:"usage_7d_growth_date,omitempty"`
	Usage7dDailyGrowth      float64             `json:"usage_7d_daily_growth"`
	Usage7dObservedAt       *string             `json:"usage_7d_observed_at,omitempty"`
	DeletedAt               *string             `json:"deleted_at,omitempty"`
	GroupIDs                []int64             `json:"group_ids"`
	FamilyCooldowns         []FamilyCooldownDTO `json:"family_cooldowns,omitempty"`
	ModelDemotions          []ModelDemotionDTO  `json:"model_demotions,omitempty"`
	TodayImageCount         *int64              `json:"today_image_count,omitempty"`
	TotalImageCount         *int64              `json:"total_image_count,omitempty"`
	TimeMixin
}

// CreateAccountReq 创建账号请求
type CreateAccountReq struct {
	Name                    string             `json:"name" binding:"required"`
	Email                   *string            `json:"email"`
	Platform                string             `json:"platform" binding:"required"`
	Type                    string             `json:"type"` // 账号类型，如 "apikey", "oauth"
	Credentials             map[string]string  `json:"credentials" binding:"required"`
	ModelPolicy             modelpolicy.Policy `json:"model_policy"`
	Priority                int                `json:"priority"`
	MaxConcurrency          int                `json:"max_concurrency"`
	ProxyID                 *int64             `json:"proxy_id"`
	RateMultiplier          OptionalFloat      `json:"rate_multiplier"`
	ModelDowngradeThreshold float64            `json:"model_downgrade_threshold"`
	UpstreamIsPool          bool               `json:"upstream_is_pool"`
	Extra                   map[string]any     `json:"extra,omitempty"`
	GroupIDs                []int64            `json:"group_ids"`
}

// UpdateAccountReq 更新账号请求。
// State 只允许 "active" / "disabled"（运维手动恢复 / 禁用）；
// rate_limited / degraded 由状态机自动写入，不接受 API 显式赋值。
type UpdateAccountReq struct {
	Name                    *string             `json:"name"`
	Email                   *string             `json:"email"`
	Type                    *string             `json:"type"`
	Credentials             map[string]string   `json:"credentials"`
	ModelPolicy             *modelpolicy.Policy `json:"model_policy"`
	State                   *string             `json:"state" binding:"omitempty,oneof=active disabled"`
	Priority                *int                `json:"priority"`
	MaxConcurrency          *int                `json:"max_concurrency"`
	ProxyID                 *int64              `json:"proxy_id"`
	RateMultiplier          OptionalFloat       `json:"rate_multiplier"`
	ModelDowngradeThreshold *float64            `json:"model_downgrade_threshold"`
	UpstreamIsPool          *bool               `json:"upstream_is_pool"`
	Extra                   map[string]any      `json:"extra,omitempty"`
	HasExtra                bool                `json:"-"`
	GroupIDs                []int64             `json:"group_ids"`
}

// AccountExportItem 导出文件中的单条账号，仅包含可跨环境迁移的字段。
type AccountExportItem struct {
	Name                    string             `json:"name"`
	Email                   *string            `json:"email"`
	Platform                string             `json:"platform"`
	Type                    string             `json:"type,omitempty"`
	Credentials             map[string]string  `json:"credentials"`
	ModelPolicy             modelpolicy.Policy `json:"model_policy,omitempty"`
	Priority                int                `json:"priority"`
	MaxConcurrency          int                `json:"max_concurrency"`
	RateMultiplier          OptionalFloat      `json:"rate_multiplier"`
	ModelDowngradeThreshold float64            `json:"model_downgrade_threshold"`
}

// AccountExportFile 导出文件结构，仅包含可跨环境迁移的账号本体字段。
type AccountExportFile struct {
	Version    int                 `json:"version"`
	ExportedAt string              `json:"exported_at"`
	Count      int                 `json:"count"`
	Accounts   []AccountExportItem `json:"accounts"`
}

// ImportAccountsReq 批量导入请求
type ImportAccountsReq struct {
	Version  int                 `json:"version"`
	Accounts []AccountExportItem `json:"accounts"`
}

// UnmarshalJSON 同时接受历史裸数组和版本化导入对象。
func (r *ImportAccountsReq) UnmarshalJSON(data []byte) error {
	var legacy []AccountExportItem
	if err := json.Unmarshal(data, &legacy); err == nil {
		r.Version = 0
		r.Accounts = legacy
		return nil
	}

	var envelope struct {
		Version  int                 `json:"version"`
		Accounts []AccountExportItem `json:"accounts"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	r.Version = envelope.Version
	r.Accounts = envelope.Accounts
	return nil
}

// ImportItemErrorResp 导入失败项响应
type ImportItemErrorResp struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// ImportAccountsResp 导入结果响应
type ImportAccountsResp struct {
	Imported int                   `json:"imported"`
	Failed   int                   `json:"failed"`
	Errors   []ImportItemErrorResp `json:"errors,omitempty"`
}

type AccountImportConfigResp struct {
	DSL string `json:"dsl"`
}

type UpdateAccountImportConfigReq struct {
	DSL string `json:"dsl" binding:"required"`
}

// BulkUpdateAccountsReq 批量更新账号请求。
type BulkUpdateAccountsReq struct {
	AccountIDs              []int                    `json:"account_ids" binding:"required,min=1"`
	State                   *string                  `json:"state" binding:"omitempty,oneof=active disabled"`
	Priority                *int                     `json:"priority"`
	PriorityOffset          *int                     `json:"priority_offset"`
	PrioritySequence        *BulkPrioritySequenceReq `json:"priority_sequence"`
	MaxConcurrency          *int                     `json:"max_concurrency"`
	RateMultiplier          OptionalFloat            `json:"rate_multiplier"`
	ModelDowngradeThreshold *float64                 `json:"model_downgrade_threshold"`
	ModelPolicy             *modelpolicy.Policy      `json:"model_policy"`
	GroupIDs                []int64                  `json:"group_ids"`
	ProxyID                 *int64                   `json:"proxy_id"`
	Extra                   map[string]any           `json:"extra,omitempty"`
}

// BulkPrioritySequenceReq 按账号请求顺序生成账号优先级序列。
type BulkPrioritySequenceReq struct {
	Initial   int `json:"initial"`
	Step      int `json:"step"`
	GroupSize int `json:"group_size"`
}

// BulkAccountIDsReq 仅携带账号 ID 列表的批量请求（用于删除、刷新令牌等）。
type BulkAccountIDsReq struct {
	AccountIDs []int `json:"account_ids" binding:"required,min=1"`
}

// BulkOpItemResp 批量操作单条结果。
type BulkOpItemResp struct {
	ID      int    `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// BulkOpResp 批量操作汇总响应。
type BulkOpResp struct {
	Success    int              `json:"success"`
	Failed     int              `json:"failed"`
	SuccessIDs []int            `json:"success_ids"`
	FailedIDs  []int            `json:"failed_ids"`
	Results    []BulkOpItemResp `json:"results"`
}

// CredentialSchemaResp 凭证字段 schema 响应
type CredentialSchemaResp struct {
	Fields       []CredentialFieldResp `json:"fields"`
	AccountTypes []AccountTypeResp     `json:"account_types,omitempty"`
}

// AccountTypeResp 账号类型定义
type AccountTypeResp struct {
	Key         string                `json:"key"`
	Label       string                `json:"label"`
	Description string                `json:"description"`
	Fields      []CredentialFieldResp `json:"fields"`
}

// CredentialFieldResp 凭证字段定义
type CredentialFieldResp struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Type         string `json:"type"` // text / password / textarea / select
	Required     bool   `json:"required"`
	Placeholder  string `json:"placeholder"`
	EditDisabled bool   `json:"edit_disabled,omitempty"` // 编辑模式下隐藏该字段
}
