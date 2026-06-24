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
	"time"
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
		case "/download-500":
			return pluginJSONResponse(req, http.StatusInternalServerError, `failed`, ""), nil
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

func TestManagerRuntimeFilesystemAndStopEdges(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing")
	mgr := NewManager(missing, "debug", "", nil)
	t.Cleanup(mgr.devWatcher.Close)

	if err := mgr.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll missing dir error = %v", err)
	}
	if err := mgr.LoadDev(ctx, "demo", missing); err == nil {
		t.Fatal("LoadDev missing src error = nil")
	}
	if err := mgr.ReloadDev(ctx, "demo"); err == nil {
		t.Fatal("ReloadDev non-dev error = nil")
	}

	root := t.TempDir()
	mgr.pluginDir = root + string(rune(0))
	if err := mgr.LoadAll(ctx); err == nil || !strings.Contains(err.Error(), "读取插件目录失败") {
		t.Fatalf("LoadAll invalid path error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "not-a-dir"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty-plugin"), 0755); err != nil {
		t.Fatalf("mkdir plugin: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "bad-plugin"), 0755); err != nil {
		t.Fatalf("mkdir bad plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bad-plugin", "bad-plugin"), []byte("not an executable plugin"), 0644); err != nil {
		t.Fatalf("write bad plugin binary: %v", err)
	}
	mgr.pluginDir = root
	if err := mgr.LoadAll(ctx); err != nil {
		t.Fatalf("LoadAll empty entries error = %v", err)
	}
	if err := mgr.waitForPluginStop(ctx, " "); err != nil {
		t.Fatalf("waitForPluginStop blank name = %v", err)
	}

	done := make(chan struct{})
	mgr.mu.Lock()
	mgr.stopping["demo"] = done
	mgr.mu.Unlock()
	close(done)
	mgr.stopPlugin("demo")

	blocked := make(chan struct{})
	mgr.mu.Lock()
	mgr.stopping["blocked"] = blocked
	mgr.mu.Unlock()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := mgr.waitForPluginStop(cancelCtx, "blocked"); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForPluginStop canceled error = %v", err)
	}

	finish := make(chan struct{})
	mgr.mu.Lock()
	mgr.stopping["finish"] = finish
	mgr.mu.Unlock()
	mgr.finishPluginStop([]string{"finish", "missing"}, finish)
	select {
	case <-finish:
	default:
		t.Fatal("finishPluginStop did not close done channel")
	}
	if got := mgr.stopping["finish"]; got != nil {
		t.Fatalf("stopping entry after finish = %v", got)
	}

	keys := pluginStopKeys(" requested ", "canonical", &PluginInstance{Name: "canonical", SourceName: "requested", BinaryDir: "bin"})
	if strings.Join(keys, ",") != "requested,canonical,bin" {
		t.Fatalf("pluginStopKeys = %v", keys)
	}

	stoppedBackground := false
	mgr.mu.Lock()
	mgr.instances["demo"] = &PluginInstance{
		Name:           "demo",
		SourceName:     "requested",
		BinaryDir:      "bin",
		Platform:       "openai",
		stopBackground: func() { stoppedBackground = true },
	}
	mgr.modelCache["openai"] = nil
	mgr.routeCache["demo"] = nil
	mgr.credCache["openai"] = nil
	mgr.accountTypeCache["openai"] = nil
	mgr.frontendPageCache["demo"] = nil
	mgr.hostHandles["demo"] = &pluginHostHandle{pluginName: "demo"}
	mgr.aliases["requested"] = "demo"
	mgr.devPaths["demo"] = root
	mgr.mu.Unlock()
	mgr.stopPlugin("requested")
	if !stoppedBackground {
		t.Fatal("stopPlugin did not call stopBackground")
	}
	if mgr.instances["demo"] != nil || mgr.modelCache["openai"] != nil || mgr.hostHandles["demo"] != nil {
		t.Fatalf("stopPlugin left runtime cache entries: instances=%+v models=%+v handles=%+v", mgr.instances, mgr.modelCache, mgr.hostHandles)
	}
	mgr.StopAll(ctx)
}

func TestStopPluginRuntimeDoesNotBeginDrain(t *testing.T) {
	mgr := &Manager{}
	inst := &PluginInstance{Name: "demo"}

	mgr.stopPluginRuntime(inst, nil, pluginStopDrainTimeout)

	inst.lifecycleMu.Lock()
	draining := inst.draining
	inst.lifecycleMu.Unlock()
	if draining {
		t.Fatal("stopPluginRuntime should use caller-provided drain state")
	}
}

func TestStopPluginDrainTimeoutIsImmediate(t *testing.T) {
	idle := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- waitPluginDrain(nil, idle, pluginStopDrainTimeout)
	}()

	select {
	case drained := <-done:
		if drained {
			t.Fatal("open idle channel should not drain with immediate stop timeout")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("immediate stop timeout blocked waiting for drain")
	}

	close(idle)
	if !waitPluginDrain(nil, idle, pluginStopDrainTimeout) {
		t.Fatal("closed idle channel should report drained")
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
		BinaryDir:      "binary-dir",
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
	if _, err := os.Stat(canonicalDir); !os.IsNotExist(err) {
		t.Fatalf("canonical dir stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(binaryDir); !os.IsNotExist(err) {
		t.Fatalf("binary dir stat error = %v, want not exist", err)
	}
	mgr.mu.RLock()
	_, devPathLeft := mgr.devPaths["canonical"]
	mgr.mu.RUnlock()
	if devPathLeft {
		t.Fatal("Uninstall left dev path entry")
	}

	mgr.pluginDir = root + string(rune(0))
	if err := mgr.Uninstall(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "删除插件目录失败") {
		t.Fatalf("Uninstall invalid path error = %v", err)
	}
}

func TestInstallFromBinaryStopsBeforeRealStartOnFilesystemError(t *testing.T) {
	mgr := NewManager(t.TempDir()+string(rune(0)), "debug", "", nil)
	t.Cleanup(mgr.devWatcher.Close)

	err := mgr.InstallFromBinary(context.Background(), "fallback-name", []byte("not an executable plugin"))
	if err == nil || !strings.Contains(err.Error(), "创建插件目录失败") {
		t.Fatalf("InstallFromBinary invalid plugin dir error = %v", err)
	}
}

func TestProbeAndRestorePreviousBinaryFilesystemErrors(t *testing.T) {
	mgr := NewManager(t.TempDir(), "debug", "", nil)
	t.Cleanup(mgr.devWatcher.Close)

	if _, err := mgr.probePluginName("bad"+string(rune(0))+"name", []byte("x")); err == nil || !strings.Contains(err.Error(), "写入临时二进制失败") {
		t.Fatalf("probePluginName invalid fallback error = %v", err)
	}

	err := mgr.restorePreviousBinary(context.Background(), "demo", filepath.Join(t.TempDir()+string(rune(0)), "demo"), []byte("previous"))
	if err == nil || !strings.Contains(err.Error(), "写回旧插件二进制失败") {
		t.Fatalf("restorePreviousBinary invalid path error = %v", err)
	}
}
