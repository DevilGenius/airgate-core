package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type runtimeCacheStatsResponse struct {
	TextRejection    runtimeCacheStatsItem `json:"text_rejection"`
	CyberRejection   runtimeCacheStatsItem `json:"cyber_rejection"`
	PromptRejection  runtimeCacheStatsItem `json:"prompt_rejection"`
	ImageRejection   runtimeCacheStatsItem `json:"image_rejection"`
	EncryptedContent runtimeCacheStatsItem `json:"encrypted_content"`
	ContextWindow    runtimeCacheStatsItem `json:"context_window"`
}

type runtimeCacheStatsItem struct {
	Size              int `json:"size"`
	Capacity          int `json:"capacity"`
	CybersecurityRisk int `json:"cybersecurity_risk"`
	InvalidPrompt     int `json:"invalid_prompt"`
}

// RuntimeCacheStats reads the OpenAI gateway's in-process cache usage.
func (m *Manager) RuntimeCacheStats(ctx context.Context) (
	textRejectionSize, textRejectionCapacity,
	textRejectionCybersecurityRisk, textRejectionInvalidPrompt,
	cyberRejectionSize, cyberRejectionCapacity,
	promptRejectionSize, promptRejectionCapacity,
	imageRejectionSize, imageRejectionCapacity,
	encryptedContentSize, encryptedContentCapacity,
	contextWindowSize, contextWindowCapacity int,
	err error,
) {
	if m == nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("plugin manager is unavailable")
	}
	inst := m.GetPluginByPlatform("openai")
	if inst == nil || inst.Gateway == nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("openai gateway is unavailable")
	}
	status, _, body, err := inst.Gateway.HandleHTTPRequest(ctx, http.MethodGet, runtimeHashPath, "", nil, nil)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("query openai runtime cache stats: %w", err)
	}
	if status != http.StatusOK {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("query openai runtime cache stats: status %d", status)
	}
	var stats runtimeCacheStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("decode openai runtime cache stats: %w", err)
	}
	return stats.TextRejection.Size, stats.TextRejection.Capacity,
		stats.TextRejection.CybersecurityRisk, stats.TextRejection.InvalidPrompt,
		stats.CyberRejection.Size, stats.CyberRejection.Capacity,
		stats.PromptRejection.Size, stats.PromptRejection.Capacity,
		stats.ImageRejection.Size, stats.ImageRejection.Capacity,
		stats.EncryptedContent.Size, stats.EncryptedContent.Capacity,
		stats.ContextWindow.Size, stats.ContextWindow.Capacity,
		nil
}
