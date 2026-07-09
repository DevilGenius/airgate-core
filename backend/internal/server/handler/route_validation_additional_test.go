package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appauth "github.com/DevilGenius/airgate-core/internal/app/auth"
	"github.com/DevilGenius/airgate-core/internal/infra/mailer"
	"github.com/DevilGenius/airgate-core/internal/upgrade"
)

func invokeHandlerForValidation(method, target, body string, params gin.Params, setup func(*gin.Context), fn func(*gin.Context)) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = params
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	if setup != nil {
		setup(c)
	}
	fn(c)
	return w
}

func TestAdditionalHandlerConstructorsAndErrorHelpers(t *testing.T) {
	if got := NewAuthHandler(nil, nil, nil, mailer.NewVerifyCodeStore(), nil, nil); got == nil {
		t.Fatal("NewAuthHandler returned nil")
	}
	if got := NewDashboardHandler(nil); got == nil {
		t.Fatal("NewDashboardHandler returned nil")
	}
	if got := NewPluginHandler(nil); got == nil {
		t.Fatal("NewPluginHandler returned nil")
	}
	if got := NewSettingsHandler(nil, "secret", nil); got == nil {
		t.Fatal("NewSettingsHandler returned nil")
	}
	if got := NewUsageHandler(nil); got == nil {
		t.Fatal("NewUsageHandler returned nil")
	}
	if got := NewUpgradeHandler(nil); got == nil {
		t.Fatal("NewUpgradeHandler returned nil")
	}

	c, _ := newHandlerTestContext()
	if ensureAdminRole(c) {
		t.Fatal("ensureAdminRole without role = true")
	}
	c.Set("role", "admin")
	if !ensureAdminRole(c) {
		t.Fatal("ensureAdminRole admin = false")
	}

	authHandler := NewAuthHandler(nil, nil, nil, nil, nil, nil)
	if code, _, unauthorized := authHandler.handleLoginError(appauth.ErrInvalidCredentials); code != http.StatusUnauthorized || !unauthorized {
		t.Fatalf("invalid credentials mapping = %d unauthorized=%v", code, unauthorized)
	}
	if code, _, unauthorized := authHandler.handleLoginError(appauth.ErrUserDisabled); code != http.StatusForbidden || !unauthorized {
		t.Fatalf("disabled mapping = %d unauthorized=%v", code, unauthorized)
	}
	if code, _, unauthorized := authHandler.handleLoginError(errRouteValidation); code != http.StatusInternalServerError || unauthorized {
		t.Fatalf("default login mapping = %d unauthorized=%v", code, unauthorized)
	}
	if code, _ := authHandler.handleRegisterError(appauth.ErrEmailAlreadyExists); code != http.StatusBadRequest {
		t.Fatalf("email exists mapping = %d", code)
	}
	if code, _ := authHandler.handleRegisterError(errRouteValidation); code != http.StatusInternalServerError {
		t.Fatalf("default register mapping = %d", code)
	}
}

var errRouteValidation = errors.New("route validation")

