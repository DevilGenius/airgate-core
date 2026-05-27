package plugin

import (
	"strconv"
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
	TextInputTokens       int
	ImageInputTokens      int
	ImageCount            int

	InputPrice           float64
	OutputPrice          float64
	CachedInputPrice     float64
	CacheCreationPrice   float64
	CacheCreation1hPrice float64
	ImageUnitPrice       float64
	ImageUnit            string

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
	snap := usageSnapshot{
		InputTokens:           usage.InputTokens,
		OutputTokens:          usage.OutputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		CacheCreationTokens:   usage.CacheCreationTokens,
		CacheCreation5mTokens: usage.CacheCreation5mTokens,
		CacheCreation1hTokens: usage.CacheCreation1hTokens,
		ReasoningOutputTokens: usage.ReasoningOutputTokens,
		TextInputTokens:       usage.TextInputTokens,
		ImageInputTokens:      usage.ImageInputTokens,
		ImageCount:            usage.ImageCount,
		InputPrice:            usage.InputPrice,
		OutputPrice:           usage.OutputPrice,
		CachedInputPrice:      usage.CachedInputPrice,
		CacheCreationPrice:    usage.CacheCreationPrice,
		CacheCreation1hPrice:  usage.CacheCreation1hPrice,
		ImageUnitPrice:        usage.ImageUnitPrice,
		ImageUnit:             usage.ImageUnit,
		InputCost:             usage.InputCost,
		OutputCost:            usage.OutputCost,
		CachedInputCost:       usage.CachedInputCost,
		CacheCreationCost:     usage.CacheCreationCost,
		ServiceTier:           usage.ServiceTier,
		ImageSize:             usage.ImageSize,
		FirstTokenMs:          usage.FirstTokenMs,
	}

	if usage.Metadata != nil {
		if snap.ImageSize == "" {
			snap.ImageSize = usage.Metadata["image_size"]
		}
	}

	return snap
}

func usageMetadataFromSDK(usage *sdk.Usage, snap usageSnapshot) map[string]string {
	meta := map[string]string{}
	if usage != nil {
		if snap.ImageSize == "" {
			putMetadata(meta, "image_size", usage.Metadata["image_size"])
			putMetadata(meta, "image_size", usage.Metadata["resolution"])
			putMetadata(meta, "image_size", usage.Metadata["size"])
		}
		if snap.ImageUnit == "" {
			putMetadata(meta, "image_unit", usage.Metadata["image_unit"])
			putMetadata(meta, "image_unit", usage.Metadata["unit"])
		}
	}

	putMetadata(meta, "image_size", snap.ImageSize)
	putMetadataInt(meta, "input_text_tokens", snap.TextInputTokens)
	putMetadataInt(meta, "input_image_tokens", snap.ImageInputTokens)
	putMetadataInt(meta, "images", snap.ImageCount)
	putMetadataFloat(meta, "image_unit_price", snap.ImageUnitPrice)
	putMetadata(meta, "image_unit", snap.ImageUnit)
	return meta
}

func putMetadata(meta map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	meta[key] = value
}

func putMetadataInt(meta map[string]string, key string, value int) {
	if value <= 0 {
		return
	}
	meta[key] = strconv.Itoa(value)
}

func putMetadataFloat(meta map[string]string, key string, value float64) {
	if value <= 0 {
		return
	}
	meta[key] = strconv.FormatFloat(value, 'f', -1, 64)
}
