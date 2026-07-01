package plugin

import (
	"os/exec"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestMatchPluginByPlatformAndPath(t *testing.T) {
	mgr := &Manager{
		instances: map[string]*PluginInstance{
			"openai-plugin":    {Name: "openai-plugin", Platform: "openai"},
			"anthropic-plugin": {Name: "anthropic-plugin", Platform: "anthropic"},
		},
		routeCache: map[string][]sdk.RouteDefinition{
			"openai-plugin": {
				{Method: "POST", Path: "/v1/messages"},
			},
			"anthropic-plugin": {
				{Method: "POST", Path: "/v1/messages"},
			},
		},
	}

	inst := mgr.MatchPluginByPlatformAndPath("anthropic", "/v1/messages")
	if inst == nil {
		t.Fatal("expected plugin instance, got nil")
	} else if inst.Platform != "anthropic" {
		t.Fatalf("expected anthropic plugin, got %q", inst.Platform)
	}
}

func TestMatchPluginByPlatformAndPathRejectsUnsupportedPath(t *testing.T) {
	mgr := &Manager{
		instances: map[string]*PluginInstance{
			"openai-plugin": {Name: "openai-plugin", Platform: "openai"},
		},
		routeCache: map[string][]sdk.RouteDefinition{
			"openai-plugin": {
				{Method: "POST", Path: "/v1/chat/completions"},
			},
		},
	}

	inst := mgr.MatchPluginByPlatformAndPath("openai", "/v1/messages")
	if inst != nil {
		t.Fatalf("expected no plugin match, got %q", inst.Name)
	}
}

func TestMatchRoutePathRequiresSegmentBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		routePath string
		path      string
		want      bool
	}{
		{name: "exact", routePath: "/v1", path: "/v1", want: true},
		{name: "segment child", routePath: "/v1", path: "/v1/chat/completions", want: true},
		{name: "trailing slash route", routePath: "/v1/", path: "/v1/chat/completions", want: true},
		{name: "version sibling", routePath: "/v1", path: "/v10/models", want: false},
		{name: "name sibling", routePath: "/v1/foo", path: "/v1/foobar", want: false},
		{name: "empty route", routePath: "", path: "/v1", want: false},
		{name: "missing prefix", routePath: "/v1/messages", path: "/v1/chat/completions", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchRoutePath(tt.routePath, tt.path); got != tt.want {
				t.Fatalf("matchRoutePath(%q, %q) = %v, want %v", tt.routePath, tt.path, got, tt.want)
			}
		})
	}
}

func TestParseGithubRepo(t *testing.T) {
	owner, name, err := parseGithubRepo("https://github.com/acme/airgate-plugin.git")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if owner != "acme" || name != "airgate-plugin" {
		t.Fatalf("expected acme/airgate-plugin, got %s/%s", owner, name)
	}
}

func TestGithubReleaseAPIURLs(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    []string
	}{
		{
			name:    "latest",
			version: "",
			want: []string{
				"https://api.github.com/repos/acme/airgate-plugin/releases/latest",
			},
		},
		{
			name:    "plain version tries v-prefixed fallback",
			version: "1.2.3",
			want: []string{
				"https://api.github.com/repos/acme/airgate-plugin/releases/tags/1.2.3",
				"https://api.github.com/repos/acme/airgate-plugin/releases/tags/v1.2.3",
			},
		},
		{
			name:    "v-prefixed version tries plain fallback",
			version: "v1.2.3",
			want: []string{
				"https://api.github.com/repos/acme/airgate-plugin/releases/tags/v1.2.3",
				"https://api.github.com/repos/acme/airgate-plugin/releases/tags/1.2.3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := githubReleaseAPIURLs("acme", "airgate-plugin", tt.version)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got) = %d, want %d: %#v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetModelsReturnsClone(t *testing.T) {
	mgr := &Manager{
		modelCache: map[string][]sdk.ModelInfo{
			"openai": {
				{ID: "gpt-4.1", Name: "GPT-4.1"},
			},
		},
	}

	models := mgr.GetModels("openai")
	models[0].Name = "mutated"

	if got := mgr.modelCache["openai"][0].Name; got != "GPT-4.1" {
		t.Fatalf("expected cached model to remain unchanged, got %q", got)
	}
}

func TestNewPluginClientConfigSetsStartTimeout(t *testing.T) {
	mgr := &Manager{}
	cfg := mgr.newPluginClientConfig(exec.Command("sh", "-c", "exit 0"), false, nil)

	if cfg.StartTimeout != pluginStartTimeout {
		t.Fatalf("StartTimeout = %v, want %v", cfg.StartTimeout, pluginStartTimeout)
	}
}
