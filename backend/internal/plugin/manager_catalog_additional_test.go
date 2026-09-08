package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestManagerCatalogClonesAliasesAndMeta(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "gateway-openai", "assets"), 0755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	srcPath := filepath.Join(root, "source", "backend")

	mgr := &Manager{
		pluginDir: root,
		instances: map[string]*PluginInstance{
			"gateway-openai": {
				Name:               "gateway-openai",
				Artifact:           &pluginArtifact{},
				frontendAssets:     map[string]bool{"index.js": true},
				DisplayName:        "OpenAI",
				Version:            "1.2.3",
				Author:             "AirGate",
				Type:               "gateway",
				Platform:           "openai",
				InstructionPresets: []string{"concise"},
				ConfigSchema:       []sdk.ConfigField{{Key: "api_base", Label: "API Base"}},
				Metadata:           map[string]string{"tier": "official"},
			},
		},
		aliases: map[string]string{},
		devPaths: map[string]string{
			"gateway-openai": srcPath,
		},
		modelCache: map[string][]sdk.ModelInfo{
			"openai": {{ID: "gpt-4.1", Name: "GPT-4.1"}},
		},
		routeCache: map[string][]sdk.RouteDefinition{
			"gateway-openai": {{Method: http.MethodPost, Path: "/v1/chat/completions"}},
		},
		credCache: map[string][]sdk.CredentialField{
			"openai": {{Key: "api_key", Label: "API Key"}},
		},
		accountTypeCache: map[string][]sdk.AccountType{
			"openai": {{
				Key:    "apikey",
				Label:  "API Key",
				Fields: []sdk.CredentialField{{Key: "api_key", Label: "API Key"}},
			}},
		},
		frontendPageCache: map[string][]sdk.FrontendPage{
			"gateway-openai": {{Path: "/plugins/openai", Title: "OpenAI"}},
		},
	}
	mgr.registerAliasesLocked("gateway-openai", "openai", " gateway-openai-dev ")

	if got := mgr.GetInstance(" openai "); got == nil || got.Name != "gateway-openai" {
		t.Fatalf("GetInstance(alias) = %+v", got)
	}
	if !mgr.IsRunning("gateway-openai-dev") || mgr.RunningCount() != 1 {
		t.Fatalf("running state mismatch")
	}
	if !mgr.IsDev("openai") {
		t.Fatal("IsDev(alias) = false, want true")
	}
	if got := mgr.GetPluginByPlatform("openai"); got == nil || got.Name != "gateway-openai" {
		t.Fatalf("GetPluginByPlatform() = %+v", got)
	}
	if got := mgr.FindPlatformByModel("gpt-4.1"); got != "openai" {
		t.Fatalf("FindPlatformByModel() = %q, want openai", got)
	}
	if got := mgr.MatchPluginByRoute(http.MethodPost, "/v1/chat/completions"); got == nil || got.Name != "gateway-openai" {
		t.Fatalf("MatchPluginByRoute() = %+v", got)
	}
	if got := mgr.MatchPluginByPathPrefix("/v1/chat/completions/extra"); got == nil || got.Name != "gateway-openai" {
		t.Fatalf("MatchPluginByPathPrefix() = %+v", got)
	}
	if got := mgr.MatchPluginByPlatformAndPath("openai", "/v1/chat/completions"); got == nil || got.Name != "gateway-openai" {
		t.Fatalf("MatchPluginByPlatformAndPath() = %+v", got)
	}
	if got := mgr.MatchPluginByPlatformAndPath("anthropic", "/v1/chat/completions"); got != nil {
		t.Fatalf("unexpected platform match: %+v", got)
	}
	if got := mgr.GetRoutes("openai"); len(got) != 1 || got[0].Path != "/v1/chat/completions" {
		t.Fatalf("GetRoutes() = %#v", got)
	}
	allRoutes := mgr.GetAllRoutes()
	allRoutes["gateway-openai"][0].Path = "/mutated"
	if got := mgr.routeCache["gateway-openai"][0].Path; got != "/v1/chat/completions" {
		t.Fatalf("GetAllRoutes should clone slices, cache path = %q", got)
	}
	fields := mgr.GetCredentialFields("openai")
	fields[0].Label = "mutated"
	if got := mgr.credCache["openai"][0].Label; got != "API Key" {
		t.Fatalf("GetCredentialFields should clone, cache label = %q", got)
	}
	types := mgr.GetAccountTypes("openai")
	types[0].Fields[0].Label = "mutated"
	if got := mgr.accountTypeCache["openai"][0].Fields[0].Label; got != "API Key" {
		t.Fatalf("GetAccountTypes should deep clone fields, cache label = %q", got)
	}
	pages := mgr.GetFrontendPages("openai")
	pages[0].Title = "mutated"
	if got := mgr.frontendPageCache["gateway-openai"][0].Title; got != "OpenAI" {
		t.Fatalf("GetFrontendPages should clone, cache title = %q", got)
	}

	if !mgr.HasWebAssets("openai") {
		t.Fatal("HasWebAssets(alias) = false, want true")
	}
	metas := mgr.GetAllPluginMeta()
	if len(metas) != 1 {
		t.Fatalf("len(GetAllPluginMeta()) = %d, want 1", len(metas))
	}
	meta := metas[0]
	if !meta.IsDev || !meta.HasWebAssets || meta.Name != "gateway-openai" || meta.FrontendPages[0].Title != "OpenAI" {
		t.Fatalf("meta = %+v", meta)
	}
	meta.Metadata["tier"] = "mutated"
	if got := mgr.instances["gateway-openai"].Metadata["tier"]; got != "official" {
		t.Fatalf("metadata should be cloned, got %q", got)
	}

	mgr.unregisterAliasesLocked("gateway-openai", "openai")
	if got := mgr.resolveName("openai"); got != "openai" {
		t.Fatalf("resolveName after unregister = %q, want openai", got)
	}
}

