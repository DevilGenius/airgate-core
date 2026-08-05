package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/accountimportdsl"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

func (h *AccountHandler) GetImportConfig(c *gin.Context) {
	raw, err := h.accountImportDSL(c.Request.Context())
	if err != nil {
		slog.Error("account_import_config_load_failed", "error", err)
		response.InternalError(c, "读取导入配置失败")
		return
	}
	response.Success(c, dto.AccountImportConfigResp{DSL: raw})
}

func (h *AccountHandler) UpdateImportConfig(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, accountimportdsl.MaxConfigBytes+(16<<10))
	var req dto.UpdateAccountImportConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}
	raw := strings.TrimSpace(req.DSL)
	if len(raw) > accountimportdsl.MaxConfigBytes {
		response.BadRequest(c, "导入配置内容过大")
		return
	}
	if _, err := accountimportdsl.Parse(raw); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if h.settingsService == nil {
		response.InternalError(c, "设置服务不可用")
		return
	}
	if err := h.settingsService.Update(c.Request.Context(), []appsettings.ItemInput{{
		Key:   accountimportdsl.SettingKey,
		Value: raw,
		Group: accountimportdsl.SettingGroup,
	}}); err != nil {
		slog.Error("account_import_config_save_failed", "error", err)
		response.InternalError(c, "保存导入配置失败")
		return
	}
	response.Success(c, dto.AccountImportConfigResp{DSL: raw})
}
