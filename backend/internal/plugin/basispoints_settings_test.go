package plugin

import (
	"net/http"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/routing"
)

func TestBasispointsGroupSettingIsTrustedAndReversible(t *testing.T) {
	const name = "X-Airgate-Plugin-Openai-Basispoints"
	source := http.Header{}
	source.Set(name, "true")
	for _, setting := range []string{"", "true", "false"} {
		info := &auth.APIKeyInfo{GroupPluginSettings: map[string]map[string]string{"openai": {"basispoints": setting}}}
		got := buildHeaders(source, info)
		if got.Get(name) != setting {
			t.Fatalf("setting=%q header=%q", setting, got.Get(name))
		}
		hostHeaders := hostForwardHeaders(hostForwardRequest{Headers: map[string]interface{}{name: "true"}}, routing.Candidate{GroupPluginSettings: info.GroupPluginSettings})
		if hostHeaders.Get(name) != setting {
			t.Fatalf("host setting=%q header=%q", setting, hostHeaders.Get(name))
		}
	}
	if source.Get(name) != "true" {
		t.Fatal("mutated client headers")
	}
}
