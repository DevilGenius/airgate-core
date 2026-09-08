package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallFromBinaryWithSHA256ValidationEdges(t *testing.T) {
	mgr := &Manager{pluginDir: t.TempDir()}
	binary := []byte("plugin-binary")

	if got := calcBinarySHA256(binary); got == "" || len(got) != 64 {
		t.Fatalf("calcBinarySHA256() = %q", got)
	}
	if err := mgr.InstallFromBinaryWithSHA256(context.Background(), "demo", binary, "not-a-sha"); err == nil {
		t.Fatal("InstallFromBinaryWithSHA256 invalid sha error = nil")
	}
	if err := mgr.InstallFromBinaryWithSHA256(context.Background(), "demo", binary, strings.Repeat("0", 64)); err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("InstallFromBinaryWithSHA256 mismatch error = %v", err)
	}
}

func TestReadPluginBinaryEnforcesLimit(t *testing.T) {
	got, err := readPluginBinary(strings.NewReader("12345"), 5)
	if err != nil || string(got) != "12345" {
		t.Fatalf("readPluginBinary exact limit = %q/%v", got, err)
	}
	if _, err := readPluginBinary(strings.NewReader("123456"), 5); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("readPluginBinary over limit error = %v", err)
	}
}

func TestParseGithubRepoEdges(t *testing.T) {
	tests := []struct {
		raw       string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{raw: " owner/repo/ ", wantOwner: "owner", wantName: "repo"},
		{raw: "https://github.com/owner/repo", wantOwner: "owner", wantName: "repo"},
		{raw: "https://github.com/owner/repo.git/", wantOwner: "owner", wantName: "repo"},
		{raw: "", wantErr: true},
		{raw: "owner", wantErr: true},
		{raw: "owner/repo/extra", wantErr: true},
		{raw: "https://example.com/owner/repo", wantErr: true},
		{raw: "git@github.com:owner/repo.git", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			owner, name, err := parseGithubRepo(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseGithubRepo(%q) error = nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGithubRepo(%q) error = %v", tt.raw, err)
			}
			if owner != tt.wantOwner || name != tt.wantName {
				t.Fatalf("parseGithubRepo(%q) = %s/%s, want %s/%s", tt.raw, owner, name, tt.wantOwner, tt.wantName)
			}
		})
	}
}

