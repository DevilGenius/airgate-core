package dto

// CredentialAccountsOverviewReq 查询凭证管理账号概览。
//
// platform 用于限制查询范围，account_type 沿用账号管理页的类型标识，
// 包括 oauth、apikey 和 oauth_plan:<platform>:<key>；include_free 独立控制
// 是否读取 Free 账号汇总，Free 不应加入 account_type。
type CredentialAccountsOverviewReq struct {
	Platform    string `json:"platform" binding:"required"`
	AccountType string `json:"account_type"`
	IncludeFree bool   `json:"include_free"`
	Page        int    `json:"page" binding:"omitempty,min=1"`
	PageSize    int    `json:"page_size" binding:"omitempty,min=1,max=200"`
}

// CredentialAccountsOverviewResp 是凭证管理 API Key 使用的只读账号快照。
// Accounts 始终只返回非 Free 账号的详细运行态列表；请求 include_free=true 时，
// FreeAccounts 额外返回独立的状态汇总和 HTTP 401 禁用账号脱敏信息。未请求 Free
// 时不返回 FreeAccounts。不返回 credentials、extra、代理凭证或模型策略。
type CredentialAccountsOverviewResp struct {
	SchemaVersion  int                          `json:"schema_version"`
	GeneratedAt    string                       `json:"generated_at"`
	Traffic        CredentialTrafficResp        `json:"traffic"`
	UsageEstimate  CredentialUsageEstimateResp  `json:"usage_estimate"`
	Scheduling     CredentialSchedulingResp     `json:"scheduling"`
	AccountSummary CredentialAccountSummaryResp `json:"account_summary"`
	FreeAccounts   *CredentialFreeAccountsResp  `json:"free_accounts,omitempty"`
	Accounts       CredentialAccountsPageResp   `json:"accounts"`
}

// CredentialFreeAccountsResp 仅在请求 include_free=true 时返回。
// Free 账号不读取 Redis 容量/占用信息；UnauthorizedAccounts 是 disabled 且
// 状态原因为 HTTP 401 的子集。
type CredentialFreeAccountsResp struct {
	Total                int                         `json:"total"`
	ByState              map[string]int              `json:"by_state"`
	UnauthorizedCount    int                         `json:"unauthorized_count"`
	UnauthorizedAccounts []CredentialFreeAccountResp `json:"unauthorized_accounts,omitempty"`
}

// CredentialFreeAccountResp 是 HTTP 401 Free 账号的脱敏简化信息，不包含容量、占用和原因字段。
type CredentialFreeAccountResp struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	State string `json:"state"`
}

// CredentialUsageEstimateResp 提供账号池标准消耗速率及四个套餐窗口的可用量估算。
type CredentialUsageEstimateResp struct {
	StandardCostPerMinute1M  float64                         `json:"standard_cost_per_minute_1m"`
	StandardCostPerMinute10M float64                         `json:"standard_cost_per_minute_10m"`
	Plus5h                   CredentialUsageAvailabilityResp `json:"plus_5h"`
	Pro5h                    CredentialUsageAvailabilityResp `json:"pro_5h"`
	Plus7d                   CredentialUsageAvailabilityResp `json:"plus_7d"`
	Pro7d                    CredentialUsageAvailabilityResp `json:"pro_7d"`
}

type CredentialUsageAvailabilityResp struct {
	Status string `json:"status"`
	// AvailableMinutes 在状态为 ready 且仍有剩余标准额度时为空，表示可用时长无上限。
	AvailableMinutes      *float64 `json:"available_minutes"`
	AvailableStandardCost *float64 `json:"available_standard_cost"`
}

type CredentialTrafficResp struct {
	Source                  string  `json:"source"`
	RPM1M                   float64 `json:"rpm_1m"`
	TPM1M                   float64 `json:"tpm_1m"`
	RPM10M                  float64 `json:"rpm_10m"`
	TPM10M                  float64 `json:"tpm_10m"`
	TPMPerRPMCoefficient1M  float64 `json:"tpm_per_rpm_coefficient_1m"`
	TPMPerRPMCoefficient10M float64 `json:"tpm_per_rpm_coefficient_10m"`
}

type CredentialSchedulingResp struct {
	Scope     string                 `json:"scope"`
	SampledAt string                 `json:"sampled_at,omitempty"`
	Capacity  CredentialCapacityResp `json:"capacity"`
	Queue     CredentialQueueResp    `json:"queue"`
}

type CredentialCapacityResp struct {
	InUse           int `json:"in_use"`
	Total           int `json:"total"`
	Available       int `json:"available"`
	WorkingAccounts int `json:"working_accounts"`
}

type CredentialQueueResp struct {
	Waiters           int `json:"waiters"`
	WaitingAccounts   int `json:"waiting_accounts"`
	MaxAccountWaiters int `json:"max_account_waiters"`
}

// CredentialAccountSummaryResp 汇总按请求筛选得到的非 Free Accounts；Free 状态见 FreeAccounts。
type CredentialAccountSummaryResp struct {
	Platform           string         `json:"platform"`
	AccountType        string         `json:"account_type,omitempty"`
	Total              int            `json:"total"`
	ConfiguredCapacity int            `json:"configured_capacity"`
	CurrentConcurrency int            `json:"current_concurrency"`
	ByState            map[string]int `json:"by_state"`
}

type CredentialAccountsPageResp struct {
	List     []CredentialAccountResp `json:"list"`
	Total    int                     `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

type CredentialAccountResp struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Email              string `json:"email,omitempty"`
	Platform           string `json:"platform"`
	Type               string `json:"type"`
	PlanType           string `json:"plan_type,omitempty"`
	State              string `json:"state"`
	Priority           int    `json:"priority"`
	StateUntil         string `json:"state_until,omitempty"`
	StateReason        string `json:"state_reason,omitempty"`
	MaxConcurrency     int    `json:"max_concurrency"`
	CurrentConcurrency int    `json:"current_concurrency"`
	LastUsedAt         string `json:"last_used_at,omitempty"`
	LastProbeAt        string `json:"last_probe_at,omitempty"`
}
