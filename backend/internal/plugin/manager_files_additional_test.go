package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallMetadataReadWriteAndRemove(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "gateway-openai"), 0755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	mgr := &Manager{pluginDir: root}

	mgr.writeInstallMetadata("", &installMetadata{GithubRepo: "ignored"})
	mgr.writeInstallMetadata("gateway-openai", &installMetadata{
		GithubRepo:  "DevilGenius/airgate-openai",
		Version:     "v1.2.3",
		CommitSHA:   strings.Repeat("A", 40),
		AssetSHA256: "sha256:" + strings.Repeat("B", 64),
	})

	meta := mgr.readInstallMetadataLocked(&PluginInstance{BinaryDir: "gateway-openai"})
	if meta.GithubRepo != "DevilGenius/airgate-openai" || meta.CommitSHA != strings.Repeat("a", 40) || meta.AssetSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("metadata = %+v", meta)
	}

	mgr.writeInstallMetadata("gateway-openai", nil)
	if got := mgr.readInstallMetadataLocked(&PluginInstance{BinaryDir: "gateway-openai"}); got != (installMetadata{}) {
		t.Fatalf("metadata after remove = %+v", got)
	}
	if got := mgr.readInstallMetadataLocked(nil); got != (installMetadata{}) {
		t.Fatalf("nil instance metadata = %+v", got)
	}

	badPath := filepath.Join(root, "gateway-openai", installMetadataFile)
	if err := os.WriteFile(badPath, []byte("{"), 0644); err != nil {
		t.Fatalf("write bad metadata: %v", err)
	}
	if got := mgr.readInstallMetadataLocked(&PluginInstance{BinaryDir: "gateway-openai"}); got != (installMetadata{}) {
		t.Fatalf("bad metadata = %+v", got)
	}
}

func TestExtractWebAssetsWritesNestedFilesAndHandlesProviderEdges(t *testing.T) {
	root := t.TempDir()
	if err := extractWebAssets(filepath.Join(root, "direct"), map[string][]byte{
		"index.html":       []byte("<html></html>"),
		"assets/app.js":    []byte("console.log('ok')"),
		"assets/style.css": []byte("body{}"),
	}); err != nil {
		t.Fatalf("extractWebAssets() error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "direct", "assets", "app.js"))
	if err != nil {
		t.Fatalf("read nested asset: %v", err)
	}
	if string(body) != "console.log('ok')" {
		t.Fatalf("nested asset body = %q", body)
	}

	mgr := &Manager{pluginDir: root}
	mgr.extractPluginWebAssets("gateway-openai", webAssetsProviderStub{
		assets: map[string][]byte{"web/index.html": []byte("plugin web")},
	})
	body, err = os.ReadFile(filepath.Join(root, "gateway-openai", "assets", "web", "index.html"))
	if err != nil {
		t.Fatalf("read plugin asset: %v", err)
	}
	if string(body) != "plugin web" {
		t.Fatalf("plugin asset body = %q", body)
	}

	mgr.extractPluginWebAssets("empty", webAssetsProviderStub{})
	mgr.extractPluginWebAssets("error", webAssetsProviderStub{err: errors.New("asset failed")})
}

type webAssetsProviderStub struct {
	assets map[string][]byte
	err    error
}

func (s webAssetsProviderStub) GetWebAssets() (map[string][]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.assets, nil
}
