package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
)

func TestDetectModeInjectedBranches(t *testing.T) {
	t.Run("docker marker", func(t *testing.T) {
		restoreUpgradeHooks(t)
		detectStat = func(string) (os.FileInfo, error) { return nil, nil }
		if !isDocker() || DetectMode() != ModeDocker {
			t.Fatal("docker marker should select docker mode")
		}
	})

	t.Run("docker cgroup", func(t *testing.T) {
		restoreUpgradeHooks(t)
		detectStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
		detectReadFile = func(string) ([]byte, error) { return []byte("0::/containerd/test"), nil }
		if !isDocker() {
			t.Fatal("containerd cgroup should be docker")
		}
	})

	t.Run("systemd and noop", func(t *testing.T) {
		restoreUpgradeHooks(t)
		detectStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
		detectReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
		detectGOOS = "linux"
		detectGetppid = func() int { return 1 }
		detectExecutable = func() (string, error) { return "/usr/bin/airgate-core", nil }
		detectOpenFile = func(string, int, os.FileMode) (*os.File, error) {
			return nil, errors.New("open denied")
		}
		detectCreateTemp = func(string, string) (*os.File, error) {
			return os.CreateTemp(t.TempDir(), "write-*")
		}
		if !isSystemd() || DetectMode() != ModeSystemd {
			t.Fatal("writable linux ppid=1 should be systemd")
		}

		detectGOOS = "windows"
		if isSystemd() {
			t.Fatal("non-linux should not be systemd")
		}
		detectGOOS = "linux"
		detectGetppid = func() int { return 2 }
		if isSystemd() {
			t.Fatal("ppid!=1 should not be systemd")
		}
		detectGetppid = func() int { return 1 }
		detectExecutable = func() (string, error) { return "", errors.New("exe failed") }
		if isSystemd() {
			t.Fatal("executable error should not be systemd")
		}
		detectExecutable = func() (string, error) { return "/usr/bin/airgate-core", nil }
		detectCreateTemp = func(string, string) (*os.File, error) { return nil, errors.New("not writable") }
		if DetectMode() != ModeNoop {
			t.Fatal("no docker and no systemd should be noop")
		}
	})

	t.Run("writable no slash", func(t *testing.T) {
		restoreUpgradeHooks(t)
		detectOpenFile = func(string, int, os.FileMode) (*os.File, error) {
			return nil, errors.New("open denied")
		}
		if isWritable("airgate-core") {
			t.Fatal("path without writable file or parent should be false")
		}
	})
}

func TestGithubClientLatestReleaseHTTPBranches(t *testing.T) {
	restoreUpgradeHooks(t)

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Fatalf("Accept header = %q", r.Header.Get("Accept"))
		}
		if requests == 1 {
			w.Header().Set("ETag", "etag-1")
			_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://example.test","body":"notes","assets":[{"name":"asset","browser_download_url":"https://asset","size":3}]}`))
			return
		}
		if r.Header.Get("If-None-Match") != "etag-1" {
			t.Fatalf("If-None-Match = %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	githubAPIBaseURL = server.URL
	githubHTTPClient = server.Client()

	client := newGithubClient()
	rel, err := client.LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease() error = %v", err)
	}
	if rel.TagName != "v2.0.0" || client.etag != "etag-1" || requests != 1 {
		t.Fatalf("release=%+v etag=%q requests=%d", rel, client.etag, requests)
	}
	if _, err := client.LatestRelease(context.Background()); err != nil || requests != 1 {
		t.Fatalf("cached LatestRelease() err=%v requests=%d", err, requests)
	}
	client.Invalidate()
	if rel, err := client.LatestRelease(context.Background()); err != nil || rel.TagName != "v2.0.0" || requests != 2 {
		t.Fatalf("304 LatestRelease() rel=%+v err=%v requests=%d", rel, err, requests)
	}
}

func TestGithubClientLatestReleaseErrors(t *testing.T) {
	t.Run("bad request url", func(t *testing.T) {
		restoreUpgradeHooks(t)
		githubAPIBaseURL = "://bad-url"
		if _, err := newGithubClient().LatestRelease(context.Background()); err == nil {
			t.Fatal("LatestRelease() error = nil, want request build error")
		}
	})

	t.Run("http error", func(t *testing.T) {
		restoreUpgradeHooks(t)
		githubHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network failed")
		})}
		if _, err := newGithubClient().LatestRelease(context.Background()); err == nil || !strings.Contains(err.Error(), "请求 GitHub API 失败") {
			t.Fatalf("LatestRelease() error = %v", err)
		}
	})

	t.Run("non ok and bad json", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			body string
			code int
		}{
			{name: "non ok", code: http.StatusForbidden, body: `{}`},
			{name: "bad json", code: http.StatusOK, body: `{`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				restoreUpgradeHooks(t)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.code)
					_, _ = w.Write([]byte(tc.body))
				}))
				defer server.Close()
				githubAPIBaseURL = server.URL
				githubHTTPClient = server.Client()
				if _, err := newGithubClient().LatestRelease(context.Background()); err == nil {
					t.Fatal("LatestRelease() error = nil")
				}
			})
		}
	})
}

