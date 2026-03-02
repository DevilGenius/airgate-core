package server

import (
	"github.com/DouDOU-start/airgate-core/internal/plugin"
	"github.com/DouDOU-start/airgate-core/internal/server/handler"
	"github.com/DouDOU-start/airgate-core/internal/server/middleware"
	"github.com/DouDOU-start/airgate-core/internal/setup"
	"github.com/gin-gonic/gin"
)

// registerRoutes 注册所有 API 路由
func (s *Server) registerRoutes() {
	r := s.engine

	// 全局中间件
	r.Use(middleware.I18n())

	// 安装向导路由（无需认证）
	setup.RegisterRoutes(r)

	// 初始化所有 Handler
	authHandler := handler.NewAuthHandler(s.db, s.jwtMgr)
	userHandler := handler.NewUserHandler(s.db)
	accountHandler := handler.NewAccountHandler(s.db)
	groupHandler := handler.NewGroupHandler(s.db)
	apikeyHandler := handler.NewAPIKeyHandler(s.db)
	subscriptionHandler := handler.NewSubscriptionHandler(s.db)
	usageHandler := handler.NewUsageHandler(s.db)
	proxyHandler := handler.NewProxyHandler(s.db)
	settingsHandler := handler.NewSettingsHandler(s.db)
	dashboardHandler := handler.NewDashboardHandler(s.db)

	// 插件相关
	pluginMgr := plugin.NewManager(s.db, "plugins")
	pluginMarketplace := plugin.NewMarketplace(s.db)
	pluginHandler := handler.NewPluginHandler(s.db, pluginMgr, pluginMarketplace)

	// API v1 路由组
	v1 := r.Group("/api/v1")

	// === 认证路由（无需 JWT） ===
	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/login", authHandler.Login)
		authGroup.POST("/register", authHandler.Register)
	}

	// === 用户路由（需要 JWT 认证） ===
	userGroup := v1.Group("")
	userGroup.Use(middleware.JWTAuth(s.jwtMgr))
	{
		// Token 刷新
		userGroup.POST("/auth/refresh", authHandler.RefreshToken)

		// TOTP 管理
		userGroup.POST("/auth/totp/setup", authHandler.TOTPSetup)
		userGroup.POST("/auth/totp/verify", authHandler.TOTPVerify)
		userGroup.POST("/auth/totp/disable", authHandler.TOTPDisable)

		// 用户资料
		userGroup.GET("/users/me", userHandler.GetMe)
		userGroup.PUT("/users/me", userHandler.UpdateProfile)
		userGroup.POST("/users/me/password", userHandler.ChangePassword)

		// API Key 管理
		userGroup.GET("/api-keys", apikeyHandler.ListKeys)
		userGroup.POST("/api-keys", apikeyHandler.CreateKey)
		userGroup.PUT("/api-keys/:id", apikeyHandler.UpdateKey)
		userGroup.DELETE("/api-keys/:id", apikeyHandler.DeleteKey)

		// 订阅
		userGroup.GET("/subscriptions", subscriptionHandler.UserSubscriptions)
		userGroup.GET("/subscriptions/active", subscriptionHandler.ActiveSubscriptions)
		userGroup.GET("/subscriptions/progress", subscriptionHandler.SubscriptionProgress)

		// 使用记录
		userGroup.GET("/usage", usageHandler.UserUsage)

		// 仪表盘
		userGroup.GET("/dashboard/stats", dashboardHandler.Stats)
	}

	// === 管理员路由（需要 JWT + AdminOnly） ===
	adminGroup := v1.Group("/admin")
	adminGroup.Use(middleware.JWTAuth(s.jwtMgr), middleware.AdminOnly())
	{
		// 用户管理
		adminGroup.GET("/users", userHandler.ListUsers)
		adminGroup.POST("/users", userHandler.CreateUser)
		adminGroup.PUT("/users/:id", userHandler.UpdateUser)
		adminGroup.POST("/users/:id/balance", userHandler.AdjustBalance)

		// 账号管理
		adminGroup.GET("/accounts", accountHandler.ListAccounts)
		adminGroup.POST("/accounts", accountHandler.CreateAccount)
		adminGroup.PUT("/accounts/:id", accountHandler.UpdateAccount)
		adminGroup.DELETE("/accounts/:id", accountHandler.DeleteAccount)
		adminGroup.POST("/accounts/:id/test", accountHandler.TestAccount)
		adminGroup.GET("/accounts/credentials-schema/:platform", accountHandler.GetCredentialsSchema)

		// 分组管理
		adminGroup.GET("/groups", groupHandler.ListGroups)
		adminGroup.POST("/groups", groupHandler.CreateGroup)
		adminGroup.GET("/groups/:id", groupHandler.GetGroup)
		adminGroup.PUT("/groups/:id", groupHandler.UpdateGroup)

		// API 密钥管理（管理员）
		adminGroup.PUT("/api-keys/:id", apikeyHandler.AdminUpdateKey)

		// 订阅管理
		adminGroup.GET("/subscriptions", subscriptionHandler.AdminListSubscriptions)
		adminGroup.POST("/subscriptions/assign", subscriptionHandler.AdminAssign)
		adminGroup.POST("/subscriptions/bulk-assign", subscriptionHandler.AdminBulkAssign)
		adminGroup.PUT("/subscriptions/:id/adjust", subscriptionHandler.AdminAdjust)

		// 代理池管理
		adminGroup.GET("/proxies", proxyHandler.ListProxies)
		adminGroup.POST("/proxies", proxyHandler.CreateProxy)
		adminGroup.PUT("/proxies/:id", proxyHandler.UpdateProxy)
		adminGroup.DELETE("/proxies/:id", proxyHandler.DeleteProxy)
		adminGroup.POST("/proxies/:id/test", proxyHandler.TestProxy)

		// 使用记录（管理员）
		adminGroup.GET("/usage", usageHandler.AdminUsage)
		adminGroup.GET("/usage/stats", usageHandler.AdminUsageStats)

		// 插件管理
		adminGroup.GET("/plugins", pluginHandler.ListPlugins)
		adminGroup.POST("/plugins/install", pluginHandler.InstallPlugin)
		adminGroup.POST("/plugins/:id/uninstall", pluginHandler.UninstallPlugin)
		adminGroup.POST("/plugins/:id/enable", pluginHandler.EnablePlugin)
		adminGroup.POST("/plugins/:id/disable", pluginHandler.DisablePlugin)
		adminGroup.PUT("/plugins/:id/config", pluginHandler.UpdateConfig)

		// 系统设置
		adminGroup.GET("/settings", settingsHandler.GetSettings)
		adminGroup.PUT("/settings", settingsHandler.UpdateSettings)

		// 仪表盘（管理员）
		adminGroup.GET("/dashboard/stats", dashboardHandler.Stats)
	}

	// 静态文件服务（前端）
	r.Static("/assets", "web/dist/assets")
	r.NoRoute(func(c *gin.Context) {
		c.File("web/dist/index.html")
	})
}
