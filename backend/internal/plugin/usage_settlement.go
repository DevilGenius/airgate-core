package plugin

import (
	"log/slog"
	"strconv"

	"github.com/DevilGenius/airgate-core/internal/billing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type usageSettlementRates struct{ Billing, Sell, Account float64 }
type usageSettlement struct {
	Usage    usageSnapshot
	Costs    billing.CalculateResult
	Metadata map[string]string
}

// settleUsage is the single bridge between plugin pricing and Core accounting.
// No provider names, transport modes, tier strings, image sizes or private
// plugin settings participate in this calculation.
func settleUsage(calculator *billing.Calculator, usage *sdk.Usage, rates usageSettlementRates) usageSettlement {
	snapshot := usageSnapshotFromSDK(usage)
	input := billing.CalculateInput{
		InputCost: snapshot.InputCost, OutputCost: snapshot.OutputCost,
		CachedInputCost: snapshot.CachedInputCost, CacheCreationCost: snapshot.CacheCreationCost,
		BillingRate: rates.Billing, SellRate: rates.Sell, AccountRate: rates.Account,
	}
	metadata := usageMetadataFromSDK(usage, snapshot)
	// Settlement audit keys are reconstructed from the typed contract only.
	for _, key := range []string{"billing.contract", "billing.charge_override", "billing.charge_addon", "billing.api_key_base_cost"} {
		delete(metadata, key)
	}
	if usage != nil && usage.Billing != nil {
		if err := usage.Billing.Validate(); err != nil {
			// Invalid optional quotes cannot turn a valid measured charge into zero.
			slog.Warn("invalid_plugin_billing_adjustments", "error", err)
		} else {
			input.BillingCostOverride = usage.Billing.ChargeOverride
			input.BillingCostAddon = usage.Billing.ChargeAddon
			input.APIKeyBaseCostOverride = usage.Billing.APIKeyBaseCost
			metadata["billing.contract"] = "1"
			for key, value := range map[string]*float64{
				"billing.charge_override":   input.BillingCostOverride,
				"billing.charge_addon":      input.BillingCostAddon,
				"billing.api_key_base_cost": input.APIKeyBaseCostOverride,
			} {
				if value != nil {
					metadata[key] = strconv.FormatFloat(*value, 'g', -1, 64)
				}
			}
		}
	}
	return usageSettlement{Usage: snapshot, Costs: calculator.Calculate(input), Metadata: metadata}
}
