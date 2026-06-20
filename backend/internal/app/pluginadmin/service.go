package pluginadmin

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/DevilGenius/airgate-core/internal/plugin"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

var pluginAdminProxyHandleHTTPRequest = (*sdkgrpc.GatewayGRPCClient).HandleHTTPRequest

// Service 提供插件管理用例编排。
type Service struct {
	manager     Manager
	marketplace MarketplaceReader
}

// NewService 创建插件管理服务。
func NewService(manager Manager, marketplace MarketplaceReader) *Service {
	return &Service{
		manager:     manager,
		marketplace: marketplace,
	}
}

// List 返回运行中的插件列表。
func (s *Service) List() []PluginMeta {
	allMeta := s.manager.GetAllPluginMeta()
	commitLookup := s.releaseCommitLookup(context.Background())
	result := make([]PluginMeta, 0, len(allMeta))
	for _, item := range allMeta {
		commitSHA := item.CommitSHA
		if commitSHA == "" {
			commitSHA = commitLookup[releaseCommitKey(item.Name, item.Version, item.BinarySHA256)]
		}
		result = append(result, PluginMeta{
			Name:               item.Name,
			DisplayName:        item.DisplayName,
			Version:            installedDisplayVersion(item.Version, item.IsDev, commitSHA, item.BinarySHA256),
			Author:             item.Author,
			Type:               item.Type,
			Platform:           item.Platform,
			AccountTypes:       append([]sdk.AccountType(nil), item.AccountTypes...),
			FrontendPages:      append([]sdk.FrontendPage(nil), item.FrontendPages...),
			InstructionPresets: append([]string(nil), item.InstructionPresets...),
			ConfigSchema:       append([]sdk.ConfigField(nil), item.ConfigSchema...),
			Metadata:           cloneStringMap(item.Metadata),
			HasWebAssets:       item.HasWebAssets,
			IsDev:              item.IsDev,
			CommitSHA:          commitSHA,
		})
	}
	return result
}

