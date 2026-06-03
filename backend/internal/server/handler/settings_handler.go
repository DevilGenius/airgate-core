package handler

import (
	appnotification "github.com/DevilGenius/airgate-core/internal/app/notification"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
)

// SettingsHandler 系统设置 Handler。
type SettingsHandler struct {
	service             *appsettings.Service
	apiKeySecret        string // AES-GCM 加密密钥
	notificationService *appnotification.Service
}

// NewSettingsHandler 创建 SettingsHandler。
func NewSettingsHandler(service *appsettings.Service, apiKeySecret string, notificationService *appnotification.Service) *SettingsHandler {
	return &SettingsHandler{
		service:             service,
		apiKeySecret:        apiKeySecret,
		notificationService: notificationService,
	}
}
