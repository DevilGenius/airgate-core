package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	pluginent "github.com/DevilGenius/airgate-core/ent/plugin"
)

// GetPluginByPlatform 根据平台查找运行中的插件实例。
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

// GetInstance 获取插件实例。
func (m *Manager) GetInstance(name string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instances[m.resolveNameLocked(name)]
}

// GetCredentialFields 获取指定平台的凭证字段声明。
func (m *Manager) GetCredentialFields(platform string) []sdk.CredentialField {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneCredentialFields(m.credCache[platform])
}

// GetAccountTypes 获取指定平台的账号类型声明。
func (m *Manager) GetAccountTypes(platform string) []sdk.AccountType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneAccountTypes(m.accountTypeCache[platform])
}

// GetModels 获取指定平台的模型列表。
func (m *Manager) GetModels(platform string) []sdk.ModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneModels(m.modelCache[platform])
}

// FindPlatformByModel 根据模型 ID 反查所属平台。
func (m *Manager) FindPlatformByModel(modelID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for platform, models := range m.modelCache {
		for _, mi := range models {
			if mi.ID == modelID {
				return platform
			}
		}
	}
	return ""
}

// GetRoutes 获取指定插件的路由声明。
func (m *Manager) GetRoutes(pluginName string) []sdk.RouteDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneRoutes(m.routeCache[m.resolveNameLocked(pluginName)])
}

// GetAllRoutes 获取所有运行中插件的路由。
func (m *Manager) GetAllRoutes() map[string][]sdk.RouteDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]sdk.RouteDefinition, len(m.routeCache))
	for key, routes := range m.routeCache {
		result[key] = cloneRoutes(routes)
	}
	return result
}

// MatchPluginByRoute 根据请求方法和路径匹配插件。
func (m *Manager) MatchPluginByRoute(method, path string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pluginName, routes := range m.routeCache {
		for _, route := range routes {
			if route.Method == method && route.Path == path {
				if inst, ok := m.instances[pluginName]; ok {
					return inst
				}
			}
		}
	}
	return nil
}

// MatchPluginByPathPrefix 根据路径前缀匹配插件。
func (m *Manager) MatchPluginByPathPrefix(path string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pluginName, routes := range m.routeCache {
		for _, route := range routes {
			if matchRoutePath(route.Path, path) {
				if inst, ok := m.instances[pluginName]; ok {
					return inst
				}
			}
		}
	}
	return nil
}

// MatchPluginByPlatformAndPath 根据平台和路径匹配插件。
func (m *Manager) MatchPluginByPlatformAndPath(platform, path string) *PluginInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for pluginName, inst := range m.instances {
		if inst.Platform != platform {
			continue
		}
		for _, route := range m.routeCache[pluginName] {
			if matchRoutePath(route.Path, path) {
				return inst
			}
		}
	}
	return nil
}

func matchRoutePath(routePath, path string) bool {
	if routePath == "" {
		return false
	}
	if path == routePath {
		return true
	}
	if !strings.HasPrefix(path, routePath) {
		return false
	}
	if strings.HasSuffix(routePath, "/") {
		return true
	}
	return len(path) > len(routePath) && path[len(routePath)] == '/'
}

// IsRunning 检查插件是否正在运行。
func (m *Manager) IsRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.instances[m.resolveNameLocked(name)]
	return ok
}

// RunningCount 获取运行中的插件数量。
func (m *Manager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.instances)
}

// GetFrontendPages 获取插件声明的前端页面。
func (m *Manager) GetFrontendPages(pluginName string) []sdk.FrontendPage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneFrontendPages(m.frontendPageCache[m.resolveNameLocked(pluginName)])
}

// GetAllPluginMeta 获取所有运行中插件的元信息。
func (m *Manager) GetAllPluginMeta() []PluginMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metas := make([]PluginMeta, 0, len(m.instances))
	for _, inst := range m.instances {
		_, isDev := m.devPaths[inst.Name]
		meta := PluginMeta{
			Generation:         inst.Generation,
			UpdateState:        "active",
			Name:               inst.Name,
			DisplayName:        inst.DisplayName,
			Version:            inst.Version,
			Author:             inst.Author,
			Type:               inst.Type,
			Platform:           inst.Platform,
			InstructionPresets: inst.InstructionPresets,
			ConfigSchema:       cloneConfigSchema(inst.ConfigSchema),
			Metadata:           cloneMetadata(inst.Metadata),
			IsDev:              isDev,
		}
		if m.updates[inst.Name] != nil {
			meta.UpdateState = "preparing"
		}
		if retiring := m.retiring[inst.Name]; retiring != nil {
			meta.UpdateState = "draining"
			meta.DrainingRequests = retiring.activeRequestCount()
		}
		if !isDev {
			meta.BinarySHA256 = m.installedBinarySHA256Locked(inst)
			meta.CommitSHA = m.readInstallMetadataLocked(inst).CommitSHA
		}
		if types, ok := m.accountTypeCache[inst.Platform]; ok {
			meta.AccountTypes = cloneAccountTypes(types)
		}
		if pages, ok := m.frontendPageCache[inst.Name]; ok {
			meta.FrontendPages = cloneFrontendPages(pages)
		}
		if len(inst.frontendAssets) > 0 {
			meta.HasWebAssets = true
		}
		metas = append(metas, meta)
	}
	return metas
}

