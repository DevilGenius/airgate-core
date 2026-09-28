package plugin

import (
	"math"
	"strconv"

	"github.com/DevilGenius/airgate-core/internal/billing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

const basispointsStandardKeyBillingKey = "basispoints_standard_key_billing"
const openAIDefaultTokenCostMetadata = "openai.billing.default_token_cost"

// Shared by external API forwarding and host gateway.forward billing.
func applyUsageBillingCostPolicy(input *billing.CalculateInput, usage *sdk.Usage, settings map[string]map[string]string, requestPath string) {
	applyImageBillingCostPolicy(input, usage, settings, requestPath)
	if input == nil || usage == nil {
		return
	}
	options := settings[openAIPluginSettingsKey]
	if options["basispoints"] != "true" || options[basispointsStandardKeyBillingKey] != "true" {
		return
	}
	if metadataText(usage.Metadata, "oauth_transport") != "basispoints" {
		return
	}
	// Use a quote from the pricing-owning plugin, never assume a fixed tier ratio.
	// Older plugins or invalid quotes keep the established charge instead of zero.
	value, err := strconv.ParseFloat(metadataText(usage.Metadata, openAIDefaultTokenCostMetadata), 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	input.APIKeyBaseCostOverride = &value
}

func usageBillingMetadata(usage *sdk.Usage, snapshot usageSnapshot, input billing.CalculateInput) map[string]string {
	metadata := usageMetadataFromSDK(usage, snapshot)
	if input.APIKeyBaseCostOverride != nil {
		metadata["api_key.billing_service_tier"] = "default"
		metadata["api_key.billing_policy"] = "basispoints_standard"
	}
	return metadata
}
