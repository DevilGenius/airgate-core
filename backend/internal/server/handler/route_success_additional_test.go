package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
	appgroup "github.com/DevilGenius/airgate-core/internal/app/group"
	appnotification "github.com/DevilGenius/airgate-core/internal/app/notification"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func requireOKResponse(t *testing.T, w interface {
	CodeValue() int
	BodyString() string
}) {
	t.Helper()
	if w.CodeValue() != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.CodeValue(), http.StatusOK, w.BodyString())
	}
	if !strings.Contains(w.BodyString(), `"message":"ok"`) {
		t.Fatalf("success body missing ok message: %s", w.BodyString())
	}
}

type responseView struct {
	code int
	body string
}

func (v responseView) CodeValue() int     { return v.code }
func (v responseView) BodyString() string { return v.body }

func asResponseView(code int, body string) responseView {
	return responseView{code: code, body: body}
}

func TestGroupAndProxyRoutesSuccessWithSQLite(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "handler_group_proxy_success", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	groupService := appgroup.NewService(store.NewGroupStore(db), scheduler.NewConcurrencyManager(nil))
	groupHandler := NewGroupHandler(groupService, nil)
	createGroup := `{"name":"Default","platform":"openai","subscription_type":"standard","status_visible":false,"rate_multiplier":1.2,"operation_policies":{"images.generate":true},"plugin_settings":{"openai":{"image_enabled":"true"}}}`
	w := invokeHandlerForValidation(http.MethodPost, "/groups", createGroup, nil, nil, groupHandler.CreateGroup)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	groupID, err := db.Group.Query().OnlyID(context.Background())
	if err != nil {
		t.Fatalf("query group id: %v", err)
	}

	w = invokeHandlerForValidation(http.MethodGet, "/groups?page=1&page_size=10&platform=openai&service_tier=", "", nil, nil, groupHandler.ListGroups)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("group list body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/groups/1", "", gin.Params{{Key: "id", Value: "1"}}, nil, groupHandler.GetGroup)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	updateGroup := `{"name":"Renamed","service_tier":"fast","note":"note","sort_weight":5}`
	w = invokeHandlerForValidation(http.MethodPut, "/groups/1", updateGroup, gin.Params{{Key: "id", Value: "1"}}, nil, groupHandler.UpdateGroup)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"name":"Renamed"`) {
		t.Fatalf("group update body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/groups/available?page=1&page_size=10&platform=openai", "", nil, func(c *gin.Context) {
		c.Set("user_id", 123)
	}, groupHandler.ListAvailableGroups)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	proxyHandler := NewProxyHandler(appproxy.NewService(store.NewProxyStore(db)))
	createProxy := `{"name":"local","protocol":"http","address":"127.0.0.1","port":8080,"username":"u","password":"p"}`
	w = invokeHandlerForValidation(http.MethodPost, "/proxies", createProxy, nil, nil, proxyHandler.CreateProxy)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	proxyID, err := db.Proxy.Query().OnlyID(context.Background())
	if err != nil {
		t.Fatalf("query proxy id: %v", err)
	}

	w = invokeHandlerForValidation(http.MethodGet, "/proxies?page=1&page_size=10&status=active", "", nil, nil, proxyHandler.ListProxies)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	updateProxy := `{"name":"disabled","status":"disabled","port":8081}`
	w = invokeHandlerForValidation(http.MethodPut, "/proxies/1", updateProxy, gin.Params{{Key: "id", Value: "1"}}, nil, proxyHandler.UpdateProxy)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"status":"disabled"`) {
		t.Fatalf("proxy update body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodDelete, "/proxies/1", "", gin.Params{{Key: "id", Value: "1"}}, nil, proxyHandler.DeleteProxy)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if proxyID != 1 || groupID != 1 {
		t.Fatalf("unexpected IDs group=%d proxy=%d", groupID, proxyID)
	}
	w = invokeHandlerForValidation(http.MethodDelete, "/groups/1", "", gin.Params{{Key: "id", Value: "1"}}, nil, groupHandler.DeleteGroup)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
}

