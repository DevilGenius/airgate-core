package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	goplugin "github.com/hashicorp/go-plugin"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// Uninstall 卸载插件。
func (m *Manager) Uninstall(ctx context.Context, name string) error {
	resolvedName := m.resolveName(name)
	inst := m.GetInstance(resolvedName)

	m.stopPlugin(resolvedName)

	m.mu.Lock()
	delete(m.devPaths, resolvedName)
	m.mu.Unlock()

	targetDirs := []string{filepath.Join(m.pluginDir, resolvedName)}
	if inst != nil && inst.BinaryDir != "" && inst.BinaryDir != resolvedName {
		targetDirs = append(targetDirs, filepath.Join(m.pluginDir, inst.BinaryDir))
	}

	for _, targetDir := range targetDirs {
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("删除插件目录失败: %w", err)
		}
	}

	slog.Info("插件已卸载", "name", resolvedName)
	return nil
}

// InstallFromBinary 从二进制数据安装插件。
func (m *Manager) InstallFromBinary(ctx context.Context, name string, binary []byte) error {
	realName, err := m.probePluginName(name, binary)
	if err != nil {
		slog.Warn("探测插件名称失败，使用传入名称", "name", name, "error", err)
		realName = name
	}

	targetDir := filepath.Join(m.pluginDir, realName)
	binaryPath := filepath.Join(targetDir, realName)
	previousBinary, previousErr := os.ReadFile(binaryPath)

	m.stopPlugin(realName)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("创建插件目录失败: %w", err)
	}
	if err := os.WriteFile(binaryPath, binary, 0755); err != nil {
		return fmt.Errorf("写入插件二进制失败: %w", err)
	}

	canonicalName, err := m.startPlugin(ctx, realName, exec.Command(binaryPath), realName)
	if err != nil {
		if previousErr == nil {
			if restoreErr := m.restorePreviousBinary(ctx, realName, binaryPath, previousBinary); restoreErr != nil {
				return fmt.Errorf("启动插件失败: %w；恢复旧版本也失败: %v", err, restoreErr)
			}
			slog.Warn("新插件启动失败，已恢复旧版本", "name", realName, "error", err)
		}
		return fmt.Errorf("启动插件失败: %w", err)
	}

	slog.Info("插件从二进制安装成功", "name", canonicalName)
	return nil
}

func (m *Manager) restorePreviousBinary(ctx context.Context, name, binaryPath string, previousBinary []byte) error {
	if err := os.WriteFile(binaryPath, previousBinary, 0755); err != nil {
		return fmt.Errorf("写回旧插件二进制失败: %w", err)
	}
	if _, err := m.startPlugin(ctx, name, exec.Command(binaryPath), name); err != nil {
		return fmt.Errorf("重启旧插件失败: %w", err)
	}
	return nil
}

func (m *Manager) probePluginName(fallbackName string, binary []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "airgate-probe-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Warn("清理插件探测临时目录失败", "dir", tmpDir, "error", err)
		}
	}()

	tmpBinary := filepath.Join(tmpDir, fallbackName)
	if err := os.WriteFile(tmpBinary, binary, 0755); err != nil {
		return "", fmt.Errorf("写入临时二进制失败: %w", err)
	}

	// 探测式 spawn：只是为了拿 Info()，不挂 host handle（capability 校验不适用）
	client := goplugin.NewClient(m.newPluginClientConfig(exec.Command(tmpBinary), false, nil))
	defer client.Kill()

	rpcClient, err := client.Client()
	if err != nil {
		return "", fmt.Errorf("连接探测进程失败: %w", err)
	}

	raw, err := rpcClient.Dispense(sdkgrpc.PluginKeyGateway)
	if err != nil {
		return "", fmt.Errorf("获取探测接口失败: %w", err)
	}
	probe, ok := raw.(*sdkgrpc.GatewayGRPCClient)
	if !ok {
		return "", fmt.Errorf("探测类型断言失败")
	}

	info := probe.Info()
	if info.Type == sdk.PluginTypeExtension {
		extRaw, err := rpcClient.Dispense(sdkgrpc.PluginKeyExtension)
		if err != nil {
			return "", fmt.Errorf("获取 extension 探测接口失败: %w", err)
		}
		if ext, ok := extRaw.(*sdkgrpc.ExtensionGRPCClient); ok {
			if extInfo := ext.Info(); extInfo.ID != "" {
				return extInfo.ID, nil
			}
		}
		return fallbackName, nil
	}

	if info.ID != "" {
		return info.ID, nil
	}
	return fallbackName, nil
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

	dlReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		return fmt.Errorf("下载插件失败: %w", err)
	}
	defer func() {
		if err := dlResp.Body.Close(); err != nil {
			slog.Warn("关闭插件下载响应失败", "url", downloadURL, "error", err)
		}
	}()

	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回状态码 %d", dlResp.StatusCode)
	}

	binary, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return fmt.Errorf("读取下载内容失败: %w", err)
	}

	return m.InstallFromBinary(ctx, repoName, binary)
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
