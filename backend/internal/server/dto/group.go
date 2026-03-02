package dto

// GroupResp 分组响应
type GroupResp struct {
	ID               int64                  `json:"id"`
	Name             string                 `json:"name"`
	Platform         string                 `json:"platform"`
	RateMultiplier   float64                `json:"rate_multiplier"`
	IsExclusive      bool                   `json:"is_exclusive"`
	SubscriptionType string                 `json:"subscription_type"` // standard / subscription
	Quotas           map[string]interface{} `json:"quotas,omitempty"`  // 日/周/月限额
	ModelRouting     map[string][]int64     `json:"model_routing,omitempty"`
	SortWeight       int                    `json:"sort_weight"`
	TimeMixin
}

// CreateGroupReq 创建分组请求
type CreateGroupReq struct {
	Name             string                 `json:"name" binding:"required"`
	Platform         string                 `json:"platform" binding:"required"`
	RateMultiplier   float64                `json:"rate_multiplier"`
	IsExclusive      bool                   `json:"is_exclusive"`
	SubscriptionType string                 `json:"subscription_type" binding:"oneof=standard subscription"`
	Quotas           map[string]interface{} `json:"quotas"`
	ModelRouting     map[string][]int64     `json:"model_routing"`
	SortWeight       int                    `json:"sort_weight"`
}

// UpdateGroupReq 更新分组请求
type UpdateGroupReq struct {
	Name             *string                `json:"name"`
	RateMultiplier   *float64               `json:"rate_multiplier"`
	IsExclusive      *bool                  `json:"is_exclusive"`
	SubscriptionType *string                `json:"subscription_type" binding:"omitempty,oneof=standard subscription"`
	Quotas           map[string]interface{} `json:"quotas"`
	ModelRouting     map[string][]int64     `json:"model_routing"`
	SortWeight       *int                   `json:"sort_weight"`
}
