package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOfficialPluginsIncludeCorePlugins(t *testing.T) {
	want := map[string]string{
		"gateway-openai":     "DevilGenius/airgate-openai",
		"gateway-claude":     "DevilGenius/airgate-claude",
		"gateway-kiro":       "DevilGenius/airgate-kiro",
		"airgate-playground": "DevilGenius/airgate-playground",
		"airgate-studio":     "DevilGenius/airgate-studio",
		"payment-epay":       "DevilGenius/airgate-epay",
	}

	got := make(map[string]MarketplacePlugin, len(officialPlugins))
	for _, p := range officialPlugins {
		got[p.Name] = p
	}

	for name, repo := range want {
		p, ok := got[name]
		if !ok {
			t.Fatalf("officialPlugins missing %q", name)
		}
		if p.GithubRepo != repo {
			t.Fatalf("officialPlugins[%q].GithubRepo = %q, want %q", name, p.GithubRepo, repo)
		}
		if p.Version != "0.0.1" {
			t.Fatalf("officialPlugins[%q].Version = %q, want 0.0.1", name, p.Version)
		}
	}
}

func TestSelectReleaseBinaryAssetSkipsChecksumAssets(t *testing.T) {
	assets := []githubAsset{
		{Name: "gateway-openai-linux-amd64.sha256", BrowserDownloadURL: "https://example.test/checksum"},
		{Name: "gateway-openai-linux-amd64", BrowserDownloadURL: "https://example.test/binary"},
	}

	got := selectReleaseBinaryAsset(assets, "linux", "amd64")
	if got == nil || got.BrowserDownloadURL != "https://example.test/binary" {
		t.Fatalf("selectReleaseBinaryAsset() = %+v", got)
	}
}

func TestResolveReleaseAssetSHA256UsesDigest(t *testing.T) {
	hash := strings.Repeat("a", 64)
	got := resolveReleaseAssetSHA256(t.Context(), githubAsset{Digest: "sha256:" + hash}, nil, "")
	if got != hash {
		t.Fatalf("resolveReleaseAssetSHA256() = %q, want %q", got, hash)
	}
}

func TestNormalizeSHA256ParsesChecksumFileContent(t *testing.T) {
	hash := strings.Repeat("b", 64)
	got := normalizeSHA256(hash + "  gateway-openai-linux-amd64\n")
	if got != hash {
		t.Fatalf("normalizeSHA256() = %q, want %q", got, hash)
	}
}

func TestNormalizeGitCommitSHA(t *testing.T) {
	sha := "ABCDEF1234567890ABCDEF1234567890ABCDEF12"
	if got := normalizeGitCommitSHA(sha); got != strings.ToLower(sha) {
		t.Fatalf("normalizeGitCommitSHA() = %q", got)
	}
	if got := normalizeGitCommitSHA(strings.Repeat("a", 64)); got != "" {
		t.Fatalf("normalizeGitCommitSHA() accepted non-commit SHA %q", got)
	}
}

func TestResolveReleaseAssetSHA256FetchesChecksumAsset(t *testing.T) {
	hash := strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(hash + "  gateway-openai-linux-amd64\n"))
	}))
	t.Cleanup(server.Close)

	got := resolveReleaseAssetSHA256(context.Background(), githubAsset{Name: "gateway-openai-linux-amd64"}, []githubAsset{{
		Name:               "gateway-openai-linux-amd64.sha256",
		BrowserDownloadURL: server.URL,
	}}, "")
	if got != hash {
		t.Fatalf("resolveReleaseAssetSHA256() = %q, want %q", got, hash)
	}
}

func TestMarketplaceSyncErrorAllowsPartialSuccess(t *testing.T) {
	err := marketplaceSyncError(1, 1, context.Canceled)
	if err != nil {
		t.Fatalf("marketplaceSyncError() = %v, want nil for partial success", err)
	}

	err = marketplaceSyncError(0, 1, context.Canceled)
	if err == nil {
		t.Fatal("marketplaceSyncError() = nil, want error when all GitHub entries fail")
	}
}

func TestNewMarketplaceOptionsAndListAvailableClone(t *testing.T) {
	t.Parallel()

	entries := []MarketplacePlugin{{
		Name:        "demo",
		Version:     "1.0.0",
		Description: "Demo",
		Type:        "extension",
	}}
	market := NewMarketplace(t.TempDir(),
		WithGithubToken("token"),
		WithEntries(entries),
		WithRefreshInterval(time.Second),
	)

	if market.githubToken != "token" || market.refreshInterval != time.Second {
		t.Fatalf("options not applied: token=%q interval=%s", market.githubToken, market.refreshInterval)
	}
	got, err := market.ListAvailable(context.Background())
	if err != nil {
		t.Fatalf("ListAvailable() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "demo" {
		t.Fatalf("ListAvailable() = %#v", got)
	}
	got[0].Name = "mutated"
	again, err := market.ListAvailable(context.Background())
	if err != nil {
		t.Fatalf("ListAvailable() second error = %v", err)
	}
	if again[0].Name != "demo" {
		t.Fatalf("ListAvailable should clone cache, got %#v", again)
	}
}

func TestMarketplaceSyncFromURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1","plugins":[{"name":"from-url","version":"2.0.0","type":"gateway"}]}`))
	}))
	t.Cleanup(server.Close)

	market := NewMarketplace(t.TempDir(), WithEntries([]MarketplacePlugin{{Name: "fallback"}}))
	if err := market.SyncFromURL(context.Background(), server.URL); err != nil {
		t.Fatalf("SyncFromURL() error = %v", err)
	}
	got, err := market.ListAvailable(context.Background())
	if err != nil {
		t.Fatalf("ListAvailable() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "from-url" || got[0].Version != "2.0.0" {
		t.Fatalf("synced plugins = %#v", got)
	}
}

func TestMarketplaceSyncFromURLErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusTeapot)
			},
		},
		{
			name: "json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`not-json`))
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			if err := NewMarketplace(t.TempDir()).SyncFromURL(context.Background(), server.URL); err == nil {
				t.Fatal("SyncFromURL() error = nil, want error")
			}
		})
	}
}

func TestMarketplaceDownload(t *testing.T) {
	t.Parallel()

	data := []byte("binary")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)

	market := NewMarketplace(t.TempDir())
	path, err := market.Download(context.Background(), "demo", "1.0.0", server.URL, hash)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("downloaded data = %q, want %q", got, data)
	}
}

func TestMarketplaceDownloadErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "status") {
			http.Error(w, "nope", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("binary"))
	}))
	t.Cleanup(server.Close)

	if _, err := NewMarketplace(t.TempDir()).Download(context.Background(), "demo", "1.0.0", server.URL+"/status", ""); err == nil {
		t.Fatal("Download(non-200) error = nil, want error")
	}
	if _, err := NewMarketplace(t.TempDir()).Download(context.Background(), "demo", "1.0.0", server.URL, strings.Repeat("0", 64)); err == nil {
		t.Fatal("Download(checksum mismatch) error = nil, want error")
	}
}

func TestMarketplaceStartStopWithStaticEntries(t *testing.T) {
	t.Parallel()

	market := NewMarketplace(t.TempDir(),
		WithEntries([]MarketplacePlugin{{Name: "static", Version: "1.0.0"}}),
		WithRefreshInterval(time.Hour),
	)
	market.Start(context.Background())
	market.Start(context.Background())
	market.Stop()
	market.Stop()

	got, err := market.ListAvailable(context.Background())
	if err != nil {
		t.Fatalf("ListAvailable() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "static" {
		t.Fatalf("cache = %#v", got)
	}
}
