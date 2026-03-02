package dto

// PluginResp 插件响应
type PluginResp struct {
	ID         int64                  `json:"id"`
	Name       string                 `json:"name"`
	Platform   string                 `json:"platform"`
	Version    string                 `json:"version"`
	Type       string                 `json:"type"` // gateway / payment / extension
	Status     string                 `json:"status"` // installed / enabled / disabled
	Config     map[string]interface{} `json:"config,omitempty"`
	BinaryPath string                 `json:"binary_path,omitempty"`
	TimeMixin
}

// PluginConfigReq 更新插件配置请求
type PluginConfigReq struct {
	Config map[string]interface{} `json:"config" binding:"required"`
}

// InstallPluginReq 安装插件请求
type InstallPluginReq struct {
	Name    string `json:"name" binding:"required"`
	Source  string `json:"source"` // 插件源名称，为空则使用默认
	Version string `json:"version"` // 版本号，为空则安装最新
}

// PluginSourceResp 插件源响应
type PluginSourceResp struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	IsOfficial bool   `json:"is_official"`
	LastSyncAt string `json:"last_sync_at,omitempty"`
}

// MarketplacePluginResp 插件市场条目
type MarketplacePluginResp struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Type        string `json:"type"`
	Installed   bool   `json:"installed"`
}
