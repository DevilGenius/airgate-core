package plugin

import (
	"strings"

	sdk "github.com/DouDOU-start/airgate-sdk/sdkgo"
)

type usageSnapshot struct {
	InputTokens           int
	OutputTokens          int
	CachedInputTokens     int
	CacheCreationTokens   int
	CacheCreation5mTokens int
	CacheCreation1hTokens int
	ReasoningOutputTokens int

	InputPrice           float64
	OutputPrice          float64
	CachedInputPrice     float64
	CacheCreationPrice   float64
	CacheCreation1hPrice float64

	InputCost         float64
	OutputCost        float64
	CachedInputCost   float64
	CacheCreationCost float64

	ServiceTier  string
	ImageSize    string
	FirstTokenMs int64
}

func usageSnapshotFromSDK(usage *sdk.Usage) usageSnapshot {
	if usage == nil {
		return usageSnapshot{}
	}
	snap := usageSnapshot{FirstTokenMs: usage.FirstTokenMs}

	accountCost := usage.AccountCost
	if accountCost <= 0 {
		for _, metric := range usage.Metrics {
			accountCost += metric.AccountCost
		}
		for _, detail := range usage.CostDetails {
			accountCost += detail.AccountCost
		}
	}
	snap.InputCost = accountCost

	for _, metric := range usage.Metrics {
		key := normalizedUsageKey(metric.Key, metric.Kind, metric.Label)
		switch key {
		case "input_tokens", "input_token", "prompt_tokens", "prompt_token":
			snap.InputTokens += int(metric.Value)
		case "output_tokens", "output_token", "completion_tokens", "completion_token":
			snap.OutputTokens += int(metric.Value)
		case "cached_input_tokens", "cached_input_token", "cache_read_tokens", "cache_read_token":
			snap.CachedInputTokens += int(metric.Value)
		case "cache_creation_tokens", "cache_creation_token":
			snap.CacheCreationTokens += int(metric.Value)
		case "cache_creation_5m_tokens", "cache_creation_5m_token":
			snap.CacheCreation5mTokens += int(metric.Value)
		case "cache_creation_1h_tokens", "cache_creation_1h_token":
			snap.CacheCreation1hTokens += int(metric.Value)
		case "reasoning_output_tokens", "reasoning_tokens", "reasoning_token":
			snap.ReasoningOutputTokens += int(metric.Value)
		}
	}

	for _, attr := range usage.Attributes {
		key := normalizedUsageKey(attr.Key, attr.Kind, attr.Label)
		switch key {
		case "service_tier", "tier":
			if snap.ServiceTier == "" {
				snap.ServiceTier = attr.Value
			}
		case "image_size", "resolution", "size":
			if snap.ImageSize == "" {
				snap.ImageSize = attr.Value
			}
		}
	}

	if usage.Metadata != nil {
		if snap.ServiceTier == "" {
			snap.ServiceTier = usage.Metadata["service_tier"]
		}
		if snap.ImageSize == "" {
			snap.ImageSize = usage.Metadata["image_size"]
		}
	}

	return snap
}

func normalizedUsageKey(parts ...string) string {
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(part, "-", "_")
		part = strings.ReplaceAll(part, " ", "_")
		return part
	}
	return ""
}
