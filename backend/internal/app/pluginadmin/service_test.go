package pluginadmin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/plugin"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
)

func TestReloadRejectsNonDevPlugin(t *testing.T) {
	service := NewService(pluginAdminManagerStub{}, pluginMarketplaceStub{})
	if err := service.Reload(t.Context(), "demo"); err != ErrPluginNotDev {
		t.Fatalf("Reload() error = %v, want %v", err, ErrPluginNotDev)
	}
}

func TestListMarketplaceMarksInstalled(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "gateway-openai", Version: "0.2.1"}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{Name: "gateway-openai"}, {Name: "gateway-gemini"}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 2 || !items[0].Installed || items[1].Installed {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
	if items[0].InstalledVersion != "0.2.1" {
		t.Fatalf("InstalledVersion = %q, want 0.2.1", items[0].InstalledVersion)
	}
}

func TestListDisplaysDevPluginVersionAsDev(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "gateway-openai", Version: "0.2.1", IsDev: true}},
	}, pluginMarketplaceStub{})

	items := service.List()
	if len(items) != 1 || items[0].Version != "dev" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestListCopiesMetadataAndHandlesNilMarketplace(t *testing.T) {
	metadata := map[string]string{"tier": "prod"}
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:     "gateway-openai",
			Version:  "0.2.1",
			Metadata: metadata,
		}},
	}, nil)

	items := service.List()
	if len(items) != 1 || items[0].Metadata["tier"] != "prod" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
	items[0].Metadata["tier"] = "changed"
	if metadata["tier"] != "prod" {
		t.Fatalf("List() returned aliased metadata map")
	}
}

func TestListDisplaysTaggedPluginWithShortHash(t *testing.T) {
	commitSHA := "1234567890abcdef1234567890abcdef12345678"
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: strings.Repeat("a", 64),
			CommitSHA:    commitSHA,
		}},
	}, pluginMarketplaceStub{})

	items := service.List()
	if len(items) != 1 || items[0].Version != "0.2.1-1234567" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestListFallsBackWhenMarketplaceLookupFails(t *testing.T) {
	hash := strings.Repeat("a", 64)
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: hash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return nil, errors.New("marketplace unavailable")
		},
	})

	items := service.List()
	if len(items) != 1 || items[0].Version != "0.2.1-aaaaaaa" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestListSkipsInvalidMarketplaceReleaseLookupEntries(t *testing.T) {
	hash := strings.Repeat("a", 64)
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: hash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{
				{Name: "gateway-openai", Version: "0.2.1", SHA256: hash, CommitSHA: "abc"},
				{Name: "gateway-openai", Version: "0.2.1", SHA256: "not-a-hash", CommitSHA: strings.Repeat("b", 40)},
			}, nil
		},
	})

	items := service.List()
	if len(items) != 1 || items[0].Version != "0.2.1-aaaaaaa" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestListDerivesInstalledCommitFromMatchingMarketplaceAsset(t *testing.T) {
	assetHash := strings.Repeat("a", 64)
	commitSHA := "abcdef1234567890abcdef1234567890abcdef12"
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: assetHash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:      "gateway-openai",
				Version:   "0.2.1",
				SHA256:    assetHash,
				CommitSHA: commitSHA,
			}}, nil
		},
	})

	items := service.List()
	if len(items) != 1 || items[0].Version != "0.2.1-abcdef1" {
		t.Fatalf("unexpected plugin list: %+v", items)
	}
}

func TestIsLoadingDelegatesToManager(t *testing.T) {
	service := NewService(pluginAdminManagerStub{loading: true}, nil)
	if !service.IsLoading() {
		t.Fatalf("IsLoading() = false, want true")
	}
}

func TestGetConfigDelegatesToManager(t *testing.T) {
	want := map[string]string{"api_key": "secret"}
	service := NewService(pluginAdminManagerStub{
		getPluginConfig: func(_ context.Context, name string) (map[string]string, error) {
			if name != "gateway-openai" {
				t.Fatalf("GetPluginConfig name = %q", name)
			}
			return want, nil
		},
	}, nil)

	got, err := service.GetConfig(t.Context(), "gateway-openai")
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if got["api_key"] != "secret" {
		t.Fatalf("GetConfig() = %+v", got)
	}
}

