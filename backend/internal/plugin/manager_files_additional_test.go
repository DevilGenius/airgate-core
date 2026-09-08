package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerationManifestChecksumAndAssets(t *testing.T) {
	m := NewManager(t.TempDir(), "debug", "", nil)
	t.Cleanup(m.devWatcher.Close)
	a, err := m.prepareArtifact(context.Background(), []byte("binary"), "", &installMetadata{Version: "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.writeActiveArtifact("demo", a); err != nil {
		t.Fatal(err)
	}
	loaded, err := m.loadArtifact("demo")
	if err != nil || loaded.Generation != a.Generation || loaded.Metadata.Version != "v2" {
		t.Fatalf("%+v %v", loaded, err)
	}
	inst := &PluginInstance{Name: "demo", Generation: a.Generation, Artifact: a, owner: m}
	if err := m.prepareGenerationAssets(context.Background(), inst, map[string][]byte{"index.js": []byte("v2")}); err != nil {
		t.Fatal(err)
	}
	m.instances["demo"] = inst
	data, err := m.ReadPluginAsset("demo", "index.js")
	if err != nil || string(data) != "v2" {
		t.Fatalf("%q %v", data, err)
	}
	for _, path := range []string{"../escape", "/absolute", "C:/escape", "..\\escape"} {
		if err := extractWebAssets(filepath.Join(t.TempDir(), "assets"), map[string][]byte{path: []byte("bad")}); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	if err := m.prepareGenerationAssets(context.Background(), inst, map[string][]byte{"index.js": []byte("overwrite")}); err == nil {
		t.Fatal("immutable asset overwritten")
	}
	if err := os.WriteFile(m.artifactBinary(a), []byte("tampered"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.loadArtifact("demo"); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
}

func TestFailedArtifactBuildPreservesActiveManifest(t *testing.T) {
	m := NewManager(t.TempDir(), "debug", "", nil)
	t.Cleanup(m.devWatcher.Close)
	a, err := m.prepareArtifact(context.Background(), []byte("known good"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.writeActiveArtifact("demo", a); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.prepareArtifact(ctx, nil, filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Fatal("canceled build succeeded")
	}
	got, err := m.loadArtifact("demo")
	if err != nil || got.Generation != a.Generation {
		t.Fatalf("active artifact changed: %+v %v", got, err)
	}
}

func TestArtifactRestartKeepsPreviousAndPrunesUnpublished(t *testing.T) {
	m := NewManager(t.TempDir(), "error", "", nil)
	defer m.devWatcher.Close()
	a, err := m.prepareArtifact(context.Background(), []byte("old"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.writeActiveArtifact("demo", a); err != nil {
		t.Fatal(err)
	}
	b, err := m.prepareArtifact(context.Background(), []byte("new"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &preparedPlugin{instance: &PluginInstance{Name: "demo", Generation: b.Generation, Artifact: b}, detachPreparation: func() bool { return true }}
	if err := m.publishPlugin(context.Background(), &pluginUpdate{}, p); err != nil {
		t.Fatal(err)
	}
	if b.Previous != a.Generation {
		t.Fatal("restart forgot previous artifact")
	}
	orphan, err := m.prepareArtifact(context.Background(), []byte("unpublished"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	m.pruneArtifacts()
	for _, kept := range []*pluginArtifact{a, b} {
		if _, err := os.Stat(m.artifactBinary(kept)); err != nil {
			t.Fatal("referenced artifact removed", err)
		}
	}
	if _, err := os.Stat(m.artifactDir(orphan.Generation)); !os.IsNotExist(err) {
		t.Fatal("unpublished artifact was not reclaimed")
	}
}
