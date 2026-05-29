package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
