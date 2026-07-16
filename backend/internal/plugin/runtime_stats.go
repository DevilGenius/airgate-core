package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const runtimeSafetyCacheStatsPath = "runtime/safety-cache"

type runtimeSafetyCacheStatsResponse struct {
	Text  runtimeSafetyCacheStatsItem `json:"text"`
	Image runtimeSafetyCacheStatsItem `json:"image"`
}

type runtimeSafetyCacheStatsItem struct {
	Size     int `json:"size"`
	Capacity int `json:"capacity"`
}

// SafetyCacheStats reads the OpenAI gateway's in-process safety cache usage.
func (m *Manager) SafetyCacheStats(ctx context.Context) (textSize, textCapacity, imageSize, imageCapacity int, err error) {
	if m == nil {
		return 0, 0, 0, 0, fmt.Errorf("plugin manager is unavailable")
	}
	inst := m.GetPluginByPlatform("openai")
	if inst == nil || inst.Gateway == nil {
		return 0, 0, 0, 0, fmt.Errorf("openai gateway is unavailable")
	}
	status, _, body, err := inst.Gateway.HandleHTTPRequest(ctx, http.MethodGet, runtimeSafetyCacheStatsPath, "", nil, nil)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("query openai safety cache stats: %w", err)
	}
	if status != http.StatusOK {
		return 0, 0, 0, 0, fmt.Errorf("query openai safety cache stats: status %d", status)
	}
	var stats runtimeSafetyCacheStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("decode openai safety cache stats: %w", err)
	}
	return stats.Text.Size, stats.Text.Capacity, stats.Image.Size, stats.Image.Capacity, nil
}