func TestInfoSystemdNoopAndReleaseFailure(t *testing.T) {
	restoreUpgradeHooks(t)
	upgradeExecutable = func() (string, error) { return "C:/airgate/airgate-core.exe", nil }
	service := NewService(ModeSystemd, nil)
	service.github = &githubClient{cached: &ReleaseInfo{TagName: "v999.0.0", FetchedAt: time.Now()}, cacheExpire: time.Now().Add(time.Hour)}
	info, err := service.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if !info.CanUpgrade || info.BinaryPath == "" {
		t.Fatalf("systemd info = %+v", info)
	}

	service = NewService(ModeNoop, nil)
	service.github = &githubClient{cached: &ReleaseInfo{TagName: "dev", FetchedAt: time.Now()}, cacheExpire: time.Now().Add(time.Hour)}
	info, err = service.Info(context.Background())
	if err != nil || info.CanUpgrade {
		t.Fatalf("noop info = %+v err=%v", info, err)
	}

	githubHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network failed")
	})}
	service = NewService(ModeSystemd, nil)
	info, err = service.Info(context.Background())
	if err != nil || info.Latest != "" {
		t.Fatalf("failed release info = %+v err=%v", info, err)
	}
}

func TestRunPrechecksLocksAndReleaseErrors(t *testing.T) {
	t.Run("busy state", func(t *testing.T) {
		service := NewService(ModeSystemd, nil)
		service.box.store(Status{State: StateDownloading})
		if err := service.Run(context.Background(), RunRequest{ConfirmDBBackup: true}); err == nil || !strings.Contains(err.Error(), "正在进行") {
			t.Fatalf("Run() error = %v", err)
		}
	})

	t.Run("redis lock error and busy", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			err  error
			ok   bool
			want string
		}{
			{name: "error", err: errors.New("redis failed"), want: "获取升级锁失败"},
			{name: "busy", ok: false, want: "已有升级任务正在进行"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rdb, mock := redismock.NewClientMock()
				if tc.err != nil {
					mock.ExpectSetNX(redisLockKey, "1", redisLockTTL).SetErr(tc.err)
				} else {
					mock.ExpectSetNX(redisLockKey, "1", redisLockTTL).SetVal(tc.ok)
				}
				service := NewService(ModeSystemd, rdb)
				err := service.Run(context.Background(), RunRequest{ConfirmDBBackup: true})
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Run() error = %v, want %q", err, tc.want)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatalf("redis expectations: %v", err)
				}
			})
		}
	})

	t.Run("release and asset errors release lock", func(t *testing.T) {
		rdb, mock := redismock.NewClientMock()
		mock.ExpectSetNX(redisLockKey, "1", redisLockTTL).SetVal(true)
		mock.ExpectDel(redisLockKey).SetVal(1)
		service := NewService(ModeSystemd, rdb)
		service.github = &githubClient{}
		githubHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network failed")
		})}
		if err := service.Run(context.Background(), RunRequest{ConfirmDBBackup: true}); err == nil || !strings.Contains(err.Error(), "拉取最新 release 失败") {
			t.Fatalf("Run() release error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}

		rdb, mock = redismock.NewClientMock()
		mock.ExpectSetNX(redisLockKey, "1", redisLockTTL).SetVal(true)
		mock.ExpectDel(redisLockKey).SetVal(1)
		service = NewService(ModeSystemd, rdb)
		service.github = &githubClient{cached: &ReleaseInfo{TagName: "v2.0.0"}, cacheExpire: time.Now().Add(time.Hour)}
		if err := service.Run(context.Background(), RunRequest{ConfirmDBBackup: true}); err == nil || !strings.Contains(err.Error(), "未找到匹配资产") {
			t.Fatalf("Run() asset error = %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("redis expectations: %v", err)
		}
	})
}

