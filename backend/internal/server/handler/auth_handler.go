// Package handler 提供 HTTP 请求处理器。
package handler

import (
	"errors"
	"log/slog"

	"github.com/DouDOU-start/airgate-core/ent"
	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	appuser "github.com/DouDOU-start/airgate-core/internal/app/user"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/infra/mailer"
)

// AuthHandler 认证相关 Handler。
type AuthHandler struct {
	service         *appauth.Service
	settingsService *appsettings.Service
	userService     *appuser.Service
	codeStore       *mailer.VerifyCodeStore
	db              *ent.Client
	jwtMgr          *auth.JWTManager
}

// NewAuthHandler 创建认证 Handler。
func NewAuthHandler(service *appauth.Service, settingsService *appsettings.Service, userService *appuser.Service, codeStore *mailer.VerifyCodeStore, db *ent.Client, jwtMgr *auth.JWTManager) *AuthHandler {
	return &AuthHandler{
		service:         service,
		settingsService: settingsService,
		userService:     userService,
		codeStore:       codeStore,
		db:              db,
		jwtMgr:          jwtMgr,
	}
}

func (h *AuthHandler) handleLoginError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, appauth.ErrInvalidCredentials):
		return 401, err.Error(), true
	case errors.Is(err, appauth.ErrUserDisabled):
		return 403, err.Error(), true
	default:
		slog.Error("登录失败", "error", err)
		return 500, "登录失败", false
	}
}

func (h *AuthHandler) handleRegisterError(err error) (int, string) {
	switch {
	case errors.Is(err, appauth.ErrEmailAlreadyExists):
		return 400, err.Error()
	default:
		slog.Error("注册失败", "error", err)
		return 500, "注册失败"
	}
}
