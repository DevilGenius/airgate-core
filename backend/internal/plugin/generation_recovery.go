package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Recovery protects both manifest generations and all live processes before
// considering any unpublished artifact. An unreadable/invalid manifest aborts
// cleanup completely. The file lock prevents concurrent candidate creation.
func (m *Manager) pruneArtifacts() {
	m.artifactMu.Lock()
	defer m.artifactMu.Unlock()
	keep := make(map[string]bool)
	plugins, err := os.ReadDir(m.pluginDir)
	if err != nil {
		return
	}
	for _, entry := range plugins {
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
		var artifact pluginArtifact
		if json.Unmarshal(data, &artifact) != nil || m.artifactDir(artifact.Generation) == "" {
			return
		}
		keep[artifact.Generation] = true
		if artifact.Previous != "" {
			if m.artifactDir(artifact.Previous) == "" {
				return
			}
			keep[artifact.Previous] = true
		}
	}
	m.mu.RLock()
	for _, inst := range m.instances {
		keep[inst.Generation] = true
	}
	for _, inst := range m.retiring {
		keep[inst.Generation] = true
	}
	m.mu.RUnlock()
	entries, err := os.ReadDir(filepath.Join(m.pluginDir, ".generations"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		generation := entry.Name()
		if !entry.IsDir() || m.artifactDir(generation) == "" || keep[generation] {
			continue
		}
		// removeArtifact independently rechecks every active/previous manifest
		// and verifies the absolute directory lies under .generations.
		m.removeArtifact(generation)
	}
}
