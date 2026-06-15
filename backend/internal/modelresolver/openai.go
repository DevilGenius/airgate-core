package modelresolver

import (
	"net/url"
	"os"
	"strings"
)

const openAIPlatform = "openai"

type openAIResolver struct{}

func init() {
	Register(openAIPlatform, openAIResolver{})
}

func (openAIResolver) ResolveSchedulingModels(path, clientModel string) []string {
	if isResponsesCompactForwardPath(path) {
		return compactUniqueModels(openAICompactSchedulingModel(clientModel))
	}
	if !isAnthropicMessagesForwardPath(path) {
		return compactUniqueModels(clientModel)
	}
	return openAIAnthropicSchedulingModels(clientModel)
}

func isResponsesCompactForwardPath(path string) bool {
	path = strings.TrimSpace(path)
	switch path {
	case "/v1/responses/compact", "/responses/compact":
		return true
	}
	if !strings.Contains(path, "compact") && !strings.Contains(path, "Compact") && !strings.Contains(path, "COMPACT") {
		return false
	}
	path = normalizeForwardPath(path)
	return path == "/v1/responses/compact" || path == "/responses/compact"
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

func normalizeForwardPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if u, err := url.Parse(path); err == nil && u != nil {
		path = u.Path
	} else if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	path = strings.TrimRight(strings.ToLower(strings.TrimSpace(path)), "/")
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func pathHasAPIPrefix(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/'
}

func openAICompactSchedulingModel(clientModel string) string {
	model := strings.TrimSpace(clientModel)
	base, ok := openAICompactBaseModel(model)
	if ok {
		return base
	}
	return model
}

func openAICompactBaseModel(model string) (string, bool) {
	const suffix = "-openai-compact"
	model = strings.TrimSpace(model)
	if model == "" || !strings.HasSuffix(strings.ToLower(model), suffix) {
		return "", false
	}
	base := strings.TrimSpace(model[:len(model)-len(suffix)])
	if base == "" {
		return "", false
	}
	return base, true
}

func openAIAnthropicSchedulingModels(clientModel string) []string {
	model := strings.ToLower(strings.TrimSpace(clientModel))
	if model == "" {
		return compactUniqueModels(clientModel)
	}

	defaultTarget := normalizedEnvModel("gpt-5.5", "AIRGATE_DEFAULT_CLAUDE_MODEL")
	switch {
	case strings.HasPrefix(model, "claude-fable-"):
		return compactUniqueModels(
			normalizedEnvModel("gpt-5.5", "AIRGATE_MODEL_FABLE", "ANTHROPIC_DEFAULT_FABLE_MODEL"),
			normalizedEnvModel("gpt-5.4", "AIRGATE_MODEL_FABLE_FALLBACK"),
		)
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
		return compactUniqueModels(clientModel)
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
