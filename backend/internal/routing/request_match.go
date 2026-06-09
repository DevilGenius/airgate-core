package routing

import (
	"net/http"
	"strings"

	"github.com/DevilGenius/airgate-core/ent"
)

type GroupMatchInput struct {
	Path        string
	ClientModel string
	NeedsImage  bool
}

type GroupMatchResult struct {
	OK        bool
	Status    int
	ErrorType string
	Code      string
	Message   string
}

func GroupMatchesRequest(g *ent.Group, input GroupMatchInput) GroupMatchResult {
	if g == nil {
		return GroupMatchResult{}
	}
	if !strings.EqualFold(g.Platform, "openai") {
		return AllowGroup()
	}
	imageEnabled := pluginSettingEnabled(g.PluginSettings, "openai", "image_enabled")
	if imageEnabled == input.NeedsImage {
		return AllowGroup()
	}
	if input.NeedsImage {
		return DenyGroup(http.StatusForbidden, "invalid_request_error", "image_generation_disabled", "当前分组未开启图片生成功能")
	}
	return DenyGroup(http.StatusBadRequest, "invalid_request_error", "chat_generation_disabled", "当前分组未开启对话功能")
}

func AllowGroup() GroupMatchResult {
	return GroupMatchResult{OK: true}
}

func DenyGroup(status int, errType, code, message string) GroupMatchResult {
	return GroupMatchResult{
		Status:    status,
		ErrorType: errType,
		Code:      code,
		Message:   message,
	}
}

func pluginSettingEnabled(settings map[string]map[string]string, plugin, key string) bool {
	for pluginName, kv := range settings {
		if !strings.EqualFold(pluginName, plugin) {
			continue
		}
		for k, v := range kv {
			if strings.EqualFold(k, key) {
				return strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
	}
	return false
}