func TestRunAsyncSuccessThroughRun(t *testing.T) {
	restoreUpgradeHooks(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "airgate-core")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	content := []byte("new binary")
	sum := sha256.Sum256(content)
	var checksumURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset":
			_, _ = w.Write(content)
		case "/asset.sha256":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  airgate-core\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	checksumURL = server.URL + "/asset.sha256"

	rdb, mock := redismock.NewClientMock()
	mock.ExpectSetNX(redisLockKey, "1", redisLockTTL).SetVal(true)
	mock.ExpectDel(redisLockKey).SetVal(1)

	exitCh := make(chan int, 1)
	upgradeExecutable = func() (string, error) { return exe, nil }
	upgradeEvalSymlinks = func(path string) (string, error) { return path, nil }
	upgradeSmokeTest = func(path string) error { return nil }
	upgradeSleep = func(time.Duration) {}
	upgradeExit = func(code int) { exitCh <- code }

	service := NewService(ModeSystemd, rdb)
	assetName := "airgate-core-" + runtime.GOOS + "-" + runtime.GOARCH
	service.github = &githubClient{
		cached: &ReleaseInfo{
			TagName: "v2.0.0",
			Assets: []Asset{
				{Name: assetName, DownloadURL: server.URL + "/asset", Size: int64(len(content))},
				{Name: assetName + ".sha256", DownloadURL: checksumURL},
			},
		},
		cacheExpire: time.Now().Add(time.Hour),
	}

	if err := service.Run(context.Background(), RunRequest{ConfirmDBBackup: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("upgrade exit was not called")
	}
	if status := service.Status(); status.State != StateRestarting {
		t.Fatalf("status = %+v", status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRunAsyncFailureStages(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *Service, string)
		want  string
	}{
		{name: "executable", setup: func(t *testing.T, _ *Service, _ string) {
			upgradeExecutable = func() (string, error) { return "", errors.New("exe failed") }
		}, want: "无法定位当前 binary 路径"},
		{name: "eval symlink", setup: func(t *testing.T, _ *Service, exe string) {
			upgradeExecutable = func() (string, error) { return exe, nil }
			upgradeEvalSymlinks = func(string) (string, error) { return "", errors.New("eval failed") }
		}, want: "解析 binary 软链失败"},
		{name: "download", setup: func(t *testing.T, _ *Service, exe string) {
			upgradeExecutable = func() (string, error) { return exe, nil }
			upgradeEvalSymlinks = func(path string) (string, error) { return path, nil }
			upgradeHTTPGet = func(string) (*http.Response, error) { return nil, errors.New("download failed") }
		}, want: "下载新版本失败"},
		{name: "checksum", setup: func(t *testing.T, service *Service, exe string) {
			upgradeExecutable = func() (string, error) { return exe, nil }
			upgradeEvalSymlinks = func(path string) (string, error) { return path, nil }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("new")) }))
			t.Cleanup(server.Close)
			service.github = &githubClient{cached: &ReleaseInfo{}, cacheExpire: time.Now().Add(time.Hour)}
			assetForFailure.DownloadURL = server.URL
		}, want: "SHA256 校验失败"},
		{name: "chmod", setup: func(t *testing.T, service *Service, exe string) {
			setupSuccessfulAsyncInputs(t, service, exe)
			upgradeChmod = func(string, os.FileMode) error { return errors.New("chmod failed") }
		}, want: "设置可执行位失败"},
		{name: "smoke", setup: func(t *testing.T, service *Service, exe string) {
			setupSuccessfulAsyncInputs(t, service, exe)
			upgradeSmokeTest = func(string) error { return errors.New("smoke failed") }
		}, want: "新版本 smoke test 失败"},
		{name: "copy", setup: func(t *testing.T, service *Service, exe string) {
			setupSuccessfulAsyncInputs(t, service, exe)
			upgradeCopyFile = func(string, string) error { return errors.New("copy failed") }
		}, want: "备份当前 binary 失败"},
		{name: "rename", setup: func(t *testing.T, service *Service, exe string) {
			setupSuccessfulAsyncInputs(t, service, exe)
			upgradeRename = func(string, string) error { return errors.New("rename failed") }
		}, want: "原子替换 binary 失败，原版本未受影响"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreUpgradeHooks(t)
			dir := t.TempDir()
			exe := filepath.Join(dir, "airgate-core")
			if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
				t.Fatalf("write exe: %v", err)
			}
			service := NewService(ModeSystemd, nil)
			assetForFailure = Asset{Name: "airgate-core-test", DownloadURL: "http://127.0.0.1/unused", Size: 3}
			tt.setup(t, service, exe)
			service.runAsync("v2.0.0", &assetForFailure)
			if status := service.Status(); status.State != StateFailed || !strings.Contains(status.Message, tt.want) {
				t.Fatalf("status = %+v, want failed %q", status, tt.want)
			}
		})
	}
}