func TestGetConfigReturnsManagerError(t *testing.T) {
	wantErr := errors.New("config failed")
	service := NewService(pluginAdminManagerStub{
		getPluginConfig: func(context.Context, string) (map[string]string, error) {
			return nil, wantErr
		},
	}, nil)

	if _, err := service.GetConfig(t.Context(), "gateway-openai"); err != wantErr {
		t.Fatalf("GetConfig() error = %v, want %v", err, wantErr)
	}
}

func TestUpdateConfigSuccess(t *testing.T) {
	calls := make([]string, 0, 2)
	service := NewService(pluginAdminManagerStub{
		updatePluginConfig: func(_ context.Context, name string, config map[string]string) error {
			if name != "gateway-openai" || config["api_key"] != "secret" {
				t.Fatalf("UpdatePluginConfig args = %q %+v", name, config)
			}
			calls = append(calls, "update")
			return nil
		},
		reloadInstance: func(_ context.Context, name string) error {
			if name != "gateway-openai" {
				t.Fatalf("ReloadInstance name = %q", name)
			}
			calls = append(calls, "reload")
			return nil
		},
	}, nil)

	if err := service.UpdateConfig(t.Context(), "gateway-openai", map[string]string{"api_key": "secret"}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if strings.Join(calls, ",") != "update,reload" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestUpdateConfigReturnsUpdateError(t *testing.T) {
	wantErr := errors.New("update failed")
	reloaded := false
	service := NewService(pluginAdminManagerStub{
		updatePluginConfig: func(context.Context, string, map[string]string) error {
			return wantErr
		},
		reloadInstance: func(context.Context, string) error {
			reloaded = true
			return nil
		},
	}, nil)

	if err := service.UpdateConfig(t.Context(), "gateway-openai", nil); err != wantErr {
		t.Fatalf("UpdateConfig() error = %v, want %v", err, wantErr)
	}
	if reloaded {
		t.Fatalf("ReloadInstance should not be called after update failure")
	}
}

func TestUpdateConfigReturnsReloadError(t *testing.T) {
	wantErr := errors.New("reload failed")
	service := NewService(pluginAdminManagerStub{
		updatePluginConfig: func(context.Context, string, map[string]string) error {
			return nil
		},
		reloadInstance: func(context.Context, string) error {
			return wantErr
		},
	}, nil)

	if err := service.UpdateConfig(t.Context(), "gateway-openai", nil); err != wantErr {
		t.Fatalf("UpdateConfig() error = %v, want %v", err, wantErr)
	}
}

func TestUploadRequiresValidSHA256(t *testing.T) {
	called := false
	service := NewService(pluginAdminManagerStub{
		installFromBinaryWithSHA256: func(context.Context, string, []byte, string) error {
			called = true
			return nil
		},
	}, nil)

	if err := service.Upload(t.Context(), "gateway-openai", []byte("binary"), "not-a-sha"); err == nil {
		t.Fatalf("Upload() error = nil, want invalid checksum error")
	}
	if called {
		t.Fatalf("InstallFromBinaryWithSHA256 should not be called for invalid checksum")
	}
}

func TestUploadDelegatesWithNormalizedSHAAndCopiedBinary(t *testing.T) {
	hash := strings.Repeat("a", 64)
	binary := []byte{1, 2, 3}
	service := NewService(pluginAdminManagerStub{
		installFromBinaryWithSHA256: func(_ context.Context, name string, gotBinary []byte, gotHash string) error {
			if name != "gateway-openai" || gotHash != hash {
				t.Fatalf("InstallFromBinaryWithSHA256 args = %q %q", name, gotHash)
			}
			gotBinary[0] = 9
			return nil
		},
	}, nil)

	if err := service.Upload(t.Context(), "gateway-openai", binary, "SHA256:"+hash+" plugin.bin"); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if binary[0] != 1 {
		t.Fatalf("Upload() passed caller binary without copying")
	}
}

func TestUploadReturnsManagerError(t *testing.T) {
	wantErr := errors.New("install failed")
	service := NewService(pluginAdminManagerStub{
		installFromBinaryWithSHA256: func(context.Context, string, []byte, string) error {
			return wantErr
		},
	}, nil)

	if err := service.Upload(t.Context(), "gateway-openai", []byte("binary"), strings.Repeat("a", 64)); err != wantErr {
		t.Fatalf("Upload() error = %v, want %v", err, wantErr)
	}
}

func TestInstallFromGithubTrimsVersionAndDelegates(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		installFromGithub: func(_ context.Context, repo, version string) error {
			if repo != "DevilGenius/airgate-openai" || version != "v1.2.3" {
				t.Fatalf("InstallFromGithub args = %q %q", repo, version)
			}
			return nil
		},
	}, nil)

	if err := service.InstallFromGithub(t.Context(), "DevilGenius/airgate-openai", " v1.2.3 "); err != nil {
		t.Fatalf("InstallFromGithub() error = %v", err)
	}
}

