package handler

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	appgroup "github.com/DevilGenius/airgate-core/internal/app/group"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
)

// GroupHandler 分组管理 Handler。
type GroupHandler struct {
	service   *appgroup.Service
	scheduler *scheduler.Scheduler
}

// NewGroupHandler 创建 GroupHandler。
func NewGroupHandler(service *appgroup.Service, sched *scheduler.Scheduler) *GroupHandler {
	return &GroupHandler{service: service, scheduler: sched}
}

func parseGroupID(raw string) (int, error) {
	return strconv.Atoi(raw)
}

func (h *GroupHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appgroup.ErrGroupNotFound):
		return 404, err.Error()
	case errors.Is(err, appgroup.ErrGroupHasSubscriptions):
		return 400, err.Error()
	case errors.Is(err, appgroup.ErrSourceGroupPlatformMismatch):
		return 400, err.Error()
	case errors.Is(err, appgroup.ErrInvalidRateMultiplier):
		return 400, appgroup.ErrInvalidRateMultiplier.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}

func (h *GroupHandler) refreshRouteGraphGroup(ctx context.Context, groupID int) {
	if h.scheduler != nil {
		h.scheduler.RefreshRouteGraphGroup(ctx, groupID)
	}
}

func (h *GroupHandler) removeRouteGraphGroup(groupID int) {
	if h.scheduler != nil {
		h.scheduler.RemoveRouteGraphGroup(groupID)
	}
}