func TestManagerInstalledBinaryHashAndNameNormalization(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	binaryDir := filepath.Join(root, "gateway-openai")
	if err := os.MkdirAll(binaryDir, 0755); err != nil {
		t.Fatalf("mkdir binary dir: %v", err)
	}
	binaryPath := filepath.Join(binaryDir, "gateway-openai")
	data := []byte("plugin binary")
	if err := os.WriteFile(binaryPath, data, 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	mgr := &Manager{pluginDir: root}
	if got := mgr.installedBinarySHA256Locked(&PluginInstance{Name: "gateway-openai", Artifact: &pluginArtifact{SHA256: want}}); got != want {
		t.Fatalf("installedBinarySHA256Locked() = %q, want %q", got, want)
	}
	if got := mgr.installedBinarySHA256Locked(nil); got != "" {
		t.Fatalf("nil installedBinarySHA256Locked() = %q, want empty", got)
	}
	if got := normalizePluginName(" gateway-openai "); got != "gateway-openai" {
		t.Fatalf("normalizePluginName() = %q", got)
	}

}

func TestManagerPluginConfigUsesSQLiteTestDB(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_config_manager", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	mgr := &Manager{
		db:      db,
		aliases: map[string]string{"openai": "gateway-openai"},
		instances: map[string]*PluginInstance{
			"gateway-openai": {Name: "gateway-openai", Version: "1.2.3", Platform: "openai", Type: "gateway"},
		},
	}

	if got, err := mgr.GetPluginConfig(ctx, "openai"); err != nil || len(got) != 0 {
		t.Fatalf("empty GetPluginConfig() = %#v, %v", got, err)
	}
	if err := mgr.UpdatePluginConfig(ctx, "openai", map[string]string{"api_base": "https://api.example.test", "enabled": "true"}); err != nil {
		t.Fatalf("create UpdatePluginConfig() error = %v", err)
	}
	got, err := mgr.GetPluginConfig(ctx, "gateway-openai")
	if err != nil {
		t.Fatalf("GetPluginConfig() error = %v", err)
	}
	if got["api_base"] != "https://api.example.test" || got["enabled"] != "true" {
		t.Fatalf("created config = %#v", got)
	}
	if err := mgr.UpdatePluginConfig(ctx, "openai", map[string]string{"api_base": "https://next.example.test"}); err != nil {
		t.Fatalf("update UpdatePluginConfig() error = %v", err)
	}
	got, err = mgr.GetPluginConfig(ctx, "openai")
	if err != nil {
		t.Fatalf("GetPluginConfig() after update error = %v", err)
	}
	if got["api_base"] != "https://next.example.test" {
		t.Fatalf("updated config = %#v", got)
	}

	nilDB := &Manager{}
	if got, err := nilDB.GetPluginConfig(ctx, "missing"); err != nil || len(got) != 0 {
		t.Fatalf("nil db GetPluginConfig() = %#v, %v", got, err)
	}
	if err := nilDB.UpdatePluginConfig(ctx, "missing", nil); err == nil {
		t.Fatal("nil db UpdatePluginConfig() error = nil, want error")
	}
}

func TestDevWatcherScansAndCloses(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "ignored.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	if _, ok := scanSourceFingerprint(filepath.Join(root, "missing")); ok {
		t.Fatal("scanSourceFingerprint(missing) ok = true, want false")
	}
	if _, ok := scanSourceFingerprint(root); !ok {
		t.Fatal("scanSourceFingerprint(root) ok = false, want true")
	}

	dw := newDevWatcher(&Manager{})
	dw.add("demo", root)
	dw.tick()
	dw.remove("demo")
	dw.Close()
	dw.Close()
	dw.add("closed", root)
}
