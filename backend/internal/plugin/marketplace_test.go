package plugin

import "testing"

func TestOfficialPluginsIncludeCorePlugins(t *testing.T) {
	want := map[string]string{
		"gateway-openai":     "DevilGenius/airgate-openai",
		"gateway-claude":     "DevilGenius/airgate-claude",
		"gateway-kiro":       "DevilGenius/airgate-kiro",
		"airgate-playground": "DevilGenius/airgate-playground",
		"airgate-studio":     "DevilGenius/airgate-studio",
		"airgate-health":     "DevilGenius/airgate-health",
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
	}
}
