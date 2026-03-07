// Package plugin 提供插件生命周期管理、市场和请求转发
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	sdk "github.com/DouDOU-start/airgate-sdk"
	sdkgrpc "github.com/DouDOU-start/airgate-sdk/grpc"
	"github.com/DouDOU-start/airgate-sdk/shared"
	"github.com/DouDOU-start/airgate-core/ent"
	entplugin "github.com/DouDOU-start/airgate-core/ent/plugin"
	"github.com/DouDOU-start/airgate-core/internal/billing"
	goplugin "github.com/hashicorp/go-plugin"
)

// PluginInstance 运行中的插件实例
type PluginInstance struct {
	ID       int
	Name     string
	Platform string
	Type     string // "gateway", "payment", "extension"
	Client   *goplugin.Client
	Gateway  *sdkgrpc.SimpleGatewayGRPCClient
}

// Manager 插件管理器
// 负责插件的安装、卸载、启停、进程管理和 gRPC 通信
type Manager struct {
	db        *ent.Client
	pluginDir string // 插件二进制目录
	priceMgr  *billing.PriceManager

	mu        sync.RWMutex
	instances map[int]*PluginInstance // pluginID → 运行实例

	// 缓存：插件启动时从 gRPC 获取并缓存
	modelCache        map[string][]sdk.ModelInfo       // platform → models
	routeCache        map[string][]sdk.RouteDefinition // pluginID(name) → routes
	credCache         map[string][]sdk.CredentialField // platform → credential fields
	accountTypeCache  map[string][]sdk.AccountType     // platform → account types
	frontendPageCache map[string][]sdk.FrontendPage    // pluginName → 前端页面声明
}

// NewManager 创建插件管理器
func NewManager(db *ent.Client, pluginDir string, priceMgr *billing.PriceManager) *Manager {
	return &Manager{
		db:         db,
		pluginDir:  pluginDir,
		priceMgr:   priceMgr,
		instances:        make(map[int]*PluginInstance),
		modelCache:        make(map[string][]sdk.ModelInfo),
		routeCache:        make(map[string][]sdk.RouteDefinition),
		credCache:         make(map[string][]sdk.CredentialField),
		accountTypeCache:  make(map[string][]sdk.AccountType),
		frontendPageCache: make(map[string][]sdk.FrontendPage),
	}
}

// LoadAll 启动时自动发现并加载插件
// 1. 扫描 pluginDir 下的子目录，发现可执行二进制则自动注册到数据库
// 2. 加载所有 status=enabled 的插件
func (m *Manager) LoadAll(ctx context.Context) error {
	// 自动发现目录中的插件
	if err := m.discoverPlugins(ctx); err != nil {
		slog.Warn("自动发现插件失败", "error", err)
	}

	plugins, err := m.db.Plugin.Query().
		Where(entplugin.StatusEQ(entplugin.StatusEnabled)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("查询已启用插件失败: %w", err)
	}

	for _, p := range plugins {
		if err := m.startPlugin(ctx, p); err != nil {
			slog.Error("加载插件失败", "name", p.Name, "error", err)
			continue
		}
		slog.Info("插件加载成功", "name", p.Name, "platform", p.Platform)
	}
	return nil
}

// discoverPlugins 扫描插件目录，自动注册新发现的插件
// 目录结构：{pluginDir}/{name}/{name}（可执行二进制）
func (m *Manager) discoverPlugins(ctx context.Context) error {
	entries, err := os.ReadDir(m.pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 插件目录不存在，跳过
		}
		return fmt.Errorf("读取插件目录失败: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		binaryPath := filepath.Join(m.pluginDir, name, name)

		// 检查二进制是否存在且可执行
		info, err := os.Stat(binaryPath)
		if err != nil || info.IsDir() {
			continue
		}

		// 检查数据库中是否已有记录
		exists, err := m.db.Plugin.Query().
			Where(entplugin.NameEQ(name)).
			Exist(ctx)
		if err != nil {
			slog.Warn("查询插件记录失败", "name", name, "error", err)
			continue
		}
		if exists {
			continue // 已注册，跳过
		}

		// 自动注册并启用
		_, err = m.db.Plugin.Create().
			SetName(name).
			SetVersion("0.0.0").
			SetType(entplugin.TypeGateway).
			SetStatus(entplugin.StatusEnabled).
			SetBinaryPath(binaryPath).
			Save(ctx)
		if err != nil {
			slog.Warn("自动注册插件失败", "name", name, "error", err)
			continue
		}
		slog.Info("自动发现并注册插件", "name", name, "binary", binaryPath)
	}
	return nil
}

