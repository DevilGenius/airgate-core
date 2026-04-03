package dto

// SettingResp 设置响应
type SettingResp struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Group string `json:"group"`
}

// UpdateSettingsReq 更新设置请求
type UpdateSettingsReq struct {
	Settings []SettingItem `json:"settings" binding:"required,min=1"`
}

// SettingItem 设置项
type SettingItem struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value"`
	Group string `json:"group"`
}

// TestSMTPReq SMTP 测试请求
type TestSMTPReq struct {
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	Username string `json:"username"`
	Password string `json:"password"`
	UseTLS   bool   `json:"use_tls"`
	From     string `json:"from" binding:"required"`
	To       string `json:"to" binding:"required"`
}
