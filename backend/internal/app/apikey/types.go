package apikey

import (
	"context"
	"time"
)

const SearchScopeAPIKey = "api_key"

// Key API Key 领域对象。
type Key struct {
	ID                    int
	Name                  string
	KeyHint               string
	KeyHash               string
	KeyEncrypted          string
	PlainKey              string
	UserID                int
	GroupID               *int
	GroupRate             float64 // 所属分组对该用户生效的实际扣费倍率（未绑定分组时为 0）
	IPWhitelist           []string
	IPBlacklist           []string
	QuotaUSD              float64
	UsedQuota             float64 // 账面已用（含 sell_rate markup）
	UsedQuotaActual       float64 // 真实成本已用（聚合 sum(usage_log.actual_cost)，仅在 fetch 时填充）
	SellRate              float64 // 销售倍率，0 表示客户侧免费，1 表示不加价
	MaxConcurrency        int     // API Key 级并发上限，0 表示不限制
	BalanceAlertEnabled   bool    // API Key 剩余额度邮件提醒开关
	BalanceAlertEmail     string  // API Key 剩余额度提醒接收邮箱
	BalanceAlertThreshold float64 // API Key 剩余额度提醒阈值
	BalanceAlertNotified  bool    // API Key 剩余额度提醒已通知标记
	TodayCost             float64 // 今日销售金额（sum(billed_cost)，含 sell_rate）
	TodayActualCost       float64 // 今日消耗金额（sum(actual_cost)，不含 sell_rate）
	ThirtyDayCost         float64 // 近 30 天销售金额（sum(billed_cost)，含 sell_rate）
	ThirtyDayActualCost   float64 // 近 30 天消耗金额（sum(actual_cost)，不含 sell_rate）
	Status                string
	ExpiresAt             *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// UsageCosts API Key 窗口用量金额。
type UsageCosts struct {
	TodaySalesCost      float64
	TodayActualCost     float64
	ThirtyDaySalesCost  float64
	ThirtyDayActualCost float64
}

// ListFilter API Key 列表查询参数。
type ListFilter struct {
	Page         int
	PageSize     int
	Keyword      string
	SearchScope  string
	IncludeUsage bool
	TZ           string
}

// ListResult API Key 列表结果。
type ListResult struct {
	List     []Key
	Total    int64
	Page     int
	PageSize int
}

// CreateInput 创建 API Key 输入。
type CreateInput struct {
	Name                  string
	GroupID               int64
	IPWhitelist           []string
	IPBlacklist           []string
	QuotaUSD              float64
	SellRate              *float64
	MaxConcurrency        int // 0 表示不限制
	BalanceAlertEnabled   bool
	BalanceAlertEmail     string
	BalanceAlertThreshold float64
	ExpiresAt             *string
}

// UpdateInput 更新 API Key 输入。
type UpdateInput struct {
	Name                  *string
	GroupID               *int64
	IPWhitelist           []string
	HasIPWhitelist        bool
	IPBlacklist           []string
	HasIPBlacklist        bool
	QuotaUSD              *float64
	SellRate              *float64
	MaxConcurrency        *int // nil 表示不改动；指向 0 表示关闭并发限制
	BalanceAlertEnabled   *bool
	BalanceAlertEmail     *string
	BalanceAlertThreshold *float64
	ExpiresAt             *string
	Status                *string
}

// GroupAccess 分组可用性检查结果。
type GroupAccess struct {
	Exists  bool
	Allowed bool
}

// Mutation 创建/更新持久化输入。
type Mutation struct {
	Name                      *string
	KeyHint                   *string
	KeyHash                   *string
	KeyEncrypted              *string
	UserID                    *int
	GroupID                   *int
	IPWhitelist               []string
	HasIPWhitelist            bool
	IPBlacklist               []string
	HasIPBlacklist            bool
	QuotaUSD                  *float64
	SellRate                  *float64
	MaxConcurrency            *int
	BalanceAlertEnabled       *bool
	BalanceAlertEmail         *string
	BalanceAlertThreshold     *float64
	ResetBalanceAlertNotified bool
	ExpiresAt                 *time.Time
	HasExpiresAt              bool
	Status                    *string
}

// Repository API Key 持久化接口。
type Repository interface {
	ListByUser(context.Context, int, ListFilter) ([]Key, int64, error)
	ListAdmin(context.Context, ListFilter) ([]Key, int64, error)
	// KeyUsage 返回每个 key 的"今日"和"近 30 天"销售/消耗金额。
	// todayStart 必须由调用方按用户时区计算好。
	KeyUsage(ctx context.Context, keyIDs []int, todayStart time.Time) (map[int]UsageCosts, error)
	GetGroupAccess(context.Context, int, int) (GroupAccess, error)
	Create(context.Context, Mutation) (Key, error)
	UpdateOwned(context.Context, int, int, Mutation) (Key, error)
	UpdateAdmin(context.Context, int, Mutation) (Key, error)
	ResetUsageAdmin(context.Context, int) (Key, error)
	DeleteOwned(context.Context, int, int) (Key, error)
	FindOwned(context.Context, int, int) (Key, error)
}
