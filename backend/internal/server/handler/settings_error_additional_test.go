package handler

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
)

func TestSettingsHandlerServiceErrorBranches(t *testing.T) {
	settingsService := appsettings.NewService(handlerSettingsRepoStub{err: errors.New("settings down")})
	handler := NewSettingsHandler(settingsService, strings.Repeat("a", 64), nil)

	w := invokeHandlerForValidation(http.MethodGet, "/settings/public", "", nil, nil, handler.GetPublicSettings)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"data":{}`) {
		t.Fatalf("public settings on service error body = %s", w.Body.String())
	}

	cases := []struct {
		name   string
		method string
		target string
		body   string
		fn     func(*gin.Context)
	}{
		{name: "get settings", method: http.MethodGet, target: "/settings?group=site", fn: handler.GetSettings},
		{name: "update settings", method: http.MethodPut, target: "/settings", body: `{"settings":[{"key":"site_name","value":"AirGate","group":"site"}]}`, fn: handler.UpdateSettings},
		{name: "get admin api key", method: http.MethodGet, target: "/settings/admin-api-key", fn: handler.GetAdminAPIKey},
		{name: "generate admin api key", method: http.MethodPost, target: "/settings/admin-api-key", fn: handler.GenerateAdminAPIKey},
		{name: "delete admin api key", method: http.MethodDelete, target: "/settings/admin-api-key", fn: handler.DeleteAdminAPIKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tc.method, tc.target, tc.body, nil, nil, tc.fn)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("%s status = %d body=%s", tc.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestSettingsHandlerGenerateAdminAPIKeyEncryptionError(t *testing.T) {
	handler := NewSettingsHandler(appsettings.NewService(handlerSettingsRepoStub{}), "short-secret", nil)

	w := invokeHandlerForValidation(http.MethodPost, "/settings/admin-api-key", "", nil, nil, handler.GenerateAdminAPIKey)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "加密密钥失败") {
		t.Fatalf("generate admin api key encryption error status=%d body=%s", w.Code, w.Body.String())
	}
}
