package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

const activeManifestName = "active.json"

func (m *Manager) manifestPath(name string) string {
	return filepath.Join(m.pluginDir, name, activeManifestName)
}

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

type pluginArtifact struct {
	Generation string          `json:"generation"`
	SHA256     string          `json:"sha256"`
	SourcePath string          `json:"source_path,omitempty"`
	Previous   string          `json:"previous,omitempty"`
	Metadata   installMetadata `json:"metadata"`
}

func (m *Manager) artifactDir(generation string) string {
	id, err := uuid.Parse(generation)
	if err != nil || id.String() != generation {
		return ""
	}
	return filepath.Join(m.pluginDir, ".generations", generation)
}

func (m *Manager) artifactBinary(a *pluginArtifact) string {
	name := "plugin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(m.artifactDir(a.Generation), name)
}

func (m *Manager) newArtifact() (*pluginArtifact, error) {
	a := &pluginArtifact{Generation: uuid.NewString()}
	if err := os.MkdirAll(m.artifactDir(a.Generation), 0755); err != nil {
		return nil, fmt.Errorf("创建插件版本目录失败: %w", err)
	}
	return a, nil
}

// Only UUID directories underneath this manager's generation root are removed.
func (m *Manager) removeArtifact(generation string) {
	dir := m.artifactDir(generation)
	if dir == "" {
		return
	}
	// A rename can succeed before a directory sync reports an error. Never
	// remove a generation that a durable manifest may already reference.
	entries, err := os.ReadDir(m.pluginDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !pluginIDPattern.MatchString(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(m.manifestPath(entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return
		}
		var manifest pluginArtifact
		if json.Unmarshal(data, &manifest) != nil {
			return
		}
		if manifest.Generation == generation || manifest.Previous == generation {
			return
		}
	}
	root, err := filepath.Abs(filepath.Join(m.pluginDir, ".generations"))
	if err != nil {
		return
	}
	target, err := filepath.Abs(dir)
	if err != nil || filepath.Dir(target) != root {
		return
	}
	_ = os.RemoveAll(target)
}

func (m *Manager) loadArtifact(name string) (*pluginArtifact, error) {
	if !pluginIDPattern.MatchString(name) {
		return nil, errors.New("invalid plugin ID")
	}
	data, err := os.ReadFile(filepath.Join(m.pluginDir, name, activeManifestName))
	if err != nil {
		return nil, err
	}
	var a pluginArtifact
	if err = json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	if m.artifactDir(a.Generation) == "" || normalizeSHA256(a.SHA256) == "" {
		return nil, errors.New("invalid plugin manifest")
	}
	actual, err := fileSHA256(m.artifactBinary(&a))
	if err != nil {
		return nil, err
	}
	if actual != a.SHA256 {
		return nil, errors.New("installed plugin checksum mismatch")
	}
	return &a, nil
}

func (m *Manager) writeActiveArtifact(name string, a *pluginArtifact) error {
	if !pluginIDPattern.MatchString(name) {
		return errors.New("invalid plugin ID")
	}
	dir := filepath.Join(m.pluginDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".active-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceManifest(f.Name(), filepath.Join(dir, activeManifestName))
}

func (m *Manager) prepareArtifact(ctx context.Context, binary []byte, source string, meta *installMetadata) (*pluginArtifact, error) {
	a, err := m.newArtifact()
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			m.removeArtifact(a.Generation)
		}
	}()
	if meta != nil {
		a.Metadata = *meta
	}
	path, err := filepath.Abs(m.artifactBinary(a))
	if err != nil {
		return nil, err
	}
	if source != "" {
		a.SourcePath, err = filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, "go", "build", "-o", path, ".")
		before, _ := scanSourceFingerprint(a.SourcePath)
		cmd.Dir = a.SourcePath
		var output limitedBuildOutput
		cmd.Stdout, cmd.Stderr = &output, &output
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("构建插件失败: %w\n%s", err, output.String())
		}
		after, _ := scanSourceFingerprint(a.SourcePath)
		if before != after {
			return nil, errors.New("source changed during plugin build; retry with a stable snapshot")
		}
	} else if err := os.WriteFile(path, binary, 0755); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.SHA256, err = fileSHA256(path)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return nil, syncErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := syncGenerationDirectory(m.artifactDir(a.Generation)); err != nil {
		return nil, err
	}
	a.Metadata.AssetSHA256 = a.SHA256
	complete = true
	return a, nil
}

type limitedBuildOutput struct{ strings.Builder }

func (b *limitedBuildOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Builder.Write(p)
	}
	return n, nil
}
