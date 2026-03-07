// Package handler 提供 HTTP API 处理器
package handler

import (
	"io"
	"strings"

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

	// 从 Manager 获取运行时元信息，建立 name → meta 的映射
	allMeta := h.manager.GetAllPluginMeta()
	metaMap := make(map[string]plugin.PluginMeta, len(allMeta))
	for _, m := range allMeta {
		metaMap[m.Name] = m
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
		// 填充运行时元信息
		if m, ok := metaMap[p.Name]; ok {
			for _, at := range m.AccountTypes {
				resp.AccountTypes = append(resp.AccountTypes, dto.AccountTypeResp{
					Key: at.Key, Label: at.Label, Description: at.Description,
				})
			}
			for _, fp := range m.FrontendPages {
				resp.FrontendPages = append(resp.FrontendPages, dto.FrontendPageResp{
					Path: fp.Path, Title: fp.Title, Icon: fp.Icon, Description: fp.Description,
				})
			}
			resp.HasWebAssets = m.HasWebAssets
		}
		list = append(list, resp)
	}

	response.Success(c, response.PagedData(list, int64(len(list)), 1, len(list)))
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

// UploadPlugin 上传安装插件
// POST /api/v1/admin/plugins/upload
func (h *PluginHandler) UploadPlugin(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请上传插件文件")
		return
	}

	// 读取文件内容
	f, err := file.Open()
	if err != nil {
		response.InternalError(c, "读取上传文件失败")
		return
	}
	defer f.Close()

	binary, err := io.ReadAll(f)
	if err != nil {
		response.InternalError(c, "读取文件内容失败")
		return
	}

	// 插件名：优先使用表单字段，否则用文件名
	name := c.PostForm("name")
	if name == "" {
		name = strings.TrimSuffix(file.Filename, ".exe")
	}

	if err := h.manager.InstallFromBinary(c.Request.Context(), name, binary); err != nil {
		response.InternalError(c, "安装插件失败: "+err.Error())
		return
	}

	response.Success(c, nil)
}

// InstallFromGithub 从 GitHub Release 安装插件
// POST /api/v1/admin/plugins/install-github
func (h *PluginHandler) InstallFromGithub(c *gin.Context) {
	var req dto.InstallGithubReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数无效")
		return
	}

	if err := h.manager.InstallFromGithub(c.Request.Context(), req.Repo); err != nil {
		response.InternalError(c, "从 GitHub 安装失败: "+err.Error())
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

// PluginStatus 获取插件运行状态
// GET /api/v1/admin/plugins/:id/status
func (h *PluginHandler) PluginStatus(c *gin.Context) {
	var param dto.IDParam
	if err := c.ShouldBindUri(&param); err != nil {
		response.BadRequest(c, "插件 ID 无效")
		return
	}

	p, err := h.db.Plugin.Get(c.Request.Context(), int(param.ID))
	if err != nil {
		response.NotFound(c, "插件不存在")
		return
	}

	running := h.manager.IsRunning(int(param.ID))

	response.Success(c, map[string]interface{}{
		"id":      p.ID,
		"name":    p.Name,
		"status":  string(p.Status),
		"running": running,
	})
}

// ListMarketplace 列出市场可用插件
// GET /api/v1/admin/marketplace/plugins
func (h *PluginHandler) ListMarketplace(c *gin.Context) {
	plugins, err := h.marketplace.ListAvailable(c.Request.Context())
	if err != nil {
		response.InternalError(c, "查询插件市场失败")
		return
	}

	list := make([]dto.MarketplacePluginResp, 0, len(plugins))
	for _, p := range plugins {
		list = append(list, dto.MarketplacePluginResp{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Author:      p.Author,
			Type:        p.Type,
		})
	}

	response.Success(c, response.PagedData(list, int64(len(list)), 1, len(list)))
}
