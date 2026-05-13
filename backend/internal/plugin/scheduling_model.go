package plugin

import (
	"net/url"
	"os"
	"strings"
)

// schedulingModelsForRequest 返回调度层使用的模型候选列表。
//
// OpenAI 插件的 /v1/messages 是 Anthropic Messages 协议翻译入口：客户端会传
// claude-*，插件实际会先映射到 GPT 模型再调用 OpenAI Responses。因此 core 选号
// 不能直接拿 claude-* 去套 OpenAI 分组的 model_routing，否则还没进插件就会报
// "无可用账户"。
func schedulingModelsForRequest(platform, path, requestedModel string) []string {
	if !strings.EqualFold(strings.TrimSpace(platform), "openai") || !isAnthropicMessagesForwardPath(path) {
		return compactUniqueModels(requestedModel)
	}
	return openAIAnthropicSchedulingModels(requestedModel)
}

func isAnthropicMessagesForwardPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if u, err := url.Parse(path); err == nil && u != nil {
		path = u.Path
	} else if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	return pathHasAPIPrefix(path, "/v1/messages") || pathHasAPIPrefix(path, "/messages")
}

func pathHasAPIPrefix(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/'
}

func openAIAnthropicSchedulingModels(requestedModel string) []string {
	model := strings.ToLower(strings.TrimSpace(requestedModel))
	if model == "" {
		return compactUniqueModels(requestedModel)
	}

	defaultTarget := normalizedEnvModel("gpt-5.5", "AIRGATE_DEFAULT_CLAUDE_MODEL")
	switch {
	case strings.HasPrefix(model, "claude-haiku-"):
		return compactUniqueModels(
			normalizedEnvModel("gpt-5.3-codex-spark", "AIRGATE_MODEL_HAIKU", "ANTHROPIC_DEFAULT_HAIKU_MODEL"),
			normalizedEnvModel("gpt-5.4-mini", "AIRGATE_MODEL_HAIKU_FALLBACK"),
		)
	case strings.HasPrefix(model, "claude-sonnet-"):
		return compactUniqueModels(
			normalizedEnvModel(defaultTarget, "AIRGATE_MODEL_SONNET", "ANTHROPIC_DEFAULT_SONNET_MODEL"),
			normalizedEnvModel("gpt-5.4", "AIRGATE_MODEL_SONNET_FALLBACK"),
		)
	case strings.HasPrefix(model, "claude-opus-"):
		return compactUniqueModels(
			normalizedEnvModel(defaultTarget, "AIRGATE_MODEL_OPUS", "ANTHROPIC_DEFAULT_OPUS_MODEL"),
			normalizedEnvModel("gpt-5.4", "AIRGATE_MODEL_OPUS_FALLBACK"),
		)
	case strings.HasPrefix(model, "claude-3") || strings.HasPrefix(model, "claude-"):
		return compactUniqueModels(
			defaultTarget,
			normalizedEnvModel("gpt-5.4", "AIRGATE_MODEL_DEFAULT_FALLBACK"),
		)
	default:
		return compactUniqueModels(requestedModel)
	}
}

func normalizedEnvModel(fallback string, keys ...string) string {
	for _, key := range keys {
		if value := normalizeMappedModelID(os.Getenv(key), ""); value != "" {
			return value
		}
	}
	return normalizeMappedModelID(fallback, fallback)
}

func normalizeMappedModelID(raw, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	if idx := strings.LastIndex(value, "@"); idx >= 0 && idx+1 < len(value) {
		value = strings.TrimSpace(value[idx+1:])
	}
	value = strings.TrimPrefix(value, "openai/")
	value = strings.TrimPrefix(value, "oai/")
	if value == "" {
		return fallback
	}
	return value
}

func compactUniqueModels(models ...string) []string {
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	return out
}
