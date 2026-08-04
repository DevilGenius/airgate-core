package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type runtimeCacheStatsResponse struct {
	TextRejection    runtimeCacheStatsItem `json:"text_rejection"`
	ImageRejection   runtimeCacheStatsItem `json:"image_rejection"`
	EncryptedContent runtimeCacheStatsItem `json:"encrypted_content"`
	ContextWindow    runtimeCacheStatsItem `json:"context_window"`
}

type runtimeCacheStatsItem struct {
	Size     int `json:"size"`
	Capacity int `json:"capacity"`
}

// RuntimeCacheStats reads the OpenAI gateway's in-process cache usage.
func (m *Manager) RuntimeCacheStats(ctx context.Context) (
	textRejectionSize, textRejectionCapacity,
	imageRejectionSize, imageRejectionCapacity,
	encryptedContentSize, encryptedContentCapacity,
	contextWindowSize, contextWindowCapacity int,
	err error,
) {
	if m == nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("plugin manager is unavailable")
	}
	inst := m.GetPluginByPlatform("openai")
	if inst == nil || inst.Gateway == nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("openai gateway is unavailable")
	}
	status, _, body, err := inst.Gateway.HandleHTTPRequest(ctx, http.MethodGet, runtimeHashPath, "", nil, nil)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("query openai runtime cache stats: %w", err)
	}
	if status != http.StatusOK {
		return 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("query openai runtime cache stats: status %d", status)
	}
	var stats runtimeCacheStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("decode openai runtime cache stats: %w", err)
	}
	return stats.TextRejection.Size, stats.TextRejection.Capacity,
		stats.ImageRejection.Size, stats.ImageRejection.Capacity,
		stats.EncryptedContent.Size, stats.EncryptedContent.Capacity,
		stats.ContextWindow.Size, stats.ContextWindow.Capacity,
		nil
}
