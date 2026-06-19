package handler

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
)

// UserHandler 用户管理 Handler。
type UserHandler struct {
	service         *appuser.Service
	settingsService *appsettings.Service
	scheduler       *scheduler.Scheduler
}

// NewUserHandler 创建 UserHandler。
func NewUserHandler(service *appuser.Service, settingsService *appsettings.Service, sched *scheduler.Scheduler) *UserHandler {
	return &UserHandler{service: service, settingsService: settingsService, scheduler: sched}
}

func parseUserID(raw string) (int, error) {
	return strconv.Atoi(raw)
}

func (h *UserHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appuser.ErrUserNotFound):
		return 404, err.Error()
	case errors.Is(err, appuser.ErrEmailAlreadyExists),
		errors.Is(err, appuser.ErrOldPasswordMismatch),
		errors.Is(err, appuser.ErrInsufficientBalance),
		errors.Is(err, appuser.ErrInvalidBalanceAction),
		errors.Is(err, appuser.ErrDeleteAdminForbidden):
		return 400, err.Error()
	case errors.Is(err, appuser.ErrInvalidRateMultiplier):
		return 400, appuser.ErrInvalidRateMultiplier.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}

func (h *UserHandler) refreshRouteGraphUser(ctx context.Context, userID int) {
	if h.scheduler != nil {
		h.scheduler.RefreshRouteGraphUser(ctx, userID)
	}
}

func (h *UserHandler) removeRouteGraphUser(userID int) {
	if h.scheduler != nil {
		h.scheduler.RemoveRouteGraphUser(userID)
	}
}