func TestAuthAndDashboardValidationBranches(t *testing.T) {
	authHandler := NewAuthHandler(nil, nil, nil, mailer.NewVerifyCodeStore(), nil, nil)
	for _, tt := range []struct {
		name   string
		method string
		target string
		body   string
		fn     func(*gin.Context)
		status int
	}{
		{name: "login bind", method: http.MethodPost, target: "/login", body: `{}`, fn: authHandler.Login, status: http.StatusBadRequest},
		{name: "api key bad format", method: http.MethodPost, target: "/login/key", body: `{"key":"bad"}`, fn: authHandler.LoginByAPIKey, status: http.StatusBadRequest},
		{name: "verify code bind", method: http.MethodPost, target: "/verify", body: `{}`, fn: authHandler.VerifyCode, status: http.StatusBadRequest},
		{name: "send verify bind", method: http.MethodPost, target: "/send", body: `{}`, fn: authHandler.SendVerifyCode, status: http.StatusBadRequest},
		{name: "refresh missing token", method: http.MethodPost, target: "/refresh", fn: authHandler.RefreshToken, status: http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, nil, nil, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	c, _ := newHandlerTestContext()
	c.Request.Header.Set("Authorization", "Bearer token")
	if got := extractRefreshBearerToken(c); got != "token" {
		t.Fatalf("extract bearer = %q", got)
	}
	c.Request.Header.Set("Authorization", "Basic token")
	if got := extractRefreshBearerToken(c); got != "" {
		t.Fatalf("extract basic = %q", got)
	}

	dashboardHandler := NewDashboardHandler(nil)
	for _, tt := range []struct {
		name   string
		target string
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		status int
	}{
		{name: "stats forbidden", target: "/stats", fn: dashboardHandler.Stats, status: http.StatusForbidden},
		{name: "trend forbidden", target: "/trend", fn: dashboardHandler.Trend, status: http.StatusForbidden},
		{name: "stats bind", target: "/stats?user_id=bad", setup: func(c *gin.Context) { c.Set("role", "admin") }, fn: dashboardHandler.Stats, status: http.StatusBadRequest},
		{name: "trend bind", target: "/trend?range=bad&granularity=hour", setup: func(c *gin.Context) { c.Set("role", "admin") }, fn: dashboardHandler.Trend, status: http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(http.MethodGet, tt.target, "", nil, tt.setup, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestCRUDHandlerValidationBranches(t *testing.T) {
	accountHandler := NewAccountHandler(nil, nil)
	apiKeyHandler := NewAPIKeyHandler(nil, nil)
	groupHandler := NewGroupHandler(nil, nil)
	proxyHandler := NewProxyHandler(nil)
	userHandler := NewUserHandler(nil, nil, nil)
	subscriptionHandler := NewSubscriptionHandler(nil)

	withUser := func(c *gin.Context) { c.Set("user_id", 7) }
	badID := gin.Params{{Key: "id", Value: "bad"}}
	validID := gin.Params{{Key: "id", Value: "7"}}

	tests := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		status int
	}{
		{name: "account list bind", method: http.MethodGet, target: "/accounts?page=0&page_size=1", fn: accountHandler.ListAccounts, status: http.StatusBadRequest},
		{name: "account import bind", method: http.MethodPost, target: "/accounts/import", body: `{}`, fn: accountHandler.ImportAccounts, status: http.StatusBadRequest},
		{name: "account import unknown version rejected", method: http.MethodPost, target: "/accounts/import", body: `{"version":3,"accounts":[{"name":"future"}]}`, fn: accountHandler.ImportAccounts, status: http.StatusBadRequest},
		{name: "account create bind", method: http.MethodPost, target: "/accounts", body: `{}`, fn: accountHandler.CreateAccount, status: http.StatusBadRequest},
		{name: "account update bad id", method: http.MethodPatch, target: "/accounts/bad", params: badID, body: `{}`, fn: accountHandler.UpdateAccount, status: http.StatusBadRequest},
		{name: "account delete bad id", method: http.MethodDelete, target: "/accounts/bad", params: badID, fn: accountHandler.DeleteAccount, status: http.StatusBadRequest},
		{name: "account bulk update bind", method: http.MethodPatch, target: "/accounts/bulk", body: `{}`, fn: accountHandler.BulkUpdateAccounts, status: http.StatusBadRequest},
		{name: "account bulk delete bind", method: http.MethodDelete, target: "/accounts/bulk", body: `{}`, fn: accountHandler.BulkDeleteAccounts, status: http.StatusBadRequest},
		{name: "account bulk clear no scheduler", method: http.MethodPost, target: "/accounts/cooldowns", body: `{"account_ids":[1]}`, fn: accountHandler.BulkClearFamilyCooldowns, status: http.StatusServiceUnavailable},
		{name: "account toggle bad id", method: http.MethodPost, target: "/accounts/bad/toggle", params: badID, fn: accountHandler.ToggleScheduling, status: http.StatusBadRequest},
		{name: "account test bad id", method: http.MethodPost, target: "/accounts/bad/test", params: badID, fn: accountHandler.TestAccount, status: http.StatusBadRequest},
		{name: "account models bad id", method: http.MethodGet, target: "/accounts/bad/models", params: badID, fn: accountHandler.GetAccountModels, status: http.StatusBadRequest},
		{name: "account single usage bad id", method: http.MethodGet, target: "/accounts/bad/usage", params: badID, fn: accountHandler.GetSingleAccountUsage, status: http.StatusBadRequest},
		{name: "account clear cooldown bad id", method: http.MethodPost, target: "/accounts/bad/cooldown", params: badID, fn: accountHandler.ClearFamilyCooldowns, status: http.StatusBadRequest},
		{name: "account clear cooldown no scheduler", method: http.MethodPost, target: "/accounts/7/cooldown", params: validID, fn: accountHandler.ClearFamilyCooldowns, status: http.StatusServiceUnavailable},
		{name: "account refresh quota bad id", method: http.MethodPost, target: "/accounts/bad/quota", params: badID, fn: accountHandler.RefreshQuota, status: http.StatusBadRequest},
		{name: "account stats bad id", method: http.MethodGet, target: "/accounts/bad/stats", params: badID, fn: accountHandler.GetAccountStats, status: http.StatusBadRequest},

		{name: "apikey list unauthorized", method: http.MethodGet, target: "/keys", fn: apiKeyHandler.ListKeys, status: http.StatusUnauthorized},
		{name: "apikey admin list bind", method: http.MethodGet, target: "/admin/keys?page=0&page_size=1", fn: apiKeyHandler.AdminListKeys, status: http.StatusBadRequest},
		{name: "apikey create unauthorized", method: http.MethodPost, target: "/keys", body: `{}`, fn: apiKeyHandler.CreateKey, status: http.StatusUnauthorized},
		{name: "apikey update unauthorized", method: http.MethodPatch, target: "/keys/7", params: validID, body: `{}`, fn: apiKeyHandler.UpdateKey, status: http.StatusUnauthorized},
		{name: "apikey delete unauthorized", method: http.MethodDelete, target: "/keys/7", params: validID, fn: apiKeyHandler.DeleteKey, status: http.StatusUnauthorized},
		{name: "apikey admin update bad id", method: http.MethodPatch, target: "/admin/keys/bad", params: badID, body: `{}`, fn: apiKeyHandler.AdminUpdateKey, status: http.StatusBadRequest},
		{name: "apikey reset bad id", method: http.MethodPost, target: "/admin/keys/bad/reset", params: badID, fn: apiKeyHandler.AdminResetKeyUsage, status: http.StatusBadRequest},
		{name: "apikey reveal unauthorized", method: http.MethodGet, target: "/keys/7/reveal", params: validID, fn: apiKeyHandler.RevealKey, status: http.StatusUnauthorized},

		{name: "group list bind", method: http.MethodGet, target: "/groups?page=0&page_size=1", fn: groupHandler.ListGroups, status: http.StatusBadRequest},
		{name: "group available unauthorized", method: http.MethodGet, target: "/groups/available", fn: groupHandler.ListAvailableGroups, status: http.StatusUnauthorized},
		{name: "group get bad id", method: http.MethodGet, target: "/groups/bad", params: badID, fn: groupHandler.GetGroup, status: http.StatusBadRequest},
		{name: "group create bind", method: http.MethodPost, target: "/groups", body: `{}`, fn: groupHandler.CreateGroup, status: http.StatusBadRequest},
		{name: "group update bad id", method: http.MethodPatch, target: "/groups/bad", params: badID, body: `{}`, fn: groupHandler.UpdateGroup, status: http.StatusBadRequest},
		{name: "group delete bad id", method: http.MethodDelete, target: "/groups/bad", params: badID, fn: groupHandler.DeleteGroup, status: http.StatusBadRequest},

		{name: "proxy list bind", method: http.MethodGet, target: "/proxies?page=0&page_size=1", fn: proxyHandler.ListProxies, status: http.StatusBadRequest},
		{name: "proxy create bind", method: http.MethodPost, target: "/proxies", body: `{}`, fn: proxyHandler.CreateProxy, status: http.StatusBadRequest},
		{name: "proxy update bad id", method: http.MethodPatch, target: "/proxies/bad", params: badID, body: `{}`, fn: proxyHandler.UpdateProxy, status: http.StatusBadRequest},
		{name: "proxy delete bad id", method: http.MethodDelete, target: "/proxies/bad", params: badID, fn: proxyHandler.DeleteProxy, status: http.StatusBadRequest},
		{name: "proxy test bad id", method: http.MethodPost, target: "/proxies/bad/test", params: badID, fn: proxyHandler.TestProxy, status: http.StatusBadRequest},

		{name: "user me unauthorized", method: http.MethodGet, target: "/me", fn: userHandler.GetMe, status: http.StatusUnauthorized},
		{name: "user profile unauthorized", method: http.MethodPatch, target: "/me", body: `{}`, fn: userHandler.UpdateProfile, status: http.StatusUnauthorized},
		{name: "user balance alert unauthorized", method: http.MethodPatch, target: "/me/alert", body: `{}`, fn: userHandler.UpdateBalanceAlert, status: http.StatusUnauthorized},
		{name: "user password unauthorized", method: http.MethodPost, target: "/me/password", body: `{}`, fn: userHandler.ChangePassword, status: http.StatusUnauthorized},
		{name: "user balance history unauthorized", method: http.MethodGet, target: "/me/balance", fn: userHandler.GetMyBalanceHistory, status: http.StatusUnauthorized},
		{name: "user list bind", method: http.MethodGet, target: "/users?page=0&page_size=1", fn: userHandler.ListUsers, status: http.StatusBadRequest},
		{name: "user create bind", method: http.MethodPost, target: "/users", body: `{}`, fn: userHandler.CreateUser, status: http.StatusBadRequest},
		{name: "user update bad id", method: http.MethodPatch, target: "/users/bad", params: badID, body: `{}`, fn: userHandler.UpdateUser, status: http.StatusBadRequest},
		{name: "user adjust bad id", method: http.MethodPost, target: "/users/bad/balance", params: badID, body: `{}`, fn: userHandler.AdjustBalance, status: http.StatusBadRequest},
		{name: "user delete bad id", method: http.MethodDelete, target: "/users/bad", params: badID, fn: userHandler.DeleteUser, status: http.StatusBadRequest},
		{name: "user toggle bad id", method: http.MethodPost, target: "/users/bad/toggle", params: badID, fn: userHandler.ToggleUserStatus, status: http.StatusBadRequest},
		{name: "user history bad id", method: http.MethodGet, target: "/users/bad/balance", params: badID, fn: userHandler.GetUserBalanceHistory, status: http.StatusBadRequest},
		{name: "user keys bad id", method: http.MethodGet, target: "/users/bad/keys", params: badID, fn: userHandler.AdminListUserKeys, status: http.StatusBadRequest},

		{name: "subscription user unauthorized", method: http.MethodGet, target: "/subscriptions", fn: subscriptionHandler.UserSubscriptions, status: http.StatusUnauthorized},
		{name: "subscription active unauthorized", method: http.MethodGet, target: "/subscriptions/active", fn: subscriptionHandler.ActiveSubscriptions, status: http.StatusUnauthorized},
		{name: "subscription progress unauthorized", method: http.MethodGet, target: "/subscriptions/progress", fn: subscriptionHandler.SubscriptionProgress, status: http.StatusUnauthorized},
		{name: "subscription admin list bind", method: http.MethodGet, target: "/admin/subscriptions?page=0&page_size=1", fn: subscriptionHandler.AdminListSubscriptions, status: http.StatusBadRequest},
		{name: "subscription assign bind", method: http.MethodPost, target: "/admin/subscriptions", body: `{}`, fn: subscriptionHandler.AdminAssign, status: http.StatusBadRequest},
		{name: "subscription bulk assign bind", method: http.MethodPost, target: "/admin/subscriptions/bulk", body: `{}`, fn: subscriptionHandler.AdminBulkAssign, status: http.StatusBadRequest},
		{name: "subscription adjust bad id", method: http.MethodPatch, target: "/admin/subscriptions/bad", params: badID, body: `{}`, fn: subscriptionHandler.AdminAdjust, status: http.StatusBadRequest},

		{name: "list available groups bind", method: http.MethodGet, target: "/groups/available?page=0&page_size=1", setup: withUser, fn: groupHandler.ListAvailableGroups, status: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestPluginSettingsUsageAndUpgradeValidationBranches(t *testing.T) {
	pluginHandler := NewPluginHandler(nil)
	settingsHandler := NewSettingsHandler(nil, "secret", nil)
	usageHandler := NewUsageHandler(nil)
	upgradeHandler := NewUpgradeHandler(&upgrade.Service{})
	withUser := func(c *gin.Context) { c.Set("user_id", 7) }
	badID := gin.Params{{Key: "id", Value: "bad"}}
	validName := gin.Params{{Key: "name", Value: "plugin"}}

	tests := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		status int
	}{
		{name: "plugin config bind", method: http.MethodPatch, target: "/plugins/plugin/config", params: validName, body: `{`, fn: pluginHandler.UpdatePluginConfig, status: http.StatusBadRequest},
		{name: "plugin upload missing file", method: http.MethodPost, target: "/plugins/upload", fn: pluginHandler.UploadPlugin, status: http.StatusBadRequest},
		{name: "plugin github bind", method: http.MethodPost, target: "/plugins/github", body: `{}`, fn: pluginHandler.InstallFromGithub, status: http.StatusBadRequest},
		{name: "plugin uninstall missing name", method: http.MethodDelete, target: "/plugins/", fn: pluginHandler.UninstallPlugin, status: http.StatusBadRequest},
		{name: "plugin reload missing name", method: http.MethodPost, target: "/plugins/reload", fn: pluginHandler.ReloadPlugin, status: http.StatusBadRequest},
		{name: "plugin proxy missing name", method: http.MethodPost, target: "/plugins//proxy", fn: pluginHandler.ProxyRequest, status: http.StatusBadRequest},

		{name: "settings update bind", method: http.MethodPatch, target: "/settings", body: `{`, fn: settingsHandler.UpdateSettings, status: http.StatusBadRequest},
		{name: "settings smtp bind", method: http.MethodPost, target: "/settings/smtp", body: `{}`, fn: settingsHandler.TestSMTP, status: http.StatusBadRequest},
		{name: "settings notification bind", method: http.MethodPost, target: "/settings/notification", body: `{}`, fn: settingsHandler.TestNotification, status: http.StatusBadRequest},
		{name: "settings upload missing file", method: http.MethodPost, target: "/settings/upload", fn: settingsHandler.UploadFile, status: http.StatusBadRequest},

		{name: "usage user unauthorized", method: http.MethodGet, target: "/usage", fn: usageHandler.UserUsage, status: http.StatusUnauthorized},
		{name: "usage user bind", method: http.MethodGet, target: "/usage?before_id=bad", setup: withUser, fn: usageHandler.UserUsage, status: http.StatusBadRequest},
		{name: "usage stats unauthorized", method: http.MethodGet, target: "/usage/stats", fn: usageHandler.UserUsageStats, status: http.StatusUnauthorized},
		{name: "usage trend unauthorized", method: http.MethodGet, target: "/usage/trend", fn: usageHandler.UserUsageTrend, status: http.StatusUnauthorized},
		{name: "usage admin bind", method: http.MethodGet, target: "/admin/usage?before_id=bad", fn: usageHandler.AdminUsage, status: http.StatusBadRequest},
		{name: "usage admin stats bind", method: http.MethodGet, target: "/admin/usage/stats", fn: usageHandler.AdminUsageStats, status: http.StatusBadRequest},
		{name: "usage admin trend bind", method: http.MethodGet, target: "/admin/usage/trend?granularity=bad", fn: usageHandler.AdminUsageTrend, status: http.StatusBadRequest},

		{name: "upgrade run bind", method: http.MethodPost, target: "/upgrade/run", body: `{`, fn: upgradeHandler.Run, status: http.StatusBadRequest},
		{name: "monitor request clear bad id", method: http.MethodDelete, target: "/monitor/requests/bad", params: badID, fn: NewMonitorHandler(nil).GetMonitorEvent, status: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}

}
