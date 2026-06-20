package dispatchresolver

import (
	"reflect"
	"testing"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func resetResolverState(t *testing.T) {
	t.Helper()

	mu.Lock()
	origResolvers := resolvers
	resolvers = map[string]*CompiledResolver{}
	mu.Unlock()

	cacheMu.Lock()
	origCached := cached
	cached = map[string]*CompiledResolver{}
	cacheMu.Unlock()

	t.Cleanup(func() {
		mu.Lock()
		resolvers = origResolvers
		mu.Unlock()

		cacheMu.Lock()
		cached = origCached
		cacheMu.Unlock()
	})
}

func TestCompileReturnsNilForEmptyOrCandidateFreeRules(t *testing.T) {
	if got := Compile(sdk.DispatchDSL{}); got != nil {
		t.Fatalf("Compile(empty) = %#v, want nil", got)
	}
	if got := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{{ID: "empty"}}}); got != nil {
		t.Fatalf("Compile(candidate-free) = %#v, want nil", got)
	}
}

func TestResolveDispatchPlansMatchesAndRendersCandidates(t *testing.T) {
	resolver := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{
		{
			ID: "ignored",
			When: sdk.DispatchWhen{
				Methods: []string{"GET"},
			},
			Candidates: []sdk.DispatchCandidate{{Scheduling: "ignored"}},
		},
		{
			ID: " image-rule ",
			When: sdk.DispatchWhen{
				Methods:       []string{" post ", ""},
				PathPrefixes:  []string{" /v1/responses/ "},
				ModelSuffixes: []string{"-image"},
			},
			Model:          sdk.DispatchModel{StripSuffix: "-IMAGE"},
			Operation:      " images.generate ",
			TimeoutProfile: " image ",
			Gate:           sdk.DispatchGate{RequiredOperation: "images.generate", Status: 403, Code: "image_disabled"},
			Candidates: []sdk.DispatchCandidate{
				{Scheduling: " ${model.base} ", Wire: "${scheduling}-wire"},
				{Scheduling: "${model.base}", Wire: "${scheduling}-wire"},
				{Scheduling: ""},
			},
		},
	}})
	if resolver == nil {
		t.Fatal("Compile returned nil")
	}

	plans := resolver.ResolveDispatchPlans(" post ", "HTTPS://example.com/v1/responses/foo?x=1", "GPT-4o-IMAGE")
	want := []sdk.DispatchPlan{{
		ClientModel:     "GPT-4o-IMAGE",
		SchedulingModel: "GPT-4o",
		WireModel:       "GPT-4o-wire",
		RuleID:          "image-rule",
		Operation:       "images.generate",
		TimeoutProfile:  "image",
		Gate:            sdk.DispatchGate{RequiredOperation: "images.generate", Status: 403, Code: "image_disabled"},
	}}
	if !reflect.DeepEqual(plans, want) {
		t.Fatalf("plans = %#v, want %#v", plans, want)
	}
}

func TestResolveDispatchPlansMismatchAndWireFallback(t *testing.T) {
	resolver := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{
		{
			ID: "exact",
			When: sdk.DispatchWhen{
				Paths:         []string{"/v1/chat/completions"},
				Models:        []string{"gpt-4.1"},
				ModelPrefixes: []string{"claude-"},
				ModelSuffixes: []string{"-latest"},
			},
			Candidates: []sdk.DispatchCandidate{{Scheduling: "${model}"}},
		},
	}})

	if got := resolver.ResolveDispatchPlans("POST", "/v1/other", "gpt-4.1"); got != nil {
		t.Fatalf("path mismatch plans = %#v, want nil", got)
	}
	if got := resolver.ResolveDispatchPlans("POST", "/v1/chat/completions", "unknown"); got != nil {
		t.Fatalf("model mismatch plans = %#v, want nil", got)
	}

	plans := resolver.ResolveDispatchPlans("POST", "/v1/chat/completions/", "gpt-4.1")
	if len(plans) != 1 {
		t.Fatalf("len(plans) = %d, want 1", len(plans))
	}
	if plans[0].SchedulingModel != "gpt-4.1" || plans[0].WireModel != "gpt-4.1" {
		t.Fatalf("plan = %#v, want wire fallback to scheduling", plans[0])
	}

	if got := resolver.ResolveDispatchPlans("POST", "/v1/chat/completions", "claude-sonnet"); len(got) != 1 {
		t.Fatalf("prefix match len = %d, want 1", len(got))
	}
	if got := resolver.ResolveDispatchPlans("POST", "/v1/chat/completions", "model-latest"); len(got) != 1 {
		t.Fatalf("suffix match len = %d, want 1", len(got))
	}
}