// startPlugin 启动插件子进程并建立 gRPC 连接
func (m *Manager) startPlugin(ctx context.Context, p *ent.Plugin) error {
	// 确定二进制路径
	binaryPath := p.BinaryPath
	if binaryPath == "" {
		binaryPath = filepath.Join(m.pluginDir, p.Name, p.Name)
	}

	// 创建 go-plugin 客户端
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: shared.Handshake,
		Plugins: goplugin.PluginSet{
			shared.PluginKeySimpleGateway: &sdkgrpc.SimpleGatewayGRPCPlugin{},
		},
		Cmd:              exec.Command(binaryPath),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
	})

	// 建立 RPC 连接
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return fmt.Errorf("连接插件进程失败: %w", err)
	}

	// 获取 SimpleGateway 接口
	raw, err := rpcClient.Dispense(shared.PluginKeySimpleGateway)
	if err != nil {
		client.Kill()
		return fmt.Errorf("获取插件接口失败: %w", err)
	}

	gateway, ok := raw.(*sdkgrpc.SimpleGatewayGRPCClient)
	if !ok {
		client.Kill()
		return fmt.Errorf("插件类型断言失败")
	}

	// 初始化插件：传入配置
	pluginCtx := newCorePluginContext(p.Config, p.Name)
	if err := gateway.Init(pluginCtx); err != nil {
		client.Kill()
		return fmt.Errorf("初始化插件失败: %w", err)
	}

	// 启动插件
	if err := gateway.Start(ctx); err != nil {
		client.Kill()
		return fmt.Errorf("启动插件失败: %w", err)
	}

	// 获取插件元信息并缓存
	platform := gateway.Platform()
	models := gateway.Models()
	routes := gateway.Routes()
	info := gateway.Info()

	instance := &PluginInstance{
		ID:       p.ID,
		Name:     p.Name,
		Platform: platform,
		Type:     string(p.Type),
		Client:   client,
		Gateway:  gateway,
	}

	m.mu.Lock()
	m.instances[p.ID] = instance
	m.modelCache[platform] = models
	m.routeCache[p.Name] = routes
	m.credCache[platform] = info.CredentialFields
	m.accountTypeCache[platform] = info.AccountTypes
	// 缓存前端页面声明
	if len(info.FrontendPages) > 0 {
		m.frontendPageCache[p.Name] = info.FrontendPages
	}
	m.mu.Unlock()

	// 提取前端静态资源
	assets, err := gateway.GetWebAssets()
	if err != nil {
		slog.Warn("获取插件前端资源失败", "plugin", p.Name, "error", err)
	} else if len(assets) > 0 {
		assetsDir := filepath.Join(m.pluginDir, p.Name, "assets")
		if err := extractWebAssets(assetsDir, assets); err != nil {
			slog.Warn("写入插件前端资源失败", "plugin", p.Name, "error", err)
		} else {
			slog.Info("已提取插件前端资源", "plugin", p.Name, "files", len(assets))
		}
	}

	// 注册模型价格到 PriceManager
	if m.priceMgr != nil {
		for _, model := range models {
			m.priceMgr.SetPrice(platform, model.ID, billing.ModelPrice{
				InputPerToken:  model.InputPrice / 1000000,  // 每百万 → 每个
				OutputPerToken: model.OutputPrice / 1000000,
				CachePerToken:  model.CachePrice / 1000000,
			})
		}
	}

	// 更新 DB 中的 platform 字段（插件首次启动后回填）
	if p.Platform != platform {
		_ = m.db.Plugin.UpdateOneID(p.ID).
			SetPlatform(platform).
			Exec(ctx)
	}

	return nil
}