// IsLoading 返回启动阶段插件是否仍在后台加载。
func (s *Service) IsLoading() bool {
	return s.manager.IsLoading()
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// GetConfig 读取插件持久化的配置（隐藏 password 类型字段的值，仅返回 key 列表）。
func (s *Service) GetConfig(ctx context.Context, name string) (map[string]string, error) {
	return s.manager.GetPluginConfig(ctx, name)
}

// UpdateConfig 写入插件配置并触发 reload。
//
// 注意 reload 失败不会回滚配置：用户应当看到错误后修改配置再重试。
func (s *Service) UpdateConfig(ctx context.Context, name string, config map[string]string) error {
	logger := sdk.LoggerFromContext(ctx)
	if err := s.manager.UpdatePluginConfig(ctx, name, config); err != nil {
		logger.Error("plugin_admin_config_updated_failed",
			sdk.LogFieldPluginID, name,
			sdk.LogFieldError, err)
		return err
	}
	logger.Info("plugin_admin_config_updated", sdk.LogFieldPluginID, name)
	if err := s.manager.ReloadInstance(ctx, name); err != nil {
		logger.Error("plugin_admin_reload_failed",
			sdk.LogFieldPluginID, name,
			sdk.LogFieldError, err)
		return err
	}
	return nil
}

// Upload 从二进制安装插件。
func (s *Service) Upload(ctx context.Context, name string, binary []byte, expectedSHA256 string) error {
	logger := sdk.LoggerFromContext(ctx)
	copied := append([]byte(nil), binary...)
	expectedSHA256 = normalizeSHA256ForCompare(expectedSHA256)
	if expectedSHA256 == "" {
		return fmt.Errorf("请提供有效的 SHA256 校验和")
	}
	if err := s.manager.InstallFromBinaryWithSHA256(ctx, name, copied, expectedSHA256); err != nil {
		logger.Error("plugin_admin_uploaded_failed",
			sdk.LogFieldPluginID, name,
			"size_bytes", len(copied),
			sdk.LogFieldError, err)
		return err
	}
	logger.Info("plugin_admin_uploaded",
		sdk.LogFieldPluginID, name,
		"size_bytes", len(copied))
	return nil
}

// InstallFromGithub 从 GitHub 安装插件。
func (s *Service) InstallFromGithub(ctx context.Context, repo, version string) error {
	logger := sdk.LoggerFromContext(ctx)
	version = strings.TrimSpace(version)
	if err := s.manager.InstallFromGithub(ctx, repo, version); err != nil {
		logger.Error("plugin_admin_uploaded_failed",
			"repo", repo,
			"version", version,
			"source", "github",
			sdk.LogFieldError, err)
		return err
	}
	logger.Info("plugin_admin_uploaded",
		"repo", repo,
		"version", version,
		"source", "github")
	return nil
}

// Uninstall 卸载插件。
func (s *Service) Uninstall(ctx context.Context, name string) error {
	logger := sdk.LoggerFromContext(ctx)
	if err := s.manager.Uninstall(ctx, name); err != nil {
		logger.Error("plugin_admin_removed_failed",
			sdk.LogFieldPluginID, name,
			sdk.LogFieldError, err)
		return err
	}
	logger.Info("plugin_admin_removed", sdk.LogFieldPluginID, name)
	return nil
}

// Reload 热加载插件。
func (s *Service) Reload(ctx context.Context, name string) error {
	logger := sdk.LoggerFromContext(ctx)
	if !s.manager.IsDev(name) {
		logger.Warn("plugin_admin_reload_failed",
			sdk.LogFieldPluginID, name,
			sdk.LogFieldReason, "not_dev_plugin")
		return ErrPluginNotDev
	}
	if err := s.manager.ReloadDev(ctx, name); err != nil {
		logger.Error("plugin_admin_reload_failed",
			sdk.LogFieldPluginID, name,
			sdk.LogFieldError, err)
		return err
	}
	logger.Info("plugin_admin_enabled",
		sdk.LogFieldPluginID, name,
		"op", "dev_reload")
	return nil
}

// Proxy 转发插件管理请求。
func (s *Service) Proxy(ctx context.Context, input ProxyInput) (ProxyResult, error) {
	inst := s.manager.GetInstance(input.Name)
	if inst == nil || inst.Gateway == nil {
		return ProxyResult{}, ErrPluginUnavailable
	}

	status, headers, body, err := pluginAdminProxyHandleHTTPRequest(
		inst.Gateway,
		ctx,
		input.Method,
		input.Action,
		input.Query,
		input.Headers,
		input.Body,
	)
	if err != nil {
		return ProxyResult{}, err
	}

	return ProxyResult{
		StatusCode: status,
		Headers:    headers,
		Body:       body,
	}, nil
}

// RefreshMarketplace 强制从 GitHub 同步市场列表。
func (s *Service) RefreshMarketplace(ctx context.Context) error {
	return s.marketplace.SyncFromGithub(ctx)
}

// ListMarketplace 返回市场插件列表。
func (s *Service) ListMarketplace(ctx context.Context) ([]MarketplacePlugin, error) {
	items, err := s.marketplace.ListAvailable(ctx)
	if err != nil {
		return nil, err
	}

	installed := make(map[string]installedPlugin)
	for _, meta := range s.manager.GetAllPluginMeta() {
		installed[meta.Name] = installedPlugin{
			version:      meta.Version,
			isDev:        meta.IsDev,
			binarySHA256: meta.BinarySHA256,
			commitSHA:    meta.CommitSHA,
		}
	}

	result := make([]MarketplacePlugin, 0, len(items))
	for _, item := range items {
		meta, ok := installed[item.Name]
		installedCommitSHA := meta.commitSHA
		if installedCommitSHA == "" {
			installedCommitSHA = matchingReleaseCommit(item, meta)
		}
		result = append(result, MarketplacePlugin{
			Name:             item.Name,
			Version:          item.Version,
			DisplayVersion:   marketplaceDisplayVersion(item.Version, item.CommitSHA, item.SHA256),
			Description:      item.Description,
			Author:           item.Author,
			Type:             item.Type,
			GithubRepo:       item.GithubRepo,
			Installed:        ok,
			InstalledVersion: installedDisplayVersion(meta.version, meta.isDev, installedCommitSHA, meta.binarySHA256),
			HasUpdate:        ok && !meta.isDev && hasPluginUpdate(item, meta),
		})
	}
	return result, nil
}

type installedPlugin struct {
	version      string
	isDev        bool
	binarySHA256 string
	commitSHA    string
}

func hasPluginUpdate(item plugin.MarketplacePlugin, installed installedPlugin) bool {
	if isNewerVersion(item.Version, installed.version) {
		return true
	}
	if normalizeVersionForCompare(item.Version) != normalizeVersionForCompare(installed.version) {
		return false
	}
	latestSHA := normalizeSHA256ForCompare(item.SHA256)
	installedSHA := normalizeSHA256ForCompare(installed.binarySHA256)
	return latestSHA != "" && installedSHA != "" && latestSHA != installedSHA
}

func installedDisplayVersion(version string, isDev bool, commitSHA, binarySHA256 string) string {
	version = normalizeVersionForCompare(version)
	if isDev || version == "" || version == "dev" {
		return "dev"
	}
	if hash := shortHash(commitSHA); hash != "" {
		return version + "-" + hash
	}
	if hash := shortHash(binarySHA256); hash != "" {
		return version + "-" + hash
	}
	return version
}

func marketplaceDisplayVersion(version, commitSHA, assetSHA256 string) string {
	version = normalizeVersionForCompare(version)
	if version == "" {
		return ""
	}
	if hash := shortHash(commitSHA); hash != "" {
		return version + "-" + hash
	}
	if hash := shortHash(assetSHA256); hash != "" {
		return version + "-" + hash
	}
	return version
}

func (s *Service) releaseCommitLookup(ctx context.Context) map[string]string {
	if s.marketplace == nil {
		return nil
	}
	items, err := s.marketplace.ListAvailable(ctx)
	if err != nil || len(items) == 0 {
		return nil
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		key := releaseCommitKey(item.Name, item.Version, item.SHA256)
		if key == "" || shortHash(item.CommitSHA) == "" {
			continue
		}
		result[key] = item.CommitSHA
	}
	return result
}

func matchingReleaseCommit(item plugin.MarketplacePlugin, installed installedPlugin) string {
	if normalizeVersionForCompare(item.Version) != normalizeVersionForCompare(installed.version) {
		return ""
	}
	latestSHA := normalizeSHA256ForCompare(item.SHA256)
	installedSHA := normalizeSHA256ForCompare(installed.binarySHA256)
	if latestSHA == "" || installedSHA == "" || latestSHA != installedSHA {
		return ""
	}
	return item.CommitSHA
}

func releaseCommitKey(name, version, sha256 string) string {
	sha256 = normalizeSHA256ForCompare(sha256)
	if sha256 == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(name)) + "\x00" + normalizeVersionForCompare(version) + "\x00" + sha256
}

