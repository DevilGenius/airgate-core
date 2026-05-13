package plugin

import (
	"math"
	"testing"

	sdk "github.com/DouDOU-start/airgate-sdk"
)

func TestImageOutputBillingOverride_UsesConfiguredTier(t *testing.T) {
	usage := &sdk.Usage{
		OutputCost: 0.40,
		ImageSize:  "1672x941",
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}

	got, ok := imageOutputBillingOverride(usage, settings)
	if !ok {
		t.Fatal("expected override")
	}
	if math.Abs(got-0.16) > 1e-9 {
		t.Fatalf("override = %v, want 0.16 for two 2K images", got)
	}
}

func TestImageOutputBillingOverride_FallsBackWhenTierUnset(t *testing.T) {
	usage := &sdk.Usage{
		OutputCost: 0.40,
		ImageSize:  "3840x2160",
	}
	settings := map[string]map[string]string{
		"openai": {
			"image_price_2k": "0.08",
		},
	}

	if got, ok := imageOutputBillingOverride(usage, settings); ok {
		t.Fatalf("override = %v, want fallback", got)
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