func (m *Manager) installedBinarySHA256Locked(inst *PluginInstance) string {
	if inst == nil || inst.Artifact == nil {
		return ""
	}
	return inst.Artifact.SHA256
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// GetPluginConfig 读取插件的当前配置（来自 DB 持久化）。
// 用于「编辑配置」UI 展示当前值。
func (m *Manager) GetPluginConfig(ctx context.Context, name string) (map[string]string, error) {
	if m.db == nil {
		return map[string]string{}, nil
	}
	resolved := m.resolveName(name)
	row, err := m.db.Plugin.Query().Where(pluginent.NameEQ(resolved)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("查询插件配置失败: %w", err)
	}
	out := make(map[string]string, len(row.Config))
	for k, v := range row.Config {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out, nil
}

// UpdatePluginConfig 把用户提交的配置写入 DB。
// 配置写入后由调用方负责热更新或重启插件使其生效。
// 当 Plugin 行不存在时（dev 插件场景）会自动创建一条占位记录。
func (m *Manager) UpdatePluginConfig(ctx context.Context, name string, config map[string]string) error {
	if m.db == nil {
		return fmt.Errorf("数据库未初始化")
	}
	resolved := m.resolveName(name)
	cfgJSON := make(map[string]interface{}, len(config))
	for k, v := range config {
		cfgJSON[k] = v
	}

	row, err := m.db.Plugin.Query().Where(pluginent.NameEQ(resolved)).Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return fmt.Errorf("查询插件失败: %w", err)
		}
		// 不存在则创建一条记录（dev / 未持久化的插件）
		inst := m.GetInstance(resolved)
		create := m.db.Plugin.Create().SetName(resolved).SetConfig(cfgJSON)
		if inst != nil {
			create = create.SetVersion(inst.Version).SetPlatform(inst.Platform)
			if inst.Type == "extension" || inst.Type == "gateway" {
				create = create.SetType(pluginent.Type(inst.Type))
			}
		}
		if _, err := create.Save(ctx); err != nil {
			return fmt.Errorf("创建插件配置失败: %w", err)
		}
		return nil
	}
	if _, err := row.Update().SetConfig(cfgJSON).Save(ctx); err != nil {
		return fmt.Errorf("更新插件配置失败: %w", err)
	}
	return nil
}

// UpdatePluginConfigLive applies a persisted configuration to a running plugin
// through ConfigWatcher. It returns false when the plugin does not support the
// hot-update RPC, allowing callers to fall back to a full reload.
func (m *Manager) UpdatePluginConfigLive(ctx context.Context, name string, config map[string]string) (bool, error) {
	inst := m.GetInstance(name)
	if inst == nil || inst.Gateway == nil {
		return false, nil
	}
	initConfig, err := m.buildInitConfig(ctx, m.resolveName(name))
	if err != nil {
		return false, err
	}
	for key, value := range config {
		initConfig[key] = value
	}
	if err := inst.UpdateConfig(newCorePluginContext(initConfig, m.resolveName(name))); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unimplemented") || strings.Contains(strings.ToLower(err.Error()), "does not support config hot reload") {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

// ReloadInstance uses the same candidate pipeline for source and installed artifacts.
func (m *Manager) ReloadInstance(ctx context.Context, name string) error {
	inst := m.GetInstance(name)
	if inst == nil || inst.Artifact == nil {
		return fmt.Errorf("plugin %s has no installed artifact", name)
	}
	if inst.Artifact.SourcePath != "" {
		return m.ReloadDev(ctx, name)
	}
	binary, err := os.ReadFile(m.artifactBinary(inst.Artifact))
	if err != nil {
		return err
	}
	return m.updatePlugin(ctx, inst.Name, binary, "", &inst.Artifact.Metadata, nil)
}

func (m *Manager) HasWebAssets(name string) bool {
	inst := m.GetInstance(name)
	return inst != nil && inst.Artifact != nil && len(inst.frontendAssets) > 0
}

func (m *Manager) resolveName(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resolveNameLocked(name)
}

func (m *Manager) resolveNameLocked(name string) string {
	name = normalizePluginName(name)
	if name == "" {
		return ""
	}
	if canonical, ok := m.aliases[name]; ok && canonical != "" {
		return canonical
	}
	return name
}

func (m *Manager) registerAliasesLocked(canonical string, aliases ...string) {
	canonical = normalizePluginName(canonical)
	if canonical == "" {
		return
	}
	m.aliases[canonical] = canonical
	for _, alias := range aliases {
		alias = normalizePluginName(alias)
		if alias == "" {
			continue
		}
		m.aliases[alias] = canonical
	}
}

func (m *Manager) unregisterAliasesLocked(canonical string, aliases ...string) {
	canonical = normalizePluginName(canonical)
	if canonical != "" {
		delete(m.aliases, canonical)
	}
	for _, alias := range aliases {
		alias = normalizePluginName(alias)
		if alias == "" {
			continue
		}
		delete(m.aliases, alias)
	}
}

func cloneModels(input []sdk.ModelInfo) []sdk.ModelInfo {
	return append([]sdk.ModelInfo(nil), input...)
}

func cloneRoutes(input []sdk.RouteDefinition) []sdk.RouteDefinition {
	return append([]sdk.RouteDefinition(nil), input...)
}

func cloneCredentialFields(input []sdk.CredentialField) []sdk.CredentialField {
	return append([]sdk.CredentialField(nil), input...)
}

func cloneAccountTypes(input []sdk.AccountType) []sdk.AccountType {
	if input == nil {
		return nil
	}
	cloned := make([]sdk.AccountType, 0, len(input))
	for _, item := range input {
		cloned = append(cloned, sdk.AccountType{
			Key:         item.Key,
			Label:       item.Label,
			Description: item.Description,
			Fields:      cloneCredentialFields(item.Fields),
		})
	}
	return cloned
}

func cloneFrontendPages(input []sdk.FrontendPage) []sdk.FrontendPage {
	return append([]sdk.FrontendPage(nil), input...)
}

func cloneConfigSchema(input []sdk.ConfigField) []sdk.ConfigField {
	return append([]sdk.ConfigField(nil), input...)
}
