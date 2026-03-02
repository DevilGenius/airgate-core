// Package plugin 提供插件生命周期管理、市场和请求转发
package plugin

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/DouDOU-start/airgate-core/ent"
	entplugin "github.com/DouDOU-start/airgate-core/ent/plugin"
)

// Manager 插件管理器
// 负责插件的安装、卸载、启停和配置管理
// 真正的 gRPC 插件通信较复杂，当前为占位实现
type Manager struct {
	db        *ent.Client
	pluginDir string // 插件二进制目录
}

// NewManager 创建插件管理器
func NewManager(db *ent.Client, pluginDir string) *Manager {
	return &Manager{
		db:        db,
		pluginDir: pluginDir,
	}
}

// ListInstalled 列出已安装的插件
func (m *Manager) ListInstalled(ctx context.Context) ([]*ent.Plugin, error) {
	return m.db.Plugin.Query().All(ctx)
}

// Install 安装插件（占位实现）
// 实际逻辑需要下载二进制、校验签名等，后续完善
func (m *Manager) Install(ctx context.Context, name, source, version string) error {
	// 检查是否已安装
	exists, err := m.db.Plugin.Query().
		Where(entplugin.NameEQ(name)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("查询插件状态失败: %w", err)
	}
	if exists {
		return fmt.Errorf("插件 %s 已安装", name)
	}

	// 创建插件记录
	_, err = m.db.Plugin.Create().
		SetName(name).
		SetVersion(version).
		SetStatus(entplugin.StatusInstalled).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("创建插件记录失败: %w", err)
	}

	slog.Info("插件安装成功", "name", name, "version", version, "source", source)
	return nil
}

// Uninstall 卸载插件
func (m *Manager) Uninstall(ctx context.Context, id int) error {
	return m.db.Plugin.DeleteOneID(id).Exec(ctx)
}

// Enable 启用插件
func (m *Manager) Enable(ctx context.Context, id int) error {
	return m.db.Plugin.UpdateOneID(id).
		SetStatus(entplugin.StatusEnabled).
		Exec(ctx)
}

// Disable 禁用插件
func (m *Manager) Disable(ctx context.Context, id int) error {
	return m.db.Plugin.UpdateOneID(id).
		SetStatus(entplugin.StatusDisabled).
		Exec(ctx)
}

// UpdateConfig 更新插件配置
func (m *Manager) UpdateConfig(ctx context.Context, id int, config map[string]interface{}) error {
	return m.db.Plugin.UpdateOneID(id).
		SetConfig(config).
		Exec(ctx)
}
