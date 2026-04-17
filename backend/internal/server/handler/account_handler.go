package handler

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"

	appaccount "github.com/DouDOU-start/airgate-core/internal/app/account"
)

// AccountHandler 上游账号管理 Handler。
type AccountHandler struct {
	service *appaccount.Service
}

// NewAccountHandler 创建 AccountHandler。
func NewAccountHandler(service *appaccount.Service) *AccountHandler {
	return &AccountHandler{service: service}
}

func parseAccountID(raw string) (int, error) {
	return strconv.Atoi(raw)
}

func parseOptionalInt(raw string) *int {
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

// parseIDList 解析逗号分隔的整数列表（如 "1,2,3"），忽略空项与非法项。
func parseIDList(raw string) []int {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, err := strconv.Atoi(p); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}

func (h *AccountHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appaccount.ErrAccountNotFound):
		return 404, err.Error()
	case errors.Is(err, appaccount.ErrPluginNotFound):
		return 500, err.Error()
	case errors.Is(err, appaccount.ErrReauthRequired):
		// 这里的"需要重新授权"说的是**上游账号**（OAuth）的凭证失效，不是当前
		// 登录用户的 session。绝对不能返回 401——前端 HTTP 客户端有全局拦截，
		// 看到 401 会把当前管理员踹出登录页。用 422 语义最贴切：请求合法但
		// 因账号状态无法处理。
		return 422, err.Error()
	case errors.Is(err, appaccount.ErrModelRequired),
		errors.Is(err, appaccount.ErrQuotaRefreshUnsupported),
		errors.Is(err, appaccount.ErrInvalidDateRange):
		return 400, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}
