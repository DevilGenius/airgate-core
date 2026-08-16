package dto

// CredentialAccountsOverviewReq 查询凭证管理账号概览。
//
// platform 用于限制查询范围，account_type 沿用账号管理页的类型标识，
// 包括 oauth、apikey 和 oauth_plan:<platform>:<key>。
type CredentialAccountsOverviewReq struct {
	Platform    string `json:"platform" binding:"required"`
	AccountType string `json:"account_type"`
	Page        int    `json:"page" binding:"omitempty,min=1"`
	PageSize    int    `json:"page_size" binding:"omitempty,min=1,max=200"`
}

// CredentialAccountsOverviewResp 是凭证管理 API Key 使用的只读账号快照。
// 不返回 credentials、extra、代理凭证或模型策略。
type CredentialAccountsOverviewResp struct {
	SchemaVersion  int                          `json:"schema_version"`
	GeneratedAt    string                       `json:"generated_at"`
	Traffic        CredentialTrafficResp        `json:"traffic"`
	Scheduling     CredentialSchedulingResp     `json:"scheduling"`
	AccountSummary CredentialAccountSummaryResp `json:"account_summary"`
	Accounts       CredentialAccountsPageResp   `json:"accounts"`
}

type CredentialTrafficResp struct {
	Source string  `json:"source"`
	RPM1M  float64 `json:"rpm_1m"`
	TPM1M  float64 `json:"tpm_1m"`
	RPM10M float64 `json:"rpm_10m"`
	TPM10M float64 `json:"tpm_10m"`
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
	State              string `json:"state"`
	Priority           int    `json:"priority"`
	StateUntil         string `json:"state_until,omitempty"`
	StateReason        string `json:"state_reason,omitempty"`
	MaxConcurrency     int    `json:"max_concurrency"`
	CurrentConcurrency int    `json:"current_concurrency"`
	LastUsedAt         string `json:"last_used_at,omitempty"`
	LastProbeAt        string `json:"last_probe_at,omitempty"`
}
