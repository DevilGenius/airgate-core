package modelresolver

import (
	"reflect"
	"testing"
)

type testResolver struct {
	models []string
}

func (r testResolver) ResolveSchedulingModels(string, string) []string {
	return r.models
}

func TestResolverRegistryAndFallback(t *testing.T) {
	Register("", testResolver{models: []string{"ignored"}})
	Register("empty", nil)
	Register(" Custom ", testResolver{models: []string{"custom"}})

	if got := ResolveSchedulingModels("custom", "/anything", "client"); !reflect.DeepEqual(got, []string{"custom"}) {
		t.Fatalf("custom resolver = %#v", got)
	}
	if got := ResolveSchedulingModels("missing", "/anything", " Model "); !reflect.DeepEqual(got, []string{"Model"}) {
		t.Fatalf("fallback resolver = %#v", got)
	}
	if got := ForPlatform(" CUSTOM ").ResolveSchedulingModels("", "unused"); !reflect.DeepEqual(got, []string{"custom"}) {
		t.Fatalf("ForPlatform custom resolver = %#v", got)
	}
}

func TestCompactUniqueModels(t *testing.T) {
	got := compactUniqueModels(" gpt-5 ", "GPT-5", "", "gpt-4")
	want := []string{"gpt-5", "gpt-4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("compactUniqueModels = %#v, want %#v", got, want)
	}
}

