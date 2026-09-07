package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/reporting"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

// UsageHandler 使用记录 Handler。
type UsageHandler struct {
	service *appusage.Service
}

// NewUsageHandler 创建 UsageHandler。
func NewUsageHandler(service *appusage.Service) *UsageHandler {
	return &UsageHandler{service: service}
}

func handleUsageError(logMessage string, err error) {
	slog.Error(logMessage, "error", err)
}

func validateUsageModelFilter(c *gin.Context, raw string) bool {
	if err := reporting.ValidateDates(c.Query("start_date"), c.Query("end_date"), c.Query("tz")); err != nil {
		response.BadRequest(c, err.Error())
		return false
	}
	if err := appusage.ValidateModelFilter(raw); err != nil {
		response.BadRequest(c, err.Error())
		return false
	}
	return true
}

func handleReportingError(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, reporting.ErrInvalidRange), errors.Is(err, reporting.ErrTooManyGroups):
		response.BadRequest(c, err.Error())
	case errors.Is(err, reporting.ErrBusy):
		c.Header("Retry-After", "1")
		response.Error(c, http.StatusServiceUnavailable, 503, err.Error())
	default:
		return false
	}
	return true
}
