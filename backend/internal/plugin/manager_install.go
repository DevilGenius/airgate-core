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
	"runtime"
	"strings"
)

const MaxPluginBinarySize int64 = 200 << 20

// Uninstall 卸载插件。
func (m *Manager) Uninstall(ctx context.Context, name string) error {
	m.artifactMu.RLock()
	defer m.artifactMu.RUnlock()
	ctx, op, err := m.beginUpdate(ctx, name)
	if err != nil {
		return err
	}
	defer m.finishUpdate(op)
	resolved := m.resolveName(name)
	inst := m.GetInstance(resolved)
	m.stopPlugin(resolved, ctx)
	m.mu.Lock()
	delete(m.devPaths, resolved)
	m.mu.Unlock()
	if err := os.Remove(m.manifestPath(resolved)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除插件清单失败: %w", err)
	}
	if inst != nil && inst.Artifact != nil {
		m.removeArtifact(inst.Generation)
		m.removeArtifact(inst.Artifact.Previous)
	}
	slog.Info("插件已卸载", "name", resolved)
	return nil
}

// InstallFromBinary 从二进制数据安装插件。
func (m *Manager) InstallFromBinary(ctx context.Context, name string, binary []byte) error {
	return m.installFromBinary(ctx, name, binary, nil)
}

// InstallFromBinaryWithSHA256 从二进制数据安装插件，并在执行前校验预期 SHA256。
func (m *Manager) InstallFromBinaryWithSHA256(ctx context.Context, name string, binary []byte, expectedSHA256 string) error {
	expectedSHA256 = normalizeSHA256(expectedSHA256)
	if expectedSHA256 == "" {
		return fmt.Errorf("无效的 SHA256 校验和")
	}
	actualSHA256 := calcBinarySHA256(binary)
	if actualSHA256 != expectedSHA256 {
		return fmt.Errorf("插件二进制 SHA256 校验失败: expected %s, got %s", expectedSHA256, actualSHA256)
	}
	return m.installFromBinary(ctx, name, binary, &installMetadata{AssetSHA256: actualSHA256})
}

func (m *Manager) installFromBinary(ctx context.Context, name string, binary []byte, meta *installMetadata) error {
	return m.updatePlugin(ctx, name, binary, "", meta, nil)
}

// InstallFromGithub 从 GitHub Release 下载并安装插件。
// version 为空时安装 latest release；非空时按 release tag 安装，可用于回滚到旧版本。
func (m *Manager) InstallFromGithub(ctx context.Context, repo, version string) error {
	owner, repoName, err := parseGithubRepo(repo)
	if err != nil {
		return err
	}

	release, err := fetchGithubReleaseForInstall(ctx, owner, repoName, version)
	if err != nil {
		return err
	}

	targetOS := runtime.GOOS
	targetArch := runtime.GOARCH
	asset := selectReleaseBinaryAsset(release.Assets, targetOS, targetArch)
	if asset == nil || asset.BrowserDownloadURL == "" {
		return fmt.Errorf("未找到适配 %s/%s 的二进制文件，Release: %s", targetOS, targetArch, release.TagName)
	}
	if asset.Size > MaxPluginBinarySize {
		return pluginBinaryTooLargeError(asset.Size)
	}

	dlReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		return fmt.Errorf("下载插件失败: %w", err)
	}
	defer func() {
		if err := dlResp.Body.Close(); err != nil {
			slog.Warn("关闭插件下载响应失败", "url", asset.BrowserDownloadURL, "error", err)
		}
	}()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回状态码 %d", dlResp.StatusCode)
	}
	if dlResp.ContentLength > MaxPluginBinarySize {
		return pluginBinaryTooLargeError(dlResp.ContentLength)
	}

	binary, err := ReadPluginBinary(dlResp.Body)
	if err != nil {
		return fmt.Errorf("读取下载内容失败: %w", err)
	}
	binarySHA256 := calcBinarySHA256(binary)
	expectedSHA256 := resolveReleaseAssetSHA256(ctx, *asset, release.Assets, "")
	if expectedSHA256 == "" {
		return fmt.Errorf("插件 Release 缺少 SHA256 校验信息")
	}
	if expectedSHA256 != binarySHA256 {
		return fmt.Errorf("插件二进制 SHA256 校验失败: expected %s, got %s", expectedSHA256, binarySHA256)
	}

	meta := &installMetadata{
		GithubRepo:  owner + "/" + repoName,
		Version:     strings.TrimPrefix(release.TagName, "v"),
		CommitSHA:   resolveGithubTagCommitSHA(ctx, owner+"/"+repoName, release.TagName, ""),
		AssetSHA256: binarySHA256,
	}

	return m.installFromBinary(ctx, repoName, binary, meta)
}

