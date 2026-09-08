package plugin

type installMetadata struct {
	GithubRepo  string `json:"github_repo,omitempty"`
	Version     string `json:"version,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	AssetSHA256 string `json:"asset_sha256,omitempty"`
}

func (m *Manager) readInstallMetadataLocked(inst *PluginInstance) installMetadata {
	if inst == nil || inst.Artifact == nil {
		return installMetadata{}
	}
	return inst.Artifact.Metadata
}
