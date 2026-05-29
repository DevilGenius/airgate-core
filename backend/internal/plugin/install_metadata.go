package plugin

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

const installMetadataFile = ".airgate-install.json"

type installMetadata struct {
	GithubRepo  string `json:"github_repo,omitempty"`
	Version     string `json:"version,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	AssetSHA256 string `json:"asset_sha256,omitempty"`
}

func (m *Manager) writeInstallMetadata(binaryDir string, meta *installMetadata) {
	if binaryDir == "" {
		return
	}
	path := filepath.Join(m.pluginDir, binaryDir, installMetadataFile)
	if meta == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Debug("plugin_install_metadata_remove_failed", "path", path, "error", err)
		}
		return
	}

	meta.CommitSHA = normalizeGitCommitSHA(meta.CommitSHA)
	meta.AssetSHA256 = normalizeSHA256(meta.AssetSHA256)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		slog.Debug("plugin_install_metadata_marshal_failed", "path", path, "error", err)
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		slog.Debug("plugin_install_metadata_write_failed", "path", path, "error", err)
	}
}

func (m *Manager) readInstallMetadataLocked(inst *PluginInstance) installMetadata {
	if inst == nil || inst.BinaryDir == "" {
		return installMetadata{}
	}
	path := filepath.Join(m.pluginDir, inst.BinaryDir, installMetadataFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("plugin_install_metadata_read_failed", "path", path, "error", err)
		}
		return installMetadata{}
	}
	var meta installMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		slog.Debug("plugin_install_metadata_parse_failed", "path", path, "error", err)
		return installMetadata{}
	}
	meta.CommitSHA = normalizeGitCommitSHA(meta.CommitSHA)
	meta.AssetSHA256 = normalizeSHA256(meta.AssetSHA256)
	return meta
}