func TestResolveSchedulingModelsForOpenAIAnthropicMessages(t *testing.T) {
	clearSchedulingModelEnv(t)

	tests := []struct {
		name  string
		path  string
		model string
		want  []string
	}{
		{
			name:  "fable 使用高阶主模型和降级模型",
			path:  "/v1/messages",
			model: "claude-fable-5",
			want:  []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:  "opus 使用主模型和降级模型",
			path:  "/v1/messages",
			model: "claude-opus-4-7",
			want:  []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:  "sonnet 使用主模型和降级模型",
			path:  "/messages",
			model: "claude-sonnet-4-6",
			want:  []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:  "haiku 使用快速模型和降级模型",
			path:  "/v1/messages/count_tokens",
			model: "claude-haiku-4-5",
			want:  []string{"gpt-5.3-codex-spark", "gpt-5.4-mini"},
		},
		{
			name:  "绝对 URL messages 路径",
			path:  "https://example.com/v1/messages?trace=1",
			model: "claude-opus-4-7",
			want:  []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:  "大小写 messages 路径",
			path:  "/V1/MESSAGES",
			model: "claude-sonnet-4-6",
			want:  []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:  "非 Claude 模型保持原样",
			path:  "/v1/messages",
			model: "gpt-5.4",
			want:  []string{"gpt-5.4"},
		},
		{
			name:  "Claude 3 使用默认分支",
			path:  "/v1/messages",
			model: "claude-3-opus",
			want:  []string{"gpt-5.5", "gpt-5.4"},
		},
		{
			name:  "空 Claude 模型保持空列表",
			path:  "/v1/messages",
			model: " ",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSchedulingModels("openai", tt.path, tt.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveSchedulingModels() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveSchedulingModelsForOpenAIAnthropicMessagesUsesEnvOverride(t *testing.T) {
	clearSchedulingModelEnv(t)
	t.Setenv("AIRGATE_MODEL_OPUS", "openai/gpt-5.4")
	t.Setenv("AIRGATE_MODEL_OPUS_FALLBACK", "oai/gpt-5.4")

	got := ResolveSchedulingModels("openai", "/v1/messages", "claude-opus-4-7")
	want := []string{"gpt-5.4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveSchedulingModels() = %#v, want %#v", got, want)
	}
}

func TestResolveSchedulingModelsForOpenAIAnthropicMessagesFableUsesSpecificEnv(t *testing.T) {
	clearSchedulingModelEnv(t)
	t.Setenv("AIRGATE_MODEL_FABLE", "openai/gpt-5.5-fable")
	t.Setenv("AIRGATE_MODEL_FABLE_FALLBACK", "oai/gpt-5.4-fable")
	t.Setenv("AIRGATE_MODEL_OPUS", "openai/gpt-5.5-opus")
	t.Setenv("AIRGATE_MODEL_OPUS_FALLBACK", "oai/gpt-5.4-opus")

	got := ResolveSchedulingModels("openai", "/v1/messages", "claude-fable-5")
	want := []string{"gpt-5.5-fable", "gpt-5.4-fable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveSchedulingModels() = %#v, want %#v", got, want)
	}
}

func TestResolveSchedulingModelsForOpenAIAnthropicMessagesFableIgnoresOpusEnv(t *testing.T) {
	clearSchedulingModelEnv(t)
	t.Setenv("AIRGATE_MODEL_OPUS", "openai/gpt-5.5-opus")
	t.Setenv("AIRGATE_MODEL_OPUS_FALLBACK", "oai/gpt-5.4-opus")

	got := ResolveSchedulingModels("openai", "/v1/messages", "claude-fable-5")
	want := []string{"gpt-5.5", "gpt-5.4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveSchedulingModels() = %#v, want %#v", got, want)
	}
}

func TestResolveSchedulingModelsIgnoreNonAnthropicRoutes(t *testing.T) {
	clearSchedulingModelEnv(t)

	tests := []struct {
		name string
		path string
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
		},
		{
			name: "absolute URL without API path",
			path: "https://example.com?trace=1",
		},
		{
			name: "root path",
			path: "/",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSchedulingModels("openai", tt.path, "claude-opus-4-7")
			want := []string{"claude-opus-4-7"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ResolveSchedulingModels() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestResolveSchedulingModelsForOpenAIResponsesCompact(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		model string
		want  []string
	}{
		{
			name:  "compact alias maps to base scheduling model",
			path:  "/v1/responses/compact",
			model: "gpt-5.5-openai-compact",
			want:  []string{"gpt-5.5"},
		},
		{
			name:  "base model stays base on compact path",
			path:  "/responses/compact?debug=1",
			model: "gpt-5.5",
			want:  []string{"gpt-5.5"},
		},
		{
			name:  "compact path is normalized",
			path:  "HTTPS://example.com/V1/RESPONSES/COMPACT/?debug=1",
			model: "gpt-5.5-openai-compact",
			want:  []string{"gpt-5.5"},
		},
		{
			name:  "compact alias is not stripped on normal responses path",
			path:  "/v1/responses",
			model: "gpt-5.5-openai-compact",
			want:  []string{"gpt-5.5-openai-compact"},
		},
		{
			name:  "compact substring in another route is not treated as compact endpoint",
			path:  "/v1/something-compact-else",
			model: "gpt-5.5-openai-compact",
			want:  []string{"gpt-5.5-openai-compact"},
		},
		{
			name:  "empty compact base is not stripped",
			path:  "/v1/responses/compact",
			model: "-openai-compact",
			want:  []string{"-openai-compact"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSchedulingModels("openai", tt.path, tt.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveSchedulingModels() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeMappedModelID(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		fallback string
		want     string
	}{
		{name: "blank", raw: " ", fallback: "fallback", want: "fallback"},
		{name: "openai prefix", raw: "openai/gpt-5", fallback: "fallback", want: "gpt-5"},
		{name: "oai prefix", raw: "oai/gpt-5", fallback: "fallback", want: "gpt-5"},
		{name: "provider suffix", raw: "provider@ oai/gpt-5", fallback: "fallback", want: "gpt-5"},
		{name: "prefix leaves empty", raw: "openai/", fallback: "fallback", want: "fallback"},
		{name: "trailing at ignored", raw: "provider@", fallback: "fallback", want: "provider@"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMappedModelID(tt.raw, tt.fallback); got != tt.want {
				t.Fatalf("normalizeMappedModelID(%q, %q) = %q, want %q", tt.raw, tt.fallback, got, tt.want)
			}
		})
	}
}

func clearSchedulingModelEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AIRGATE_DEFAULT_CLAUDE_MODEL",
		"AIRGATE_MODEL_FABLE",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"AIRGATE_MODEL_OPUS",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"AIRGATE_MODEL_SONNET",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"AIRGATE_MODEL_HAIKU",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"AIRGATE_MODEL_HAIKU_FALLBACK",
		"AIRGATE_MODEL_FABLE_FALLBACK",
		"AIRGATE_MODEL_OPUS_FALLBACK",
		"AIRGATE_MODEL_SONNET_FALLBACK",
		"AIRGATE_MODEL_DEFAULT_FALLBACK",
	} {
		t.Setenv(key, "")
	}
}
