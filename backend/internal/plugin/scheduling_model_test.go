package plugin

import (
	"reflect"
	"testing"
)

func TestSchedulingModelsForOpenAIAnthropicMessages(t *testing.T) {
	clearSchedulingModelEnv(t)

	tests := []struct {
		name  string
		path  string
		model string
		want  []string
	}{
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
			name:  "非 Claude 模型保持原样",
			path:  "/v1/messages",
			model: "gpt-5.4",
			want:  []string{"gpt-5.4"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := schedulingModelsForRequest("openai", tt.path, tt.model)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("schedulingModelsForRequest() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSchedulingModelsForOpenAIAnthropicMessagesUsesEnvOverride(t *testing.T) {
	clearSchedulingModelEnv(t)
	t.Setenv("AIRGATE_MODEL_OPUS", "openai/gpt-5.4")
	t.Setenv("AIRGATE_MODEL_OPUS_FALLBACK", "oai/gpt-5.4")

	got := schedulingModelsForRequest("openai", "/v1/messages", "claude-opus-4-7")
	want := []string{"gpt-5.4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schedulingModelsForRequest() = %#v, want %#v", got, want)
	}
}

func TestSchedulingModelsIgnoreNonAnthropicRoutes(t *testing.T) {
	clearSchedulingModelEnv(t)

	got := schedulingModelsForRequest("openai", "/v1/chat/completions", "claude-opus-4-7")
	want := []string{"claude-opus-4-7"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schedulingModelsForRequest() = %#v, want %#v", got, want)
	}
}

func clearSchedulingModelEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AIRGATE_DEFAULT_CLAUDE_MODEL",
		"AIRGATE_MODEL_OPUS",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"AIRGATE_MODEL_SONNET",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"AIRGATE_MODEL_HAIKU",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"AIRGATE_MODEL_HAIKU_FALLBACK",
		"AIRGATE_MODEL_OPUS_FALLBACK",
		"AIRGATE_MODEL_SONNET_FALLBACK",
		"AIRGATE_MODEL_DEFAULT_FALLBACK",
	} {
		t.Setenv(key, "")
	}
}
