package dto

// AccountResp 账号响应
type AccountResp struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	Platform       string            `json:"platform"`
	Credentials    map[string]string `json:"credentials"`
	Status         string            `json:"status"` // active / error / disabled
	Priority       int               `json:"priority"`
	MaxConcurrency int               `json:"max_concurrency"`
	ProxyID        *int64            `json:"proxy_id,omitempty"`
	RateMultiplier float64           `json:"rate_multiplier"`
	ErrorMsg       string            `json:"error_msg,omitempty"`
	LastUsedAt     *string           `json:"last_used_at,omitempty"`
	GroupIDs       []int64           `json:"group_ids"`
	TimeMixin
}

// CreateAccountReq 创建账号请求
type CreateAccountReq struct {
	Name           string            `json:"name" binding:"required"`
	Platform       string            `json:"platform" binding:"required"`
	Credentials    map[string]string `json:"credentials" binding:"required"`
	Priority       int               `json:"priority"`
	MaxConcurrency int               `json:"max_concurrency"`
	ProxyID        *int64            `json:"proxy_id"`
	RateMultiplier float64           `json:"rate_multiplier"`
	GroupIDs       []int64           `json:"group_ids"`
}

// UpdateAccountReq 更新账号请求
type UpdateAccountReq struct {
	Name           *string            `json:"name"`
	Credentials    map[string]string  `json:"credentials"`
	Status         *string            `json:"status" binding:"omitempty,oneof=active disabled"`
	Priority       *int               `json:"priority"`
	MaxConcurrency *int               `json:"max_concurrency"`
	ProxyID        *int64             `json:"proxy_id"`
	RateMultiplier *float64           `json:"rate_multiplier"`
	GroupIDs       []int64            `json:"group_ids"`
}

// CredentialSchemaResp 凭证字段 schema 响应
type CredentialSchemaResp struct {
	Fields []CredentialFieldResp `json:"fields"`
}

// CredentialFieldResp 凭证字段定义
type CredentialFieldResp struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // text / password / textarea / select
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder"`
}