func TestSettingsDashboardAndUsageRoutesSuccessWithSQLite(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "handler_misc_success", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	settingsService := appsettings.NewService(store.NewSettingsStore(db))
	settingsHandler := NewSettingsHandler(settingsService, strings.Repeat("a", 64), appnotification.NewService(settingsService))
	updateSettings := `{"settings":[{"key":"site_name","value":"AirGate Test","group":"site"},{"key":"registration_enabled","value":"true","group":"registration"},{"key":"smtp_password","value":"secret","group":"smtp"}]}`
	w := invokeHandlerForValidation(http.MethodPut, "/settings", updateSettings, nil, nil, settingsHandler.UpdateSettings)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	for _, tt := range []struct {
		name   string
		target string
		fn     func(*gin.Context)
		want   string
	}{
		{name: "public settings", target: "/settings/public", fn: settingsHandler.GetPublicSettings, want: "AirGate Test"},
		{name: "all settings", target: "/settings?group=site", fn: settingsHandler.GetSettings, want: "site_name"},
		{name: "admin key empty", target: "/settings/admin-api-key", fn: settingsHandler.GetAdminAPIKey},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(http.MethodGet, tt.target, "", nil, nil, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
			if tt.want != "" && !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.want)
			}
		})
	}

	w = invokeHandlerForValidation(http.MethodPost, "/settings/admin-api-key", "", nil, nil, settingsHandler.GenerateAdminAPIKey)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"key":"admin-`) {
		t.Fatalf("generate admin key body = %s", w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodGet, "/settings/admin-api-key", "", nil, nil, settingsHandler.GetAdminAPIKey)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"hint":"admin-`) {
		t.Fatalf("admin key hint body = %s", w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodDelete, "/settings/admin-api-key", "", nil, nil, settingsHandler.DeleteAdminAPIKey)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	dashboardHandler := NewDashboardHandler(appdashboard.NewService(store.NewDashboardStore(db, nil)))
	w = invokeHandlerForValidation(http.MethodGet, "/dashboard/stats?tz=UTC", "", nil, func(c *gin.Context) {
		c.Set("role", "admin")
	}, dashboardHandler.Stats)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	w = invokeHandlerForValidation(http.MethodGet, "/dashboard/trend?range=7d&granularity=day&tz=UTC", "", nil, func(c *gin.Context) {
		c.Set("role", "admin")
	}, dashboardHandler.Trend)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	usageHandler := NewUsageHandler(appusage.NewService(store.NewUsageStore(db)))
	usageCases := []struct {
		name   string
		target string
		setup  func(*gin.Context)
		fn     func(*gin.Context)
	}{
		{name: "user usage", target: "/usage?page=1&page_size=10", setup: func(c *gin.Context) { c.Set("user_id", 7) }, fn: usageHandler.UserUsage},
		{name: "scoped user usage", target: "/usage?page=1&page_size=10", setup: func(c *gin.Context) {
			c.Set("user_id", 7)
			c.Set(middleware.CtxKeyAPIKeyID, 99)
		}, fn: usageHandler.UserUsage},
		{name: "user stats", target: "/usage/stats", setup: func(c *gin.Context) { c.Set("user_id", 7) }, fn: usageHandler.UserUsageStats},
		{name: "user trend", target: "/usage/trend?granularity=day&tz=UTC", setup: func(c *gin.Context) { c.Set("user_id", 7) }, fn: usageHandler.UserUsageTrend},
		{name: "admin usage", target: "/admin/usage?page=1&page_size=10", fn: usageHandler.AdminUsage},
		{name: "admin stats", target: "/admin/usage/stats?group_by=model,user", fn: usageHandler.AdminUsageStats},
		{name: "admin trend", target: "/admin/usage/trend?granularity=day&tz=UTC", fn: usageHandler.AdminUsageTrend},
	}
	for _, tt := range usageCases {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(http.MethodGet, tt.target, "", nil, tt.setup, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
		})
	}
}