func TestFetchGithubReleaseForInstallFallbacksAndErrors(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = pluginRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/acme/plugin/releases/tags/1.2.3":
			return pluginJSONResponse(req, http.StatusNotFound, `{}`, ""), nil
		case "/repos/acme/plugin/releases/tags/v1.2.3":
			return pluginJSONResponse(req, http.StatusOK, `{"tag_name":"v1.2.3","assets":[]}`, ""), nil
		case "/repos/acme/plugin/releases/latest":
			return pluginJSONResponse(req, http.StatusNotFound, `{}`, ""), nil
		case "/repos/acme/plugin/releases/tags/bad":
			return pluginJSONResponse(req, http.StatusInternalServerError, `{}`, ""), nil
		case "/repos/acme/plugin/releases/tags/invalid-json":
			return pluginJSONResponse(req, http.StatusOK, `{`, ""), nil
		default:
			t.Fatalf("unexpected GitHub path %s", req.URL.Path)
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	release, err := fetchGithubReleaseForInstall(context.Background(), "acme", "plugin", "1.2.3")
	if err != nil {
		t.Fatalf("fetchGithubReleaseForInstall fallback error = %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("release tag = %q, want v1.2.3", release.TagName)
	}

	if _, err := fetchGithubReleaseForInstall(context.Background(), "acme", "plugin", ""); err == nil || !strings.Contains(err.Error(), "没有 Release") {
		t.Fatalf("latest missing error = %v", err)
	}
	if _, err := fetchGithubReleaseForInstall(context.Background(), "acme", "plugin", "bad"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("non-OK release error = %v", err)
	}
	if _, _, err := fetchGithubReleaseByURL(context.Background(), "https://api.github.com/repos/acme/plugin/releases/tags/invalid-json"); err == nil {
		t.Fatal("invalid JSON release error = nil")
	}
}

func TestFetchGithubReleaseByURLTransportError(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = pluginRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	if _, status, err := fetchGithubReleaseByURL(context.Background(), "https://api.github.com/repos/acme/plugin/releases/latest"); err == nil || status != 0 {
		t.Fatalf("transport error status=%d err=%v", status, err)
	}
}

func TestInstallFromGithubValidationErrorsBeforeProcessStart(t *testing.T) {
	binary := []byte("downloaded-plugin")
	wrongSHA := strings.Repeat("0", 64)
	assetName := fmt.Sprintf("airgate-plugin-%s-%s", runtime.GOOS, runtime.GOARCH)

	previousTransport := http.DefaultTransport
	http.DefaultTransport = pluginRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/acme/missing-asset/releases/latest":
			return pluginJSONResponse(req, http.StatusOK, `{"tag_name":"v1.0.0","assets":[]}`, ""), nil
		case "/repos/acme/download-500/releases/latest":
			return pluginJSONResponse(req, http.StatusOK, fmt.Sprintf(`{"tag_name":"v1.0.0","assets":[{"name":%q,"browser_download_url":"https://downloads.test/download-500"}]}`, assetName), ""), nil
		case "/repos/acme/no-sha/releases/latest":
			return pluginJSONResponse(req, http.StatusOK, fmt.Sprintf(`{"tag_name":"v1.0.0","assets":[{"name":%q,"browser_download_url":"https://downloads.test/no-sha"}]}`, assetName), ""), nil
		case "/repos/acme/mismatch/releases/latest":
			return pluginJSONResponse(req, http.StatusOK, fmt.Sprintf(`{"tag_name":"v1.0.0","assets":[{"name":%q,"browser_download_url":"https://downloads.test/mismatch","digest":"sha256:%s"}]}`, assetName, wrongSHA), ""), nil
		case "/repos/acme/asset-too-large/releases/latest":
			return pluginJSONResponse(req, http.StatusOK, fmt.Sprintf(`{"tag_name":"v1.0.0","assets":[{"name":%q,"browser_download_url":"https://downloads.test/asset-too-large","size":%d,"digest":"sha256:%s"}]}`, assetName, MaxPluginBinarySize+1, wrongSHA), ""), nil
		case "/repos/acme/content-too-large/releases/latest":
			return pluginJSONResponse(req, http.StatusOK, fmt.Sprintf(`{"tag_name":"v1.0.0","assets":[{"name":%q,"browser_download_url":"https://downloads.test/content-too-large","digest":"sha256:%s"}]}`, assetName, wrongSHA), ""), nil
		case "/download-500":
			return pluginJSONResponse(req, http.StatusInternalServerError, `failed`, ""), nil
		case "/content-too-large":
			resp := pluginJSONResponse(req, http.StatusOK, "", "")
			resp.ContentLength = MaxPluginBinarySize + 1
			return resp, nil
		case "/no-sha", "/mismatch":
			return pluginJSONResponse(req, http.StatusOK, string(binary), ""), nil
		default:
			t.Fatalf("unexpected install request %s", req.URL.String())
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	mgr := &Manager{pluginDir: t.TempDir()}
	cases := []struct {
		repo string
		want string
	}{
		{repo: "acme/missing-asset", want: "未找到适配"},
		{repo: "acme/download-500", want: "下载返回状态码 500"},
		{repo: "acme/no-sha", want: "缺少 SHA256"},
		{repo: "acme/mismatch", want: "SHA256"},
		{repo: "acme/asset-too-large", want: "超过"},
		{repo: "acme/content-too-large", want: "超过"},
	}
	for _, tc := range cases {
		t.Run(tc.repo, func(t *testing.T) {
			err := mgr.InstallFromGithub(context.Background(), tc.repo, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("InstallFromGithub(%s) error = %v, want contain %q", tc.repo, err, tc.want)
			}
		})
	}
}

func TestManagerInstallLocalFilesystemEdges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mgr := NewManager(root, "debug", "", nil)
	t.Cleanup(mgr.devWatcher.Close)

	canonicalDir := filepath.Join(root, "canonical")
	binaryDir := filepath.Join(root, "binary-dir")
	if err := os.MkdirAll(canonicalDir, 0755); err != nil {
		t.Fatalf("mkdir canonical: %v", err)
	}
	if err := os.MkdirAll(binaryDir, 0755); err != nil {
		t.Fatalf("mkdir binary dir: %v", err)
	}
	stoppedBackground := false
	mgr.mu.Lock()
	mgr.instances["canonical"] = &PluginInstance{
		Name:           "canonical",
		SourceName:     "alias",
		Platform:       "openai",
		stopBackground: func() { stoppedBackground = true },
	}
	mgr.aliases["alias"] = "canonical"
	mgr.devPaths["canonical"] = filepath.Join(root, "src")
	mgr.mu.Unlock()

	if err := mgr.Uninstall(ctx, "alias"); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !stoppedBackground {
		t.Fatal("Uninstall did not stop background work")
	}
	if _, err := os.Stat(canonicalDir); err != nil {
		t.Fatalf("canonical dir stat error = %v, unrelated directory should remain", err)
	}
	if _, err := os.Stat(binaryDir); err != nil {
		t.Fatalf("binary dir stat error = %v, want not exist", err)
	}
	mgr.mu.RLock()
	_, devPathLeft := mgr.devPaths["canonical"]
	mgr.mu.RUnlock()
	if devPathLeft {
		t.Fatal("Uninstall left dev path entry")
	}

	mgr.pluginDir = root + string(rune(0))
	if err := mgr.Uninstall(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "删除插件清单失败") {
		t.Fatalf("Uninstall invalid path error = %v", err)
	}
}

func TestInstallFromBinaryPreservesActiveOnFilesystemError(t *testing.T) {
	mgr := NewManager(t.TempDir()+string(rune(0)), "debug", "", nil)
	t.Cleanup(mgr.devWatcher.Close)

	err := mgr.InstallFromBinary(context.Background(), "fallback-name", []byte("not an executable plugin"))
	if err == nil || !strings.Contains(err.Error(), "创建插件版本目录失败") {
		t.Fatalf("InstallFromBinary invalid plugin dir error = %v", err)
	}
}
