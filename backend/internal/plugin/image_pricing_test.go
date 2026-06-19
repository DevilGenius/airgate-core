package plugin

import (
	"math"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/billing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestImageBillingCostOverride_UsesConfiguredTier(t *testing.T) {
	usage := &sdk.Usage{
		OutputCost: 0.40,
		Metadata: map[string]string{
			"openai.image.size":  "1672x941",
			"openai.image.count": "2",
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}

	got, ok := imageBillingCostFromSettings(usage, settings)
	if !ok {
		t.Fatal("expected override")
	}
	if math.Abs(got-0.16) > 1e-9 {
		t.Fatalf("override = %v, want 0.16 for two 2K images", got)
	}
}

func TestImageBillingCostOverride_FallsBackWhenTierUnset(t *testing.T) {
	usage := &sdk.Usage{
		OutputCost: 0.40,
		Metadata: map[string]string{
			"openai.image.size":  "3840x2160",
			"openai.image.count": "2",
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}

	if got, ok := imageBillingCostFromSettings(usage, settings); ok {
		t.Fatalf("override = %v, want fallback", got)
	}
}

func TestApplyImageBillingCostPolicy_ImagesAPIUsesOverride(t *testing.T) {
	usage := &sdk.Usage{
		InputCost:  0.10,
		OutputCost: 0.40,
		Metadata: map[string]string{
			"openai.image.size":  "1024x1024",
			"openai.image.count": "2",
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_1k": "0.03",
		},
	}
	input := billing.CalculateInput{
		InputCost:   usage.InputCost,
		OutputCost:  usage.OutputCost,
		BillingRate: 0.50,
		SellRate:    1,
	}

	applyImageBillingCostPolicy(&input, usage, settings, "/v1/images/generations")

	if input.BillingCostOverride == nil {
		t.Fatal("expected Images API billing override")
	}
	if input.BillingCostAddon != nil {
		t.Fatal("Images API must not use additive image billing")
	}
	if math.Abs(*input.BillingCostOverride-0.06) > 1e-9 {
		t.Fatalf("override = %v, want 0.06", *input.BillingCostOverride)
	}
}

func TestApplyImageBillingCostPolicy_ResponsesAddsImageCost(t *testing.T) {
	usage := &sdk.Usage{
		InputCost:  0.10,
		OutputCost: 0.40,
		Metadata: map[string]string{
			"openai.image.size":  "1672x941",
			"openai.image.count": "2",
		},
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}
	input := billing.CalculateInput{
		InputCost:   usage.InputCost,
		OutputCost:  usage.OutputCost,
		BillingRate: 0.50,
		SellRate:    1,
	}

	applyImageBillingCostPolicy(&input, usage, settings, "/v1/responses")

	if input.BillingCostOverride != nil {
		t.Fatal("Responses image_generation must not override token billing")
	}
	if input.BillingCostAddon == nil {
		t.Fatal("expected Responses image_generation billing addon")
	}
	if math.Abs(*input.BillingCostAddon-0.16) > 1e-9 {
		t.Fatalf("addon = %v, want 0.16", *input.BillingCostAddon)
	}

	calc := billing.NewCalculator().Calculate(input)
	if math.Abs(calc.ActualCost-0.41) > 1e-9 {
		t.Fatalf("actual cost = %v, want token cost 0.25 + image cost 0.16", calc.ActualCost)
	}
}

func TestUsageSnapshotFromSDKReadsPluginMetadata(t *testing.T) {
	usage := &sdk.Usage{
		InputTokens:     10,
		OutputCost:      0.40,
		ReasoningEffort: "high",
		Metadata: map[string]string{
			"service_tier":                    "priority",
			"openai.image.size":               "1672x941",
			"openai.image.input_text_tokens":  "3",
			"openai.image.input_image_tokens": "7",
			"openai.image.count":              "2",
			"openai.image.unit_price":         "0.2",
			"openai.image.unit":               "USD/image",
		},
	}

	snap := usageSnapshotFromSDK(usage)
	if snap.ServiceTier != "priority" || snap.ImageSize != "1672x941" {
		t.Fatalf("snapshot metadata strings = (%q, %q)", snap.ServiceTier, snap.ImageSize)
	}
	if snap.TextInputTokens != 3 || snap.ImageInputTokens != 7 || snap.ImageCount != 2 {
		t.Fatalf("snapshot metadata ints = (%d, %d, %d)", snap.TextInputTokens, snap.ImageInputTokens, snap.ImageCount)
	}
	if snap.ImageUnitPrice != 0.2 || snap.ImageUnit != "USD/image" {
		t.Fatalf("snapshot image price = (%v, %q)", snap.ImageUnitPrice, snap.ImageUnit)
	}
	if got := resolveReasoningEffort("", usage); got != "high" {
		t.Fatalf("resolveReasoningEffort = %q, want high", got)
	}
}

func TestUsageMetadataFromSDKPreservesPluginMetadata(t *testing.T) {
	usage := &sdk.Usage{
		Metadata: map[string]string{
			"custom.plugin.value":             "kept",
			"openai.image.size":               "1672x941",
			"openai.image.input_text_tokens":  "3",
			"claude.cache_creation_1h_tokens": "4",
		},
	}

	meta := usageMetadataFromSDK(usage, usageSnapshotFromSDK(usage))
	if meta["custom.plugin.value"] != "kept" {
		t.Fatalf("custom plugin metadata = %q, want kept", meta["custom.plugin.value"])
	}
	if meta["openai.image.size"] != "1672x941" || meta["openai.image.input_text_tokens"] != "3" {
		t.Fatalf("openai image metadata not preserved: %+v", meta)
	}
	if meta["claude.cache_creation_1h_tokens"] != "4" {
		t.Fatalf("claude metadata = %q, want 4", meta["claude.cache_creation_1h_tokens"])
	}
}

func TestImageTierForSize(t *testing.T) {
	tests := []struct {
		size      string
		wantTier  string
		wantPrice float64
	}{
		{size: "1024x1024", wantTier: "1k", wantPrice: 0.10},
		{size: "1672x941", wantTier: "2k", wantPrice: 0.20},
		{size: "2560x1440", wantTier: "2k", wantPrice: 0.20},
		{size: "2048x2048", wantTier: "2k", wantPrice: 0.20},
		{size: "3840x2160", wantTier: "4k", wantPrice: 0.40},
	}

	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			tier, price, ok := imageTierForSize(tt.size)
			if !ok {
				t.Fatal("expected tier")
			}
			if tier != tt.wantTier || price != tt.wantPrice {
				t.Fatalf("imageTierForSize() = (%q, %v), want (%q, %v)", tier, price, tt.wantTier, tt.wantPrice)
			}
		})
	}
}

func TestShouldForwardPluginSetting_HidesImagePrices(t *testing.T) {
	if shouldForwardPluginSetting("openai", "image_price_1k") {
		t.Fatal("image price settings should stay inside core")
	}
	if !shouldForwardPluginSetting("openai", "image_enabled") {
		t.Fatal("image_enabled should still be forwarded to the plugin")
	}
	if !shouldForwardPluginSetting("claude", "claude_code_only") {
		t.Fatal("non-openai plugin settings should still be forwarded")
	}
}