func ReadPluginBinary(r io.Reader) ([]byte, error) {
	return readPluginBinary(r, MaxPluginBinarySize)
}

func readPluginBinary(r io.Reader, limit int64) ([]byte, error) {
	binary, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(binary)) > limit {
		return nil, pluginBinaryTooLargeError(int64(len(binary)))
	}
	return binary, nil
}

func pluginBinaryTooLargeError(size int64) error {
	return fmt.Errorf("插件二进制超过 %d MiB 上限: %d bytes", MaxPluginBinarySize>>20, size)
}

func calcBinarySHA256(binary []byte) string {
	sum := sha256.Sum256(binary)
	return hex.EncodeToString(sum[:])
}

func fetchGithubReleaseForInstall(ctx context.Context, owner, repoName, version string) (githubRelease, error) {
	var lastStatus int
	for _, apiURL := range githubReleaseAPIURLs(owner, repoName, version) {
		release, status, err := fetchGithubReleaseByURL(ctx, apiURL)
		if err == nil {
			return release, nil
		}
		lastStatus = status
		if status != http.StatusNotFound {
			return githubRelease{}, err
		}
	}

	if strings.TrimSpace(version) == "" {
		return githubRelease{}, fmt.Errorf("仓库 %s/%s 不存在或没有 Release", owner, repoName)
	}
	if lastStatus == http.StatusNotFound {
		return githubRelease{}, fmt.Errorf("仓库 %s/%s 不存在或没有 Release %s", owner, repoName, strings.TrimSpace(version))
	}
	return githubRelease{}, fmt.Errorf("无法获取仓库 %s/%s 的 Release %s", owner, repoName, strings.TrimSpace(version))
}

func fetchGithubReleaseByURL(ctx context.Context, apiURL string) (githubRelease, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubRelease{}, 0, fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("关闭 GitHub API 响应失败", "url", apiURL, "error", err)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		return githubRelease{}, resp.StatusCode, fmt.Errorf("GitHub Release 不存在")
	}
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, resp.StatusCode, fmt.Errorf("GitHub API 返回状态码 %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, resp.StatusCode, fmt.Errorf("解析 Release 数据失败: %w", err)
	}
	return release, resp.StatusCode, nil
}

func githubReleaseAPIURLs(owner, repoName, version string) []string {
	baseURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repoName)
	version = strings.TrimSpace(version)
	if version == "" {
		return []string{baseURL + "/latest"}
	}

	tags := []string{version}
	if strings.HasPrefix(version, "v") {
		if trimmed := strings.TrimPrefix(version, "v"); trimmed != "" {
			tags = append(tags, trimmed)
		}
	} else {
		tags = append(tags, "v"+version)
	}

	urls := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		urls = append(urls, baseURL+"/tags/"+url.PathEscape(tag))
	}
	return urls
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

// parseGithubRepo 解析 GitHub 仓库地址。
func parseGithubRepo(repo string) (owner, name string, err error) {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), "/")
	repo = strings.TrimSuffix(repo, ".git")

	if strings.Contains(repo, "github.com") {
		parts := strings.Split(repo, "github.com/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("无效的 GitHub 地址: %s", repo)
		}
		repo = parts[1]
	}

	segments := strings.Split(repo, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("无效的仓库格式，请使用 owner/repo 格式")
	}
	return segments[0], segments[1], nil
}