// stopPlugin 停止插件进程
func (m *Manager) stopPlugin(pluginID int) error {
	m.mu.Lock()
	inst, ok := m.instances[pluginID]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.instances, pluginID)
	// 清理缓存
	delete(m.modelCache, inst.Platform)
	delete(m.routeCache, inst.Name)
	delete(m.credCache, inst.Platform)
	delete(m.accountTypeCache, inst.Platform)
	delete(m.frontendPageCache, inst.Name)
	m.mu.Unlock()

	// 停止插件
	if inst.Gateway != nil {
		_ = inst.Gateway.Stop(context.Background())
	}
	if inst.Client != nil {
		inst.Client.Kill()
	}

	slog.Info("插件已停止", "name", inst.Name)
	return nil
}

// StopAll 停止所有插件
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.RLock()
	ids := make([]int, 0, len(m.instances))
	for id := range m.instances {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		if err := m.stopPlugin(id); err != nil {
			slog.Error("停止插件失败", "id", id, "error", err)
		}
	}
}

// ListInstalled 列出已安装的插件
func (m *Manager) ListInstalled(ctx context.Context) ([]*ent.Plugin, error) {
	return m.db.Plugin.Query().All(ctx)
}

// Install 安装插件
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
		SetBinaryPath(filepath.Join(m.pluginDir, name, name)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("创建插件记录失败: %w", err)
	}

	slog.Info("插件安装成功", "name", name, "version", version, "source", source)
	return nil
}

// InstallFromBinary 从二进制数据安装插件
// 写入文件、探测真实插件名、创建/更新 DB 记录、自动启动
func (m *Manager) InstallFromBinary(ctx context.Context, name string, binary []byte) error {
	// 先写入临时目录，探测插件真实名称
	realName, err := m.probePluginName(name, binary)
	if err != nil {
		slog.Warn("探测插件名称失败，使用传入名称", "name", name, "error", err)
		realName = name
	}

	// 写入最终目录
	targetDir := filepath.Join(m.pluginDir, realName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("创建插件目录失败: %w", err)
	}
	binaryPath := filepath.Join(targetDir, realName)
	if err := os.WriteFile(binaryPath, binary, 0755); err != nil {
		return fmt.Errorf("写入插件二进制失败: %w", err)
	}

	// 查找已有记录
	existing, err := m.db.Plugin.Query().
		Where(entplugin.NameEQ(realName)).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("查询插件记录失败: %w", err)
	}

	var pluginID int
	if existing != nil {
		// 已存在：停止旧进程，更新记录
		_ = m.stopPlugin(existing.ID)
		if err := m.db.Plugin.UpdateOneID(existing.ID).
			SetStatus(entplugin.StatusEnabled).
			SetBinaryPath(binaryPath).
			Exec(ctx); err != nil {
			return fmt.Errorf("更新插件记录失败: %w", err)
		}
		pluginID = existing.ID
	} else {
		// 不存在：创建新记录
		p, err := m.db.Plugin.Create().
			SetName(realName).
			SetVersion("0.0.0").
			SetType(entplugin.TypeGateway).
			SetStatus(entplugin.StatusEnabled).
			SetBinaryPath(binaryPath).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("创建插件记录失败: %w", err)
		}
		pluginID = p.ID
	}

	// 启动插件
	p, err := m.db.Plugin.Get(ctx, pluginID)
	if err != nil {
		return fmt.Errorf("查询插件失败: %w", err)
	}
	if err := m.startPlugin(ctx, p); err != nil {
		return fmt.Errorf("启动插件失败: %w", err)
	}

	slog.Info("插件从二进制安装成功", "name", realName)
	return nil
}

