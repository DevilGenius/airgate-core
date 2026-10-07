package dispatchresolver

import (
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestFallbackTargetIsResolvedOnlyInsideCore(t *testing.T) {
	resetResolverState(t)
	signal := sdk.ForwardOutcome{Kind: sdk.OutcomeClientError, FailoverScope: sdk.FailoverScopeModelReroute, ModelFallbackReason: sdk.ModelFallbackContextWindow}
	RegisterPlatformDSL("openai", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ContextWindowFallback: "platform-long", Candidates: []sdk.DispatchCandidate{{Scheduling: "${model}"}}}}})
	for _, tc := range []struct {
		name, target, client string
		signal               sdk.ForwardOutcome
		want                 string
	}{
		{"configured", "large", "small", signal, "large"},
		{"disabled", "", "small", signal, ""},
		{"loop", "large", "LARGE", signal, ""},
		{"success", "large", "small", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, FailoverScope: sdk.FailoverScopeModelReroute, ModelFallbackReason: sdk.ModelFallbackContextWindow}, ""},
		{"unknown reason", "large", "small", sdk.ForwardOutcome{Kind: sdk.OutcomeClientError, FailoverScope: sdk.FailoverScopeModelReroute, ModelFallbackReason: "unknown"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ContextWindowFallback: tc.target, Candidates: []sdk.DispatchCandidate{{Scheduling: "${model}", Wire: "wire"}}}}})
			plans := ResolveDispatchPlans("openai", group, "POST", "/v1/responses", tc.client)
			got, ok := FallbackTarget("openai", group, "POST", "/v1/responses", plans[0], tc.signal)
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("got %q/%v want %q", got, ok, tc.want)
			}
		})
	}
	plans := ResolveDispatchPlans("openai", nil, "POST", "/v1/responses", "client")
	if got, ok := FallbackTarget("openai", nil, "POST", "/v1/responses", plans[0], signal); !ok || got != "platform-long" {
		t.Fatalf("platform fallback %q/%v", got, ok)
	}
}

func TestGroupDecisionOwnsFallbackAndWireModel(t *testing.T) {
	resetResolverState(t)
	RegisterPlatformDSL("openai", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ID: "platform", ContextWindowFallback: "platform-long", Candidates: []sdk.DispatchCandidate{{Scheduling: "platform-pool", Wire: "platform-wire"}}}}})
	for _, fallback := range []string{"group-long", ""} {
		group := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ID: "group", When: sdk.DispatchWhen{Models: []string{"client"}}, ContextWindowFallback: fallback, Candidates: []sdk.DispatchCandidate{{Scheduling: "group-pool", Wire: "group-wire"}}}}})
		plans := ResolveDispatchPlans("openai", group, "POST", "/v1/responses", "client")
		target, _ := FallbackTarget("openai", group, "POST", "/v1/responses", plans[0], sdk.ForwardOutcome{Kind: sdk.OutcomeClientError, FailoverScope: sdk.FailoverScopeModelReroute, ModelFallbackReason: sdk.ModelFallbackContextWindow})
		if len(plans) != 1 || plans[0].WireModel != "group-wire" || target != fallback {
			t.Fatalf("group policy overridden: %+v/%s", plans, target)
		}
	}
}
func TestAliasDecisionPrecedesSchedulingAndRetainsOperation(t *testing.T) {
	rule := sdk.DispatchRule{ID: "image-alias", When: sdk.DispatchWhen{Methods: []string{"POST"}, Paths: []string{"/v1/images/generations"}, Models: []string{"gpt-image-2.5"}}, Operation: "images.generate", TimeoutProfile: "image", Gate: sdk.DispatchGate{RequiredOperation: "images.generate"}, Candidates: []sdk.DispatchCandidate{{Scheduling: "gpt-image-2.5-sunburst", Wire: "gpt-image-2.5-sunburst"}}}
	plans := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{rule}}).ResolveDispatchPlans("POST", "/v1/images/generations", "gpt-image-2.5")
	if len(plans) != 1 {
		t.Fatal(plans)
	}
	p := plans[0]
	if p.ClientModel != "gpt-image-2.5" || p.SchedulingModel != "gpt-image-2.5-sunburst" || p.WireModel != p.SchedulingModel || p.Operation != rule.Operation || p.Gate != rule.Gate || p.TimeoutProfile != "image" {
		t.Fatalf("incomplete decision %+v", p)
	}
}