func TestCompileCached(t *testing.T) {
	resetResolverState(t)

	first := CompileCached(" key ", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		Candidates: []sdk.DispatchCandidate{{Scheduling: "first"}},
	}}})
	second := CompileCached("key", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		Candidates: []sdk.DispatchCandidate{{Scheduling: "second"}},
	}}})
	if first == nil || second == nil {
		t.Fatal("CompileCached returned nil")
	}
	if first != second {
		t.Fatal("CompileCached did not reuse resolver for stable cache key")
	}

	uncached := CompileCached("", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		Candidates: []sdk.DispatchCandidate{{Scheduling: "uncached"}},
	}}})
	if uncached == nil || uncached == first {
		t.Fatalf("CompileCached blank key = %#v, want a fresh resolver", uncached)
	}
}

func TestPlatformAndIdentityFallback(t *testing.T) {
	resetResolverState(t)

	RegisterPlatformDSL("", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{Candidates: []sdk.DispatchCandidate{{Scheduling: "ignored"}}}}})
	RegisterPlatformDSL(" OpenAI ", sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		ID:         "platform",
		Candidates: []sdk.DispatchCandidate{{Scheduling: "platform-model"}},
	}}})

	platformPlans := ResolveDispatchPlans("openai", nil, "POST", "/anything", "client-model")
	if len(platformPlans) != 1 || platformPlans[0].SchedulingModel != "platform-model" {
		t.Fatalf("platform plans = %#v", platformPlans)
	}

	groupResolver := Compile(sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		ID:         "group",
		Candidates: []sdk.DispatchCandidate{{Scheduling: "group-model"}},
	}}})
	groupPlans := ResolveDispatchPlans("openai", groupResolver, "POST", "/anything", "client-model")
	if len(groupPlans) != 1 || groupPlans[0].RuleID != "group" || groupPlans[0].SchedulingModel != "group-model" {
		t.Fatalf("group plans = %#v", groupPlans)
	}

	UnregisterPlatformDSL("openai")
	identity := ResolveDispatchPlans("openai", nil, "POST", "/anything", " client-model ")
	want := []sdk.DispatchPlan{{ClientModel: "client-model", SchedulingModel: "client-model", WireModel: "client-model"}}
	if !reflect.DeepEqual(identity, want) {
		t.Fatalf("identity = %#v, want %#v", identity, want)
	}
	if got := ResolveDispatchPlans("openai", nil, "POST", "/anything", " "); got != nil {
		t.Fatalf("blank identity = %#v, want nil", got)
	}

	RegisterPlatformDSL("openai", sdk.DispatchDSL{})
	if resolver := platformResolver("openai"); resolver != nil {
		t.Fatalf("platform resolver after empty DSL registration = %#v, want nil", resolver)
	}

	UnregisterPlatformDSL("")
}

func TestHelpers(t *testing.T) {
	if got := renderTemplate(" ${model.base}/${model}/${scheduling} ", "client", "base", "sched"); got != "base/client/sched" {
		t.Fatalf("renderTemplate = %q", got)
	}
	if got := renderTemplate(" ", "client", "base", "sched"); got != "" {
		t.Fatalf("renderTemplate blank = %q", got)
	}

	for _, tt := range []struct {
		path   string
		prefix string
		want   bool
	}{
		{path: "/v1", prefix: "/v1", want: true},
		{path: "/v1/models", prefix: "/v1", want: true},
		{path: "/v10/models", prefix: "/v1", want: false},
		{path: "/v1", prefix: "", want: false},
		{path: "/v1", prefix: "/missing", want: false},
	} {
		if got := pathHasAPIPrefix(tt.path, tt.prefix); got != tt.want {
			t.Fatalf("pathHasAPIPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.want)
		}
	}
}