// probePluginName 临时启动插件二进制，通过 gRPC 获取真实插件 ID
func (m *Manager) probePluginName(fallbackName string, binary []byte) (string, error) {
	// 写入临时文件
	tmpDir, err := os.MkdirTemp("", "airgate-probe-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpBinary := filepath.Join(tmpDir, fallbackName)
	if err := os.WriteFile(tmpBinary, binary, 0755); err != nil {
		return "", fmt.Errorf("写入临时二进制失败: %w", err)
	}

	// 启动临时 go-plugin 客户端
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: shared.Handshake,
		Plugins: goplugin.PluginSet{
			shared.PluginKeySimpleGateway: &sdkgrpc.SimpleGatewayGRPCPlugin{},
		},
		Cmd:              exec.Command(tmpBinary),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
	})
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		return "", fmt.Errorf("连接探测进程失败: %w", err)
	}

	raw, err := rpcClient.Dispense(shared.PluginKeySimpleGateway)
	if err != nil {
		return "", fmt.Errorf("获取探测接口失败: %w", err)
	}

	gateway, ok := raw.(*sdkgrpc.SimpleGatewayGRPCClient)
	if !ok {
		return "", fmt.Errorf("探测类型断言失败")
	}

	info := gateway.Info()
	if info.ID == "" {
		return fallbackName, nil
	}
	return info.ID, nil
}

// InstallFromGithub 从 GitHub Release 下载并安装插件
func (m *Manager) InstallFromGithub(ctx context.Context, repo string) error {
	owner, repoName, err := parseGithubRepo(repo)
	if err != nil {
		return err
	}

	// 查询最新 Release
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repoName)
	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("仓库 %s/%s 不存在或没有 Release", owner, repoName)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API 返回状态码 %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("解析 Release 数据失败: %w", err)
	}

	// 匹配当前系统的二进制
	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH
	var downloadURL string
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, targetOS) && strings.Contains(name, targetArch) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("未找到适配 %s/%s 的二进制文件，Release: %s", targetOS, targetArch, release.TagName)
	}

	// 下载二进制
	dlReq, _ := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		return fmt.Errorf("下载插件失败: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回状态码 %d", dlResp.StatusCode)
	}

	binary, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return fmt.Errorf("读取下载内容失败: %w", err)
	}

	// 用仓库名作为插件名
	return m.InstallFromBinary(ctx, repoName, binary)
}

// githubRelease GitHub Release API 响应
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset GitHub Release Asset
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// parseGithubRepo 解析 GitHub 仓库地址
// 支持格式：owner/repo 或 https://github.com/owner/repo
func parseGithubRepo(repo string) (owner, name string, err error) {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), "/")
	repo = strings.TrimSuffix(repo, ".git")

	// 完整 URL 格式
	if strings.Contains(repo, "github.com") {
		parts := strings.Split(repo, "github.com/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("无效的 GitHub 地址: %s", repo)
		}
		repo = parts[1]
	}

	// owner/repo 格式
	segments := strings.Split(repo, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("无效的仓库格式，请使用 owner/repo 格式")
	}
	return segments[0], segments[1], nil
}

// Uninstall 卸载插件
func (m *Manager) Uninstall(ctx context.Context, id int) error {
	// 先停止插件
	_ = m.stopPlugin(id)
	return m.db.Plugin.DeleteOneID(id).Exec(ctx)
}

// Enable 启用插件并启动进程
func (m *Manager) Enable(ctx context.Context, id int) error {
	// 更新 DB 状态
	if err := m.db.Plugin.UpdateOneID(id).
		SetStatus(entplugin.StatusEnabled).
		Exec(ctx); err != nil {
		return fmt.Errorf("更新插件状态失败: %w", err)
	}

	// 启动插件进程
	p, err := m.db.Plugin.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("查询插件失败: %w", err)
	}

	if err := m.startPlugin(ctx, p); err != nil {
		// 启动失败回滚状态
		_ = m.db.Plugin.UpdateOneID(id).
			SetStatus(entplugin.StatusInstalled).
			Exec(ctx)
		return fmt.Errorf("启动插件失败: %w", err)
	}

	return nil
}

