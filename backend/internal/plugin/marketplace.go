package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MarketplacePlugin 市场插件条目
type MarketplacePlugin struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Type        string `json:"type"` // gateway / payment / extension
	GithubRepo  string `json:"github_repo,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
}

// RegistryJSON 插件源注册表结构
type RegistryJSON struct {
	Version string              `json:"version"`
	Plugins []MarketplacePlugin `json:"plugins"`
}

// MarketplaceOption 配置选项
type MarketplaceOption func(*Marketplace)

// WithGithubToken 设置 GitHub Token
func WithGithubToken(token string) MarketplaceOption {
	return func(m *Marketplace) {
		m.githubToken = token
	}
}

// WithEntries 用配置文件中的条目覆盖默认列表
func WithEntries(entries []MarketplacePlugin) MarketplaceOption {
	return func(m *Marketplace) {
		if len(entries) > 0 {
			m.entries = entries
		}
	}
}

// WithRefreshInterval 设置后台同步间隔
func WithRefreshInterval(d time.Duration) MarketplaceOption {
	return func(m *Marketplace) {
		if d > 0 {
			m.refreshInterval = d
		}
	}
}

// Marketplace 插件市场
type Marketplace struct {
	pluginDir       string
	githubToken     string
	refreshInterval time.Duration

	mu      sync.RWMutex
	entries []MarketplacePlugin // 静态条目（含 github_repo）
	cache   []MarketplacePlugin // 已同步的最新数据
	etags   map[string]string   // repo -> ETag，用于条件请求避免消耗配额

	stopCh    chan struct{}
	stopped   chan struct{}
	once      sync.Once
	startOnce sync.Once
	started   bool
}

// 默认刷新间隔：6 小时
// 7 个插件 × 4 次/天 = 28 次/天，未认证 IP 配额 60/h 也绰绰有余；
// 配合 ETag 条件请求，未变更时返回 304 不计配额。
const defaultRefreshInterval = 6 * time.Hour

// NewMarketplace 创建插件市场
func NewMarketplace(pluginDir string, opts ...MarketplaceOption) *Marketplace {
	m := &Marketplace{
		pluginDir:       pluginDir,
		refreshInterval: defaultRefreshInterval,
		entries:         append([]MarketplacePlugin(nil), officialPlugins...),
		etags:           make(map[string]string),
		stopCh:          make(chan struct{}),
		stopped:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	// 初始 cache 用静态 entries 兜底，避免首次同步前列表为空
	m.cache = append([]MarketplacePlugin(nil), m.entries...)
	return m
}

// officialPlugins 官方插件列表（作为无源时的 fallback，绑定 GitHub 仓库）
var officialPlugins = []MarketplacePlugin{
	{
		Name:        "gateway-openai",
		Version:     "0.0.1",
		Description: "OpenAI API 网关插件",
		Author:      "AirGate",
		Type:        "gateway",
		GithubRepo:  "DevilGenius/airgate-openai",
	},
	{
		Name:        "payment-epay",
		Version:     "0.0.1",
		Description: "多渠道支付插件：易支付 / 支付宝官方 / 微信支付官方",
		Author:      "AirGate",
		Type:        "extension",
		GithubRepo:  "DevilGenius/airgate-epay",
	},
	{
		Name:        "airgate-playground",
		Version:     "0.0.1",
		Description: "AI 对话插件：网页聊天、多模型切换、会话管理",
		Author:      "AirGate",
		Type:        "extension",
		GithubRepo:  "DevilGenius/airgate-playground",
	},
	{
		Name:        "gateway-claude",
		Version:     "0.0.1",
		Description: "Claude Messages API 网关插件：OAuth 授权、TLS 指纹、用量监控",
		Author:      "AirGate",
		Type:        "gateway",
		GithubRepo:  "DevilGenius/airgate-claude",
	},
	{
		Name:        "gateway-kiro",
		Version:     "0.0.1",
		Description: "Kiro (AWS CodeWhisperer) 反代网关，兼容 Anthropic Messages API",
		Author:      "AirGate",
		Type:        "gateway",
		GithubRepo:  "DevilGenius/airgate-kiro",
	},
	{
		Name:        "airgate-studio",
		Version:     "0.0.1",
		Description: "面向图片、视频、音频等多模态内容生成的统一创作中心",
		Author:      "AirGate",
		Type:        "extension",
		GithubRepo:  "DevilGenius/airgate-studio",
	},
}

// ListAvailable 列出可用插件（返回缓存或同步后的数据）
func (m *Marketplace) ListAvailable(ctx context.Context) ([]MarketplacePlugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MarketplacePlugin, len(m.cache))
	copy(out, m.cache)
	return out, nil
}

// Start 启动后台同步 goroutine。若 entries 中没有任何 GithubRepo 则跳过。
func (m *Marketplace) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		m.mu.Lock()
		m.started = true
		m.mu.Unlock()
		go m.run(ctx)
	})
}

// Stop 停止后台同步
func (m *Marketplace) Stop() {
	m.once.Do(func() {
		close(m.stopCh)
		m.mu.RLock()
		started := m.started
		m.mu.RUnlock()
		if started {
			<-m.stopped
		}
	})
}

// run 后台运行循环：启动时同步一次，之后按 refreshInterval 定时同步
func (m *Marketplace) run(ctx context.Context) {
	defer close(m.stopped)

	// 启动后立即同步一次（异步，不阻塞 server 启动）
	if err := m.SyncFromGithub(ctx); err != nil {
		slog.Warn("插件市场首次同步失败", "error", err)
	}

	ticker := time.NewTicker(m.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			if err := m.SyncFromGithub(ctx); err != nil {
				slog.Warn("插件市场同步失败", "error", err)
			}
		}
	}
}

// SyncFromGithub 遍历 entries，串行调用 GitHub API 拉取 latest release。
// 使用 ETag 条件请求：上游 release 未变更时 GitHub 返回 304，**不消耗 API 配额**。
func (m *Marketplace) SyncFromGithub(ctx context.Context) error {
	m.mu.RLock()
	entries := append([]MarketplacePlugin(nil), m.entries...)
	prevCache := append([]MarketplacePlugin(nil), m.cache...)
	token := m.githubToken
	etagSnapshot := make(map[string]string, len(m.etags))
	for k, v := range m.etags {
		etagSnapshot[k] = v
	}
	m.mu.RUnlock()

	if len(entries) == 0 {
		return nil
	}

	// 把上次缓存按 name 索引，方便 304 时复用旧数据
	prevByName := make(map[string]MarketplacePlugin, len(prevCache))
	for _, p := range prevCache {
		prevByName[p.Name] = p
	}

	updated := make([]MarketplacePlugin, 0, len(entries))
	newEtags := make(map[string]string, len(entries))
	var lastErr error
	notModified := 0
	fetched := 0
	failed := 0
	successfulGithub := 0

	for _, entry := range entries {
		if entry.GithubRepo == "" {
			updated = append(updated, entry)
			continue
		}

		release, etag, status, err := fetchLatestRelease(ctx, entry.GithubRepo, token, etagSnapshot[entry.GithubRepo])
		if err != nil {
			slog.Debug("拉取插件 release 失败", "repo", entry.GithubRepo, "error", err)
			lastErr = err
			failed++
			// 失败时保留上次缓存条目
			if prev, ok := prevByName[entry.Name]; ok {
				updated = append(updated, prev)
			} else {
				updated = append(updated, entry)
			}
			// 保留旧 etag 以便下次仍走条件请求
			if old := etagSnapshot[entry.GithubRepo]; old != "" {
				newEtags[entry.GithubRepo] = old
			}
			continue
		}

		if status == http.StatusNotModified {
			notModified++
			successfulGithub++
			// 复用上次结果，etag 保留
			if prev, ok := prevByName[entry.Name]; ok {
				updated = append(updated, prev)
			} else {
				updated = append(updated, entry)
			}
			newEtags[entry.GithubRepo] = etagSnapshot[entry.GithubRepo]
			continue
		}

		fetched++
		successfulGithub++
		merged := entry
		if release.TagName != "" {
			merged.Version = strings.TrimPrefix(release.TagName, "v")
		}
		merged.CommitSHA = resolveGithubTagCommitSHA(ctx, entry.GithubRepo, release.TagName, token)
		if asset := selectReleaseBinaryAsset(release.Assets, runtime.GOOS, runtime.GOARCH); asset != nil {
			merged.DownloadURL = asset.BrowserDownloadURL
			merged.SHA256 = resolveReleaseAssetSHA256(ctx, *asset, release.Assets, token)
		}
		// 描述保持静态值（来自 officialPlugins 或 config），不用 release body：
		// release notes 描述的是"这一版改了什么"，与"插件是干嘛的"是两回事，
		// GitHub generate_release_notes 还会自动塞入 "## What's Changed" 标题。
		updated = append(updated, merged)
		if etag != "" {
			newEtags[entry.GithubRepo] = etag
		}
	}

	m.mu.Lock()
	m.cache = updated
	m.etags = newEtags
	m.mu.Unlock()

	slog.Info("插件市场同步完成",
		"count", len(updated),
		"fetched", fetched,
		"not_modified", notModified,
		"failed", failed,
	)
	return marketplaceSyncError(successfulGithub, failed, lastErr)
}

func marketplaceSyncError(successfulGithub, failed int, lastErr error) error {
	if successfulGithub == 0 && failed > 0 {
		return lastErr
	}
	return nil
}

// SyncFromURL 从指定 URL 同步插件列表（保留兼容旧接口）
func (m *Marketplace) SyncFromURL(ctx context.Context, registryURL string) error {
	resp, err := http.Get(registryURL)
	if err != nil {
		return fmt.Errorf("请求插件源失败: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("关闭插件源响应失败", "url", registryURL, "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("插件源返回状态码 %d", resp.StatusCode)
	}

	var registry RegistryJSON
	if err := json.NewDecoder(resp.Body).Decode(&registry); err != nil {
		return fmt.Errorf("解析插件源数据失败: %w", err)
	}

	m.mu.Lock()
	m.cache = registry.Plugins
	m.mu.Unlock()

	slog.Info("插件源同步完成", "url", registryURL, "count", len(registry.Plugins))
	return nil
}

// githubReleaseInfo GitHub release API 简化结构
type githubReleaseInfo struct {
	TagName string        `json:"tag_name"`
	Name    string        `json:"name"`
	Body    string        `json:"body"`
	Assets  []githubAsset `json:"assets"`
}

type githubObjectRef struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

type githubRefInfo struct {
	Object githubObjectRef `json:"object"`
}

type githubTagInfo struct {
	Object githubObjectRef `json:"object"`
}

// fetchLatestRelease 调用 GitHub API 获取仓库最新 release。
// 若提供 etag，会发送 If-None-Match 条件请求；上游未变更时返回 (nil, "", 304, nil)，**不消耗配额**。
// 返回值：release 信息、新的 ETag、HTTP 状态码、错误
func fetchLatestRelease(ctx context.Context, repo, token, etag string) (*githubReleaseInfo, string, int, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 304 Not Modified：上游未变更，直接复用缓存（不消耗 GitHub 配额）
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, http.StatusNotModified, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", resp.StatusCode, fmt.Errorf("仓库 %s 没有 release", repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", resp.StatusCode, fmt.Errorf("GitHub API 状态码 %d", resp.StatusCode)
	}

	var info githubReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("解析 release 失败: %w", err)
	}
	return &info, resp.Header.Get("ETag"), resp.StatusCode, nil
}

func resolveGithubTagCommitSHA(ctx context.Context, repo, tagName, token string) string {
	sha, err := fetchGithubTagCommitSHA(ctx, repo, tagName, token)
	if err != nil {
		slog.Debug("plugin_release_commit_resolve_failed", "repo", repo, "tag", tagName, "error", err)
		return ""
	}
	return sha
}

func fetchGithubTagCommitSHA(ctx context.Context, repo, tagName, token string) (string, error) {
	repo = strings.TrimSpace(repo)
	tagName = strings.TrimSpace(tagName)
	if repo == "" || tagName == "" {
		return "", nil
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/git/ref/tags/%s", repo, url.PathEscape(tagName))
	var ref githubRefInfo
	if err := fetchGithubAPIJSON(ctx, apiURL, token, &ref); err != nil {
		return "", err
	}

	switch ref.Object.Type {
	case "commit":
		return normalizeGitCommitSHA(ref.Object.SHA), nil
	case "tag":
		if ref.Object.URL == "" {
			return "", nil
		}
		var tag githubTagInfo
		if err := fetchGithubAPIJSON(ctx, ref.Object.URL, token, &tag); err != nil {
			return "", err
		}
		if tag.Object.Type == "commit" {
			return normalizeGitCommitSHA(tag.Object.SHA), nil
		}
	}
	return "", nil
}

func fetchGithubAPIJSON(ctx context.Context, apiURL, token string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API 状态码 %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("解析 GitHub API 响应失败: %w", err)
	}
	return nil
}

func selectReleaseBinaryAsset(assets []githubAsset, goos, goarch string) *githubAsset {
	goos = strings.ToLower(strings.TrimSpace(goos))
	goarch = strings.ToLower(strings.TrimSpace(goarch))
	for idx := range assets {
		name := strings.ToLower(assets[idx].Name)
		if strings.HasSuffix(name, ".sha256") {
			continue
		}
		if strings.Contains(name, goos) && strings.Contains(name, goarch) {
			return &assets[idx]
		}
	}
	return nil
}

func resolveReleaseAssetSHA256(ctx context.Context, asset githubAsset, assets []githubAsset, token string) string {
	if hash := normalizeSHA256(asset.Digest); hash != "" {
		return hash
	}

	checksumName := asset.Name + ".sha256"
	for idx := range assets {
		if assets[idx].Name != checksumName || assets[idx].BrowserDownloadURL == "" {
			continue
		}
		body, err := fetchSmallTextAsset(ctx, assets[idx].BrowserDownloadURL, token)
		if err != nil {
			slog.Debug("plugin_release_checksum_fetch_failed", "asset", checksumName, "error", err)
			return ""
		}
		return normalizeSHA256(body)
	}
	return ""
}

func fetchSmallTextAsset(ctx context.Context, url, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载校验文件返回状态码 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	fields := strings.Fields(value)
	if len(fields) > 0 {
		value = fields[0]
	}
	if len(value) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func normalizeGitCommitSHA(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 40 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

// Download 下载插件二进制到本地
func (m *Marketplace) Download(ctx context.Context, pluginName, version, downloadURL, expectedSHA256 string) (string, error) {
	// 创建目标目录
	targetDir := filepath.Join(m.pluginDir, pluginName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("创建插件目录失败: %w", err)
	}

	// 下载文件
	resp, err := http.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("下载插件失败: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("关闭插件下载响应失败", "url", downloadURL, "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载返回状态码 %d", resp.StatusCode)
	}

	// 写入临时文件
	tmpFile := filepath.Join(targetDir, pluginName+".tmp")
	f, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	closeTempFile := func() error {
		if err := f.Close(); err != nil {
			return fmt.Errorf("关闭临时文件失败: %w", err)
		}
		return nil
	}
	removeTempFile := func() {
		if err := os.Remove(tmpFile); err != nil && !os.IsNotExist(err) {
			slog.Warn("删除临时插件文件失败", "path", tmpFile, "error", err)
		}
	}

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		if closeErr := closeTempFile(); closeErr != nil {
			slog.Warn("写入失败后关闭临时文件失败", "path", tmpFile, "error", closeErr)
		}
		removeTempFile()
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	if err := closeTempFile(); err != nil {
		removeTempFile()
		return "", err
	}

	// SHA256 校验
	if expectedSHA256 != "" {
		actualHash := hex.EncodeToString(hasher.Sum(nil))
		if actualHash != expectedSHA256 {
			removeTempFile()
			return "", fmt.Errorf("SHA256 校验失败: 期望 %s，实际 %s", expectedSHA256, actualHash)
		}
	}

	// 重命名为最终文件
	finalPath := filepath.Join(targetDir, pluginName)
	if err := os.Rename(tmpFile, finalPath); err != nil {
		removeTempFile()
		return "", fmt.Errorf("移动文件失败: %w", err)
	}

	// 设置可执行权限
	if err := os.Chmod(finalPath, 0755); err != nil {
		return "", fmt.Errorf("设置执行权限失败: %w", err)
	}

	slog.Info("插件下载完成", "name", pluginName, "version", version, "path", finalPath)
	return finalPath, nil
}