func TestInstallFromGithubReturnsManagerError(t *testing.T) {
	wantErr := errors.New("github failed")
	service := NewService(pluginAdminManagerStub{
		installFromGithub: func(context.Context, string, string) error {
			return wantErr
		},
	}, nil)

	if err := service.InstallFromGithub(t.Context(), "repo", "v1"); err != wantErr {
		t.Fatalf("InstallFromGithub() error = %v, want %v", err, wantErr)
	}
}

func TestUninstallDelegatesToManager(t *testing.T) {
	called := false
	service := NewService(pluginAdminManagerStub{
		uninstall: func(_ context.Context, name string) error {
			if name != "gateway-openai" {
				t.Fatalf("Uninstall name = %q", name)
			}
			called = true
			return nil
		},
	}, nil)

	if err := service.Uninstall(t.Context(), "gateway-openai"); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !called {
		t.Fatalf("Uninstall manager method was not called")
	}
}

func TestUninstallReturnsManagerError(t *testing.T) {
	wantErr := errors.New("uninstall failed")
	service := NewService(pluginAdminManagerStub{
		uninstall: func(context.Context, string) error {
			return wantErr
		},
	}, nil)

	if err := service.Uninstall(t.Context(), "gateway-openai"); err != wantErr {
		t.Fatalf("Uninstall() error = %v, want %v", err, wantErr)
	}
}

func TestListMarketplaceDoesNotOfferUpdatesForDevPlugin(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "airgate-playground", Version: "0.1.0", IsDev: true}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{Name: "airgate-playground", Version: "0.1.10"}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
}

func TestListMarketplaceReturnsReaderError(t *testing.T) {
	wantErr := errors.New("list failed")
	service := NewService(pluginAdminManagerStub{}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return nil, wantErr
		},
	})

	if _, err := service.ListMarketplace(t.Context()); err != wantErr {
		t.Fatalf("ListMarketplace() error = %v, want %v", err, wantErr)
	}
}

func TestListMarketplaceOffersUpdateForNewerVersion(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{Name: "gateway-openai", Version: "0.2.1"}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{Name: "gateway-openai", Version: "0.2.2"}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
}

func TestListMarketplaceOffersUpdateForSameVersionDifferentHash(t *testing.T) {
	installedHash := strings.Repeat("a", 64)
	latestHash := strings.Repeat("b", 64)
	latestCommit := strings.Repeat("c", 40)
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: installedHash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:      "gateway-openai",
				Version:   "0.2.1",
				SHA256:    "sha256:" + latestHash,
				CommitSHA: latestCommit,
			}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || !items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
	if items[0].Version != "0.2.1" || items[0].DisplayVersion != "0.2.1-ccccccc" {
		t.Fatalf("unexpected market versions: version=%q display=%q", items[0].Version, items[0].DisplayVersion)
	}
	if items[0].InstalledVersion != "0.2.1-aaaaaaa" {
		t.Fatalf("InstalledVersion = %q", items[0].InstalledVersion)
	}
}

