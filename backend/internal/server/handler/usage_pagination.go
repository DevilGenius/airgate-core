package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

func (h *UsageHandler) UserUsagePagination(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}
	h.usagePagination(c, int64(userID))
}

func (h *UsageHandler) AdminUsagePagination(c *gin.Context) { h.usagePagination(c, 0) }

func (h *UsageHandler) usagePagination(c *gin.Context, userID int64) {
	var query dto.UsageQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.BindError(c, err)
		return
	}
	filter := appusage.ListFilter{UserID: query.UserID, APIKeyID: query.APIKeyID, AccountID: query.AccountID, GroupID: query.GroupID, Platform: query.Platform, Model: query.Model, StartDate: query.StartDate, EndDate: query.EndDate, TZ: c.Query("tz")}
	if userID > 0 {
		// Match UserUsage exactly: never trust a query-supplied owner or key scope.
		filter.UserID = &userID
		if keyID := scopedAPIKeyID(c); keyID > 0 {
			filter.APIKeyID = &keyID
			filter.ScopedToKey = true
		}
	} else {
		filter.AccountSearch = query.Account
	}
	info, err := h.service.Pagination(c.Request.Context(), userID, filter, query.RefreshPagination)
	if err != nil {
		if handleUsagePaginationError(c, err) {
			return
		}
		handleUsageError("准备使用记录页数失败", err)
		response.InternalError(c, "页数暂时无法准备，请稍后重试")
		return
	}
	response.Success(c, info)
}

func handleUsagePaginationError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, appusage.ErrPageIndexExpired):
		response.Error(c, http.StatusConflict, 40901, "分页记录已过期或发生变更，请刷新后重新跳页")
	case errors.Is(err, appusage.ErrInvalidListFilter):
		response.BadRequest(c, "使用记录筛选参数无效，请检查日期范围和编号")
	default:
		return false
	}
	return true
}
