package credentialimport

import (
	"context"
	"net/http"
	"testing"

	apppluginadmin "github.com/DevilGenius/airgate-core/internal/app/pluginadmin"
)

type capabilityProxyStub struct {
	target apppluginadmin.CapabilityTarget
	result apppluginadmin.ProxyResult
	err    error
	input  apppluginadmin.ProxyInput
}

func (s *capabilityProxyStub) ResolveGatewayCapability(_, _ string) (apppluginadmin.CapabilityTarget, error) {
	return s.target, s.err
}

func (s *capabilityProxyStub) Proxy(_ context.Context, input apppluginadmin.ProxyInput) (apppluginadmin.ProxyResult, error) {
	s.input = input
	return s.result, s.err
}

func TestParseUsesCapabilityTargetWithoutPublicPluginName(t *testing.T) {
	proxy := &capabilityProxyStub{
		target: apppluginadmin.CapabilityTarget{
			PluginName: "renamed-openai-plugin",
			Metadata:   `{"formats":["codex"]}`,
		},
		result: apppluginadmin.ProxyResult{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"format":"codex","accounts":[{"name":"one","type":"oauth","credentials":{"access_token":"token"},"priority":50,"max_concurrency":10,"rate_multiplier":1}],"renamed":true}`),
		},
	}
	service := NewService(proxy)
	result, err := service.Parse(t.Context(), ParseInput{
		Platform: "OpenAI",
		Format:   "Codex",
		Files:    []InputFile{{Name: "one.json", Content: []byte(`{"auth":{}}`)}},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Accounts) != 1 || result.Accounts[0].Name != "one" {
		t.Fatalf("Parse() result = %+v", result)
	}
	if proxy.input.Name != "renamed-openai-plugin" || proxy.input.Action != capabilityAction || proxy.input.Method != http.MethodPost {
		t.Fatalf("proxy input = %+v", proxy.input)
	}
}

func TestParseRejectsUnsupportedFormatBeforeProxy(t *testing.T) {
	proxy := &capabilityProxyStub{
		target: apppluginadmin.CapabilityTarget{
			PluginName: "gateway",
			Metadata:   `{"formats":["codex"]}`,
		},
	}
	_, err := NewService(proxy).Parse(t.Context(), ParseInput{Platform: "openai", Format: "cpa"})
	if err != ErrUnsupportedFormat {
		t.Fatalf("Parse() error = %v, want %v", err, ErrUnsupportedFormat)
	}
	if proxy.input.Name != "" {
		t.Fatalf("proxy should not be called: %+v", proxy.input)
	}
}