func TestDownloadVerifyChecksumSmokeAndCopyErrors(t *testing.T) {
	t.Run("download status and open/read/write errors", func(t *testing.T) {
		restoreUpgradeHooks(t)
		service := NewService(ModeSystemd, nil)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		defer server.Close()
		if err := service.download(&Asset{DownloadURL: server.URL}, filepath.Join(t.TempDir(), "x")); err == nil || !strings.Contains(err.Error(), "HTTP 状态码") {
			t.Fatalf("download status error = %v", err)
		}

		upgradeHTTPGet = func(string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("body")), ContentLength: 4}, nil
		}
		upgradeOpenFile = func(string, int, os.FileMode) (upgradeWriteCloser, error) {
			return nil, errors.New("open failed")
		}
		if err := service.download(&Asset{DownloadURL: "x"}, "dst"); err == nil || !strings.Contains(err.Error(), "open failed") {
			t.Fatalf("download open error = %v", err)
		}
		upgradeOpenFile = func(string, int, os.FileMode) (upgradeWriteCloser, error) {
			return errorWriteCloser{writeErr: errors.New("write failed")}, nil
		}
		if err := service.download(&Asset{DownloadURL: "x"}, "dst"); err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("download write error = %v", err)
		}
		upgradeHTTPGet = func(string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{err: errors.New("read failed")}, ContentLength: 4}, nil
		}
		upgradeOpenFile = func(string, int, os.FileMode) (upgradeWriteCloser, error) {
			return &bufferWriteCloser{}, nil
		}
		if err := service.download(&Asset{DownloadURL: "x"}, "dst"); err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("download read error = %v", err)
		}
	})

	t.Run("verify checksum errors", func(t *testing.T) {
		restoreUpgradeHooks(t)
		service := NewService(ModeSystemd, nil)
		path := filepath.Join(t.TempDir(), "asset")
		if err := os.WriteFile(path, []byte("body"), 0o600); err != nil {
			t.Fatalf("write asset: %v", err)
		}
		service.github = &githubClient{cached: &ReleaseInfo{}, cacheExpire: time.Now().Add(time.Hour)}
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "缺少") {
			t.Fatalf("missing checksum error = %v", err)
		}
		service.github = &githubClient{}
		githubHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("release failed")
		})}
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "release failed") {
			t.Fatalf("release error = %v", err)
		}
		service.github = &githubClient{cached: &ReleaseInfo{Assets: []Asset{{Name: "asset.sha256", DownloadURL: "checksum"}}}, cacheExpire: time.Now().Add(time.Hour)}
		upgradeHTTPGet = func(string) (*http.Response, error) { return nil, errors.New("checksum download failed") }
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "checksum download failed") {
			t.Fatalf("checksum http error = %v", err)
		}
		upgradeHTTPGet = func(string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{err: errors.New("checksum read failed")}}, nil
		}
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "checksum read failed") {
			t.Fatalf("checksum read error = %v", err)
		}
		upgradeHTTPGet = func(string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "无法解析") {
			t.Fatalf("empty checksum error = %v", err)
		}
		upgradeHTTPGet = func(string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("0", 64)))}, nil
		}
		if err := service.verifyChecksum(&Asset{Name: "asset"}, filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatalf("missing file checksum error = nil")
		}
		upgradeOpen = func(string) (upgradeReadCloser, error) {
			return errorReadCloser{err: errors.New("file read failed")}, nil
		}
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "file read failed") {
			t.Fatalf("file read checksum error = %v", err)
		}
		upgradeOpen = defaultUpgradeOpen
		if err := service.verifyChecksum(&Asset{Name: "asset"}, path); err == nil || !strings.Contains(err.Error(), "不匹配") {
			t.Fatalf("mismatch checksum error = %v", err)
		}
	})

	t.Run("smoke and copy errors", func(t *testing.T) {
		restoreUpgradeHooks(t)
		if err := smokeTest(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("smokeTest missing binary error = nil")
		}
		upgradeCommand = smokeHelperCommand("not-core")
		if err := smokeTest("helper"); err == nil || !strings.Contains(err.Error(), "输出异常") {
			t.Fatalf("smokeTest bad output error = %v", err)
		}
		upgradeCommand = smokeHelperCommand("airgate-core v2\n")
		if err := smokeTest("helper"); err != nil {
			t.Fatalf("smokeTest success error = %v", err)
		}

		dir := t.TempDir()
		if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst")); err == nil {
			t.Fatal("copyFile missing source error = nil")
		}
		src := filepath.Join(dir, "src")
		if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
			t.Fatalf("write src: %v", err)
		}
		upgradeOpenFile = func(string, int, os.FileMode) (upgradeWriteCloser, error) {
			return nil, errors.New("open dst failed")
		}
		if err := copyFile(src, filepath.Join(dir, "dst")); err == nil || !strings.Contains(err.Error(), "open dst failed") {
			t.Fatalf("copyFile dst error = %v", err)
		}
		upgradeOpenFile = func(string, int, os.FileMode) (upgradeWriteCloser, error) {
			return errorWriteCloser{writeErr: errors.New("copy write failed")}, nil
		}
		if err := copyFile(src, filepath.Join(dir, "dst")); err == nil || !strings.Contains(err.Error(), "copy write failed") {
			t.Fatalf("copyFile write error = %v", err)
		}
	})
}