// Disable 禁用插件并停止进程
func (m *Manager) Disable(ctx context.Context, id int) error {
	// 停止插件进程
	_ = m.stopPlugin(id)

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

// GetPluginByPlatform 根据平台查找运行中的插件实例
func (m *Manager) GetPluginByPlatform(platform string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, inst := range m.instances {
		if inst.Platform == platform {
			return inst
		}
	}
	return nil
}

// GetInstance 获取插件实例
func (m *Manager) GetInstance(pluginID int) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instances[pluginID]
}

// GetCredentialFields 获取指定平台的凭证字段声明
func (m *Manager) GetCredentialFields(platform string) []sdk.CredentialField {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.credCache[platform]
}

// GetAccountTypes 获取指定平台的账号类型声明
func (m *Manager) GetAccountTypes(platform string) []sdk.AccountType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accountTypeCache[platform]
}

// GetRoutes 获取指定插件的路由声明
func (m *Manager) GetRoutes(pluginName string) []sdk.RouteDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routeCache[pluginName]
}

// GetAllRoutes 获取所有运行中插件的路由
func (m *Manager) GetAllRoutes() map[string][]sdk.RouteDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]sdk.RouteDefinition, len(m.routeCache))
	for k, v := range m.routeCache {
		result[k] = v
	}
	return result
}

// MatchPluginByRoute 根据请求方法和路径匹配插件
func (m *Manager) MatchPluginByRoute(method, path string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pluginName, routes := range m.routeCache {
		for _, route := range routes {
			if route.Method == method && route.Path == path {
				// 通过名称查找实例
				for _, inst := range m.instances {
					if inst.Name == pluginName {
						return inst
					}
				}
			}
		}
	}
	return nil
}

// MatchPluginByPathPrefix 根据路径前缀匹配插件
// 插件声明的路由路径用作匹配依据
func (m *Manager) MatchPluginByPathPrefix(path string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pluginName, routes := range m.routeCache {
		for _, route := range routes {
			// 检查请求路径是否以插件声明的路径开头
			if path == route.Path || len(path) > len(route.Path) && path[:len(route.Path)] == route.Path {
				for _, inst := range m.instances {
					if inst.Name == pluginName {
						return inst
					}
				}
			}
		}
	}
	return nil
}

// IsRunning 检查插件是否正在运行
func (m *Manager) IsRunning(pluginID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.instances[pluginID]
	return ok
}

// GetFrontendPages 获取插件声明的前端页面
func (m *Manager) GetFrontendPages(pluginName string) []sdk.FrontendPage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.frontendPageCache[pluginName]
}

// PluginMeta 插件运行时元信息
type PluginMeta struct {
	Name          string
	Platform      string
	AccountTypes  []sdk.AccountType
	FrontendPages []sdk.FrontendPage
	HasWebAssets  bool
}

// GetAllPluginMeta 获取所有运行中插件的元信息
func (m *Manager) GetAllPluginMeta() []PluginMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var metas []PluginMeta
	for _, inst := range m.instances {
		meta := PluginMeta{
			Name:     inst.Name,
			Platform: inst.Platform,
		}
		if types, ok := m.accountTypeCache[inst.Platform]; ok {
			meta.AccountTypes = types
		}
		if pages, ok := m.frontendPageCache[inst.Name]; ok {
			meta.FrontendPages = pages
		}
		// 检查前端资源目录是否存在
		assetsDir := filepath.Join(m.pluginDir, inst.Name, "assets")
		if _, err := os.Stat(assetsDir); err == nil {
			meta.HasWebAssets = true
		}
		metas = append(metas, meta)
	}
	return metas
}

// HasWebAssets 检查插件是否有前端资源
func (m *Manager) HasWebAssets(pluginName string) bool {
	assetsDir := filepath.Join(m.pluginDir, pluginName, "assets")
	_, err := os.Stat(assetsDir)
	return err == nil
}

// extractWebAssets 将前端资源写入指定目录
func extractWebAssets(dir string, assets map[string][]byte) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	for path, content := range assets {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("创建子目录失败: %w", err)
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return fmt.Errorf("写入文件 %s 失败: %w", path, err)
		}
	}
	return nil
}