func TestListMarketplaceDoesNotOfferUpdateForSameVersionSameHash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	commitSHA := "fedcba9876543210fedcba9876543210fedcba98"
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.2.1",
			BinarySHA256: hash,
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:      "gateway-openai",
				Version:   "0.2.1",
				SHA256:    hash,
				CommitSHA: commitSHA,
			}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
	if items[0].InstalledVersion != "0.2.1-fedcba9" || items[0].DisplayVersion != "0.2.1-fedcba9" {
		t.Fatalf("unexpected versions: installed=%q display=%q", items[0].InstalledVersion, items[0].DisplayVersion)
	}
}

func TestListMarketplaceDoesNotOfferHashUpdateForDifferentVersion(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:         "gateway-openai",
			Version:      "0.3.0",
			BinarySHA256: strings.Repeat("a", 64),
		}},
	}, pluginMarketplaceStub{
		listAvailable: func(context.Context) ([]plugin.MarketplacePlugin, error) {
			return []plugin.MarketplacePlugin{{
				Name:    "gateway-openai",
				Version: "0.2.1",
				SHA256:  strings.Repeat("b", 64),
			}}, nil
		},
	})

	items, err := service.ListMarketplace(t.Context())
	if err != nil {
		t.Fatalf("ListMarketplace() error = %v", err)
	}
	if len(items) != 1 || !items[0].Installed || items[0].HasUpdate {
		t.Fatalf("unexpected marketplace items: %+v", items)
	}
}

func TestReloadDevPlugin(t *testing.T) {
	called := false
	service := NewService(pluginAdminManagerStub{
		isDev: true,
		reloadDev: func(_ context.Context, name string) error {
			if name != "demo" {
				t.Fatalf("ReloadDev name = %q", name)
			}
			called = true
			return nil
		},
	}, nil)

	if err := service.Reload(t.Context(), "demo"); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !called {
		t.Fatalf("ReloadDev was not called")
	}
}

func TestReloadReturnsReloadError(t *testing.T) {
	wantErr := errors.New("reload dev failed")
	service := NewService(pluginAdminManagerStub{
		isDev: true,
		reloadDev: func(context.Context, string) error {
			return wantErr
		},
	}, nil)

	if err := service.Reload(t.Context(), "demo"); err != wantErr {
		t.Fatalf("Reload() error = %v, want %v", err, wantErr)
	}
}

func TestProxyReturnsUnavailableWhenInstanceMissing(t *testing.T) {
	service := NewService(pluginAdminManagerStub{}, nil)
	if _, err := service.Proxy(t.Context(), ProxyInput{Name: "gateway-openai"}); err != ErrPluginUnavailable {
		t.Fatalf("Proxy() error = %v, want %v", err, ErrPluginUnavailable)
	}
}

func TestProxyReturnsUnavailableWhenGatewayMissing(t *testing.T) {
	service := NewService(pluginAdminManagerStub{
		instance: &plugin.PluginInstance{},
	}, nil)

	if _, err := service.Proxy(t.Context(), ProxyInput{Name: "gateway-openai"}); err != ErrPluginUnavailable {
		t.Fatalf("Proxy() error = %v, want %v", err, ErrPluginUnavailable)
	}
}