var assetForFailure Asset

func setupSuccessfulAsyncInputs(t *testing.T, service *Service, exe string) {
	t.Helper()
	content := []byte("new")
	sum := sha256.Sum256(content)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:])))
			return
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(server.Close)
	assetForFailure = Asset{Name: "airgate-core-test", DownloadURL: server.URL + "/asset", Size: int64(len(content))}
	service.github = &githubClient{cached: &ReleaseInfo{Assets: []Asset{{Name: "airgate-core-test.sha256", DownloadURL: server.URL + "/asset.sha256"}}}, cacheExpire: time.Now().Add(time.Hour)}
	upgradeExecutable = func() (string, error) { return exe, nil }
	upgradeEvalSymlinks = func(path string) (string, error) { return path, nil }
	upgradeSmokeTest = func(string) error { return nil }
}

func restoreUpgradeHooks(t *testing.T) {
	t.Helper()
	prevDetectGOOS := detectGOOS
	prevDetectStat := detectStat
	prevDetectReadFile := detectReadFile
	prevDetectGetppid := detectGetppid
	prevDetectExecutable := detectExecutable
	prevDetectOpenFile := detectOpenFile
	prevDetectCreateTemp := detectCreateTemp
	prevDetectRemove := detectRemove
	prevGithubBase := githubAPIBaseURL
	prevGithubHTTP := githubHTTPClient
	prevHTTPGet := upgradeHTTPGet
	prevExecutable := upgradeExecutable
	prevEval := upgradeEvalSymlinks
	prevRemove := upgradeRemove
	prevChmod := upgradeChmod
	prevRename := upgradeRename
	prevOpen := upgradeOpen
	prevOpenFile := upgradeOpenFile
	prevSmoke := upgradeSmokeTest
	prevCommand := upgradeCommand
	prevCopy := upgradeCopyFile
	prevSleep := upgradeSleep
	prevExit := upgradeExit
	t.Cleanup(func() {
		detectGOOS = prevDetectGOOS
		detectStat = prevDetectStat
		detectReadFile = prevDetectReadFile
		detectGetppid = prevDetectGetppid
		detectExecutable = prevDetectExecutable
		detectOpenFile = prevDetectOpenFile
		detectCreateTemp = prevDetectCreateTemp
		detectRemove = prevDetectRemove
		githubAPIBaseURL = prevGithubBase
		githubHTTPClient = prevGithubHTTP
		upgradeHTTPGet = prevHTTPGet
		upgradeExecutable = prevExecutable
		upgradeEvalSymlinks = prevEval
		upgradeRemove = prevRemove
		upgradeChmod = prevChmod
		upgradeRename = prevRename
		upgradeOpen = prevOpen
		upgradeOpenFile = prevOpenFile
		upgradeSmokeTest = prevSmoke
		upgradeCommand = prevCommand
		upgradeCopyFile = prevCopy
		upgradeSleep = prevSleep
		upgradeExit = prevExit
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func smokeHelperCommand(output string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestUpgradeSmokeHelperProcess")
		cmd.Env = append(os.Environ(), "UPGRADE_SMOKE_HELPER=1", "UPGRADE_SMOKE_OUTPUT="+output)
		return cmd
	}
}

func TestUpgradeSmokeHelperProcess(t *testing.T) {
	if os.Getenv("UPGRADE_SMOKE_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.Write([]byte(os.Getenv("UPGRADE_SMOKE_OUTPUT")))
	os.Exit(0)
}

type errorReadCloser struct {
	err error
}

func (r errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r errorReadCloser) Close() error {
	return nil
}

type errorWriteCloser struct {
	writeErr error
}

func (w errorWriteCloser) Write([]byte) (int, error) {
	return 0, w.writeErr
}

func (w errorWriteCloser) Close() error {
	return nil
}

type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error {
	return nil
}
