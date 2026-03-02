package plugin

import (
	"context"

	"github.com/DouDOU-start/airgate-core/ent"
)

// MarketplacePlugin 市场插件条目
type MarketplacePlugin struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Type        string `json:"type"` // gateway / payment / extension
}

// Marketplace 插件市场
type Marketplace struct {
	db *ent.Client
}

// NewMarketplace 创建插件市场
func NewMarketplace(db *ent.Client) *Marketplace {
	return &Marketplace{db: db}
}

// officialPlugins 官方插件列表
var officialPlugins = []MarketplacePlugin{
	{
		Name:        "gateway-openai",
		Version:     "0.1.0",
		Description: "OpenAI API 网关插件",
		Author:      "AirGate",
		Type:        "gateway",
	},
	{
		Name:        "gateway-claude",
		Version:     "0.1.0",
		Description: "Anthropic Claude API 网关插件",
		Author:      "AirGate",
		Type:        "gateway",
	},
	{
		Name:        "gateway-gemini",
		Version:     "0.1.0",
		Description: "Google Gemini API 网关插件",
		Author:      "AirGate",
		Type:        "gateway",
	},
	{
		Name:        "gateway-sora",
		Version:     "0.1.0",
		Description: "OpenAI Sora 视频生成网关插件",
		Author:      "AirGate",
		Type:        "gateway",
	},
	{
		Name:        "gateway-antigravity",
		Version:     "0.1.0",
		Description: "反重力 API 网关插件",
		Author:      "AirGate",
		Type:        "gateway",
	},
	{
		Name:        "payment-epay",
		Version:     "0.1.0",
		Description: "易支付接入插件",
		Author:      "AirGate",
		Type:        "payment",
	},
}

// ListAvailable 列出可用插件（占位实现，返回官方插件列表）
// 后续从插件源 URL 动态获取
func (m *Marketplace) ListAvailable(ctx context.Context) ([]MarketplacePlugin, error) {
	return officialPlugins, nil
}