func TestProxyDelegatesToGateway(t *testing.T) {
	restore := replacePluginAdminProxyHandler(func(gateway *sdkgrpc.GatewayGRPCClient, _ context.Context, method, path, query string, headers http.Header, body []byte) (int, http.Header, []byte, error) {
		if gateway == nil {
			t.Fatalf("gateway = nil")
		}
		if method != http.MethodPost || path != "admin/action" || query != "dry_run=true" {
			t.Fatalf("request route = %s %s?%s", method, path, query)
		}
		if headers.Get("X-Request") != "one" || string(body) != "payload" {
			t.Fatalf("request payload = headers=%v body=%q", headers, body)
		}
		return http.StatusAccepted, http.Header{"X-Plugin": []string{"ok"}}, []byte("accepted"), nil
	})
	t.Cleanup(restore)
	service := NewService(pluginAdminManagerStub{
		instance: &plugin.PluginInstance{Gateway: &sdkgrpc.GatewayGRPCClient{}},
	}, nil)

	result, err := service.Proxy(t.Context(), ProxyInput{
		Name:    "gateway-openai",
		Method:  http.MethodPost,
		Action:  "admin/action",
		Query:   "dry_run=true",
		Headers: http.Header{"X-Request": []string{"one"}},
		Body:    []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if result.StatusCode != http.StatusAccepted || result.Headers.Get("X-Plugin") != "ok" || string(result.Body) != "accepted" {
		t.Fatalf("Proxy() = %+v", result)
	}
}

func TestProxyReturnsGatewayError(t *testing.T) {
	wantErr := errors.New("gateway failed")
	restore := replacePluginAdminProxyHandler(func(*sdkgrpc.GatewayGRPCClient, context.Context, string, string, string, http.Header, []byte) (int, http.Header, []byte, error) {
		return 0, nil, nil, wantErr
	})
	t.Cleanup(restore)
	service := NewService(pluginAdminManagerStub{
		instance: &plugin.PluginInstance{Gateway: &sdkgrpc.GatewayGRPCClient{}},
	}, nil)

	if _, err := service.Proxy(t.Context(), ProxyInput{Name: "gateway-openai"}); err != wantErr {
		t.Fatalf("Proxy() error = %v, want %v", err, wantErr)
	}
}

func TestRefreshMarketplaceDelegates(t *testing.T) {
	wantErr := errors.New("sync failed")
	called := false
	service := NewService(pluginAdminManagerStub{}, pluginMarketplaceStub{
		syncFromGithub: func(context.Context) error {
			called = true
			return wantErr
		},
	})

	if err := service.RefreshMarketplace(t.Context()); err != wantErr {
		t.Fatalf("RefreshMarketplace() error = %v, want %v", err, wantErr)
	}
	if !called {
		t.Fatalf("SyncFromGithub was not called")
	}
}

func TestIsNewerVersionEdges(t *testing.T) {
	tests := []struct {
		name           string
		marketplaceVer string
		installedVer   string
		want           bool
	}{
		{name: "empty marketplace", marketplaceVer: "", installedVer: "1.0.0"},
		{name: "equal after trimming v", marketplaceVer: "v1.0.0", installedVer: "1.0.0"},
		{name: "numeric newer", marketplaceVer: "1.2.0", installedVer: "1.1.9", want: true},
		{name: "numeric older", marketplaceVer: "1.0.0", installedVer: "1.1.0"},
		{name: "string newer", marketplaceVer: "1.beta", installedVer: "1.alpha", want: true},
		{name: "missing segment older", marketplaceVer: "1.0", installedVer: "1.0.1"},
		{name: "numeric equivalent strings", marketplaceVer: "1.02", installedVer: "1.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewerVersion(tt.marketplaceVer, tt.installedVer); got != tt.want {
				t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tt.marketplaceVer, tt.installedVer, got, tt.want)
			}
		})
	}
}

func TestNormalizeSHA256ForCompareEdges(t *testing.T) {
	hash := strings.Repeat("a", 64)
	if got := normalizeSHA256ForCompare(" SHA256:" + hash + " plugin.bin "); got != hash {
		t.Fatalf("normalizeSHA256ForCompare() = %q, want %q", got, hash)
	}
	if got := normalizeSHA256ForCompare("abc"); got != "" {
		t.Fatalf("normalizeSHA256ForCompare(short) = %q, want empty", got)
	}
	if got := normalizeSHA256ForCompare(strings.Repeat("g", 64)); got != "" {
		t.Fatalf("normalizeSHA256ForCompare(invalid hex) = %q, want empty", got)
	}
}