func normalizeVersionForCompare(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

// isNewerVersion 判断 marketplaceVer 是否比 installedVer 新。
// 采用简单的 semver 语义：按点分段数字比较，忽略前导 v。非数字段做字符串比较。
// 任一参数为空则返回 false。
func isNewerVersion(marketplaceVer, installedVer string) bool {
	if marketplaceVer == "" || installedVer == "" {
		return false
	}
	m := strings.TrimPrefix(strings.TrimSpace(marketplaceVer), "v")
	i := strings.TrimPrefix(strings.TrimSpace(installedVer), "v")
	if m == i {
		return false
	}
	mParts := strings.Split(m, ".")
	iParts := strings.Split(i, ".")
	n := len(mParts)
	if len(iParts) > n {
		n = len(iParts)
	}
	for idx := 0; idx < n; idx++ {
		var mp, ip string
		if idx < len(mParts) {
			mp = mParts[idx]
		}
		if idx < len(iParts) {
			ip = iParts[idx]
		}
		mn, mErr := strconv.Atoi(mp)
		in, iErr := strconv.Atoi(ip)
		if mErr == nil && iErr == nil {
			if mn != in {
				return mn > in
			}
			continue
		}
		if mp != ip {
			return mp > ip
		}
	}
	return false
}

func normalizeSHA256ForCompare(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	fields := strings.Fields(value)
	if len(fields) > 0 {
		value = fields[0]
	}
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func shortHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	fields := strings.Fields(value)
	if len(fields) > 0 {
		value = fields[0]
	}
	if len(value) < 7 {
		return ""
	}
	for idx, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			if idx < 7 {
				return ""
			}
			break
		}
	}
	return value[:7]
}
