package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) prepareGenerationAssets(ctx context.Context, inst *PluginInstance, assets map[string][]byte) error {
	dir := filepath.Join(m.artifactDir(inst.Generation), "assets")
	if err := extractWebAssetsContext(ctx, dir, assets); err != nil {
		return err
	}
	inst.frontendAssets = make(map[string]bool, len(assets))
	for name := range assets {
		inst.frontendAssets[name] = true
	}
	return nil
}

func extractWebAssets(dir string, assets map[string][]byte) error {
	return extractWebAssetsContext(context.Background(), dir, assets)
}

func extractWebAssetsContext(ctx context.Context, dir string, assets map[string][]byte) error {
	if len(assets) > 4096 {
		return fmt.Errorf("too many plugin assets")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	total := 0
	for path, content := range assets {
		if err := ctx.Err(); err != nil {
			return err
		}
		total += len(content)
		if !safeAssetPath(path) || total > 64<<20 {
			return fmt.Errorf("invalid or excessive plugin assets")
		}
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("创建子目录失败: %w", err)
		}
		if existing, err := os.ReadFile(fullPath); err == nil {
			if !bytes.Equal(existing, content) {
				return fmt.Errorf("immutable asset changed: %s", path)
			}
			continue
		}
		if err := os.WriteFile(fullPath, content, 0644); err != nil {
			return fmt.Errorf("写入文件 %s 失败: %w", path, err)
		}
	}
	return nil
}

func safeAssetPath(path string) bool {
	return path != "" && filepath.IsLocal(path) && !strings.Contains(path, "\\") && !strings.Contains(path, ":") && !strings.HasPrefix(path, "/") && !strings.Contains("/"+path+"/", "/../")
}

func (m *Manager) ReadPluginAsset(name, path string) ([]byte, error) {
	if !safeAssetPath(path) {
		return nil, os.ErrNotExist
	}
	inst, release, err := m.GetInstance(name).Acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	if inst.Artifact == nil {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(m.artifactDir(inst.Generation), "assets", path))
}