func TestShortHashEdges(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "abcdef", want: ""},
		{value: "abcxyz1", want: ""},
		{value: "abcdef0zzzz", want: "abcdef0"},
		{value: "SHA256:" + strings.Repeat("A", 64) + " plugin.bin", want: "aaaaaaa"},
	}
	for _, tt := range tests {
		if got := shortHash(tt.value); got != tt.want {
			t.Fatalf("shortHash(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func replacePluginAdminProxyHandler(handler func(*sdkgrpc.GatewayGRPCClient, context.Context, string, string, string, http.Header, []byte) (int, http.Header, []byte, error)) func() {
	previous := pluginAdminProxyHandleHTTPRequest
	pluginAdminProxyHandleHTTPRequest = handler
	return func() {
		pluginAdminProxyHandleHTTPRequest = previous
	}
}

type pluginAdminManagerStub struct {
	allMeta                     []plugin.PluginMeta
	loading                     bool
	isDev                       bool
	instance                    *plugin.PluginInstance
	installFromBinaryWithSHA256 func(context.Context, string, []byte, string) error
	installFromGithub           func(context.Context, string, string) error
	uninstall                   func(context.Context, string) error
	reloadDev                   func(context.Context, string) error
	reloadInstance              func(context.Context, string) error
	getPluginConfig             func(context.Context, string) (map[string]string, error)
	updatePluginConfig          func(context.Context, string, map[string]string) error
}

func (s pluginAdminManagerStub) GetAllPluginMeta() []plugin.PluginMeta {
	return append([]plugin.PluginMeta(nil), s.allMeta...)
}
func (s pluginAdminManagerStub) InstallFromBinary(context.Context, string, []byte) error {
	return nil
}
func (s pluginAdminManagerStub) InstallFromBinaryWithSHA256(ctx context.Context, name string, binary []byte, hash string) error {
	if s.installFromBinaryWithSHA256 != nil {
		return s.installFromBinaryWithSHA256(ctx, name, binary, hash)
	}
	return nil
}
func (s pluginAdminManagerStub) InstallFromGithub(ctx context.Context, repo, version string) error {
	if s.installFromGithub != nil {
		return s.installFromGithub(ctx, repo, version)
	}
	return nil
}
func (s pluginAdminManagerStub) Uninstall(ctx context.Context, name string) error {
	if s.uninstall != nil {
		return s.uninstall(ctx, name)
	}
	return nil
}
func (s pluginAdminManagerStub) ReloadDev(ctx context.Context, name string) error {
	if s.reloadDev != nil {
		return s.reloadDev(ctx, name)
	}
	return nil
}
func (s pluginAdminManagerStub) ReloadInstance(ctx context.Context, name string) error {
	if s.reloadInstance != nil {
		return s.reloadInstance(ctx, name)
	}
	return nil
}
func (s pluginAdminManagerStub) IsDev(string) bool                         { return s.isDev }
func (s pluginAdminManagerStub) IsLoading() bool                           { return s.loading }
func (s pluginAdminManagerStub) GetInstance(string) *plugin.PluginInstance { return s.instance }
func (s pluginAdminManagerStub) GetPluginConfig(ctx context.Context, name string) (map[string]string, error) {
	if s.getPluginConfig != nil {
		return s.getPluginConfig(ctx, name)
	}
	return nil, nil
}
func (s pluginAdminManagerStub) UpdatePluginConfig(ctx context.Context, name string, config map[string]string) error {
	if s.updatePluginConfig != nil {
		return s.updatePluginConfig(ctx, name, config)
	}
	return nil
}

type pluginMarketplaceStub struct {
	listAvailable  func(context.Context) ([]plugin.MarketplacePlugin, error)
	syncFromGithub func(context.Context) error
}

func (s pluginMarketplaceStub) ListAvailable(ctx context.Context) ([]plugin.MarketplacePlugin, error) {
	if s.listAvailable == nil {
		return nil, nil
	}
	return s.listAvailable(ctx)
}

func (s pluginMarketplaceStub) SyncFromGithub(ctx context.Context) error {
	if s.syncFromGithub != nil {
		return s.syncFromGithub(ctx)
	}
	return nil
}
