// Package handler 提供 HTTP API 处理器
package handler

import (
	"github.com/DouDOU-start/airgate-core/ent"
	entplugin "github.com/DouDOU-start/airgate-core/ent/plugin"
	"github.com/DouDOU-start/airgate-core/internal/plugin"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
	"github.com/gin-gonic/gin"
)

// PluginHandler 插件管理 API
type PluginHandler struct {
	db          *ent.Client
	manager     *plugin.Manager
	marketplace *plugin.Marketplace
}

// NewPluginHandler 创建插件管理 Handler
func NewPluginHandler(db *ent.Client, manager *plugin.Manager, marketplace *plugin.Marketplace) *PluginHandler {
	return &PluginHandler{
		db:          db,
		manager:     manager,
		marketplace: marketplace,
	}
}

// ListPlugins 获取已安装的插件列表
// GET /api/v1/admin/plugins
func (h *PluginHandler) ListPlugins(c *gin.Context) {
	plugins, err := h.db.Plugin.Query().
		Order(entplugin.ByCreatedAt()).
		All(c.Request.Context())
	if err != nil {
		response.InternalError(c, "查询插件列表失败")
		return
	}

	list := make([]dto.PluginResp, 0, len(plugins))
	for _, p := range plugins {
		resp := dto.PluginResp{
			ID:       int64(p.ID),
			Name:     p.Name,
			Platform: p.Platform,
			Version:  p.Version,
			Type:     string(p.Type),
			Status:   string(p.Status),
			Config:   p.Config,
			TimeMixin: dto.TimeMixin{
				CreatedAt: p.CreatedAt,
				UpdatedAt: p.UpdatedAt,
			},
		}
		list = append(list, resp)
	}

	response.Success(c, list)
}

// InstallPlugin 安装插件
// POST /api/v1/admin/plugins/install
func (h *PluginHandler) InstallPlugin(c *gin.Context) {
	var req dto.InstallPluginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}

	if err := h.manager.Install(c.Request.Context(), req.Name, req.Source, req.Version); err != nil {
		response.InternalError(c, "安装插件失败: "+err.Error())
		return
	}

	response.Success(c, nil)
}

// UninstallPlugin 卸载插件
// POST /api/v1/admin/plugins/:id/uninstall
func (h *PluginHandler) UninstallPlugin(c *gin.Context) {
	var param dto.IDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.BadRequest(c, "插件 ID 无效")
		return
	}

	if err := h.manager.Uninstall(c.Request.Context(), int(param.ID)); err != nil {
		response.InternalError(c, "卸载插件失败")
		return
	}

	response.Success(c, nil)
}

// EnablePlugin 启用插件
// POST /api/v1/admin/plugins/:id/enable
func (h *PluginHandler) EnablePlugin(c *gin.Context) {
	var param dto.IDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.BadRequest(c, "插件 ID 无效")
		return
	}

	if err := h.manager.Enable(c.Request.Context(), int(param.ID)); err != nil {
		response.InternalError(c, "启用插件失败: "+err.Error())
		return
	}

	response.Success(c, nil)
}

// DisablePlugin 停用插件
// POST /api/v1/admin/plugins/:id/disable
func (h *PluginHandler) DisablePlugin(c *gin.Context) {
	var param dto.IDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.BadRequest(c, "插件 ID 无效")
		return
	}

	if err := h.manager.Disable(c.Request.Context(), int(param.ID)); err != nil {
		response.InternalError(c, "停用插件失败: "+err.Error())
		return
	}

	response.Success(c, nil)
}

// UpdateConfig 更新插件配置
// PUT /api/v1/admin/plugins/:id/config
func (h *PluginHandler) UpdateConfig(c *gin.Context) {
	var param dto.IDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.BadRequest(c, "插件 ID 无效")
		return
	}

	var req dto.PluginConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}

	if err := h.manager.UpdateConfig(c.Request.Context(), int(param.ID), req.Config); err != nil {
		response.InternalError(c, "更新配置失败")
		return
	}

	response.Success(c, nil)
}
