package plugin

import (
	"math"
	"net/http"
	"reflect"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestSettlementUsesOnlyTypedAdjustments(t *testing.T) {
	standard, addon := 5.0, 0.4
	usage := &sdk.Usage{InputCost: 10, Metadata: map[string]string{
		"service_tier": "priority", "oauth_transport": "basispoints",
		"openai.billing.default_token_cost": "0", "openai.image.size": "1024x1024", "openai.image.count": "2", "billing.api_key_base_cost": "0",
	}}
	calc := billing.NewCalculator()
	rates := usageSettlementRates{Billing: 0.2, Sell: 3, Account: 0.7}
	legacy := settleUsage(calc, usage, rates)
	if _, exists := legacy.Metadata["billing.api_key_base_cost"]; exists {
		t.Fatal("opaque metadata forged settlement audit")
	}
	if legacy.Costs.ActualCost != 2 || legacy.Costs.BilledCost != 6 || legacy.Costs.AccountCost != 7 {
		t.Fatalf("opaque metadata affected charges: %+v", legacy.Costs)
	}
	usage.Billing = &sdk.BillingAdjustments{APIKeyBaseCost: &standard, ChargeAddon: &addon}
	settled := settleUsage(calc, usage, rates)
	if math.Abs(settled.Costs.ActualCost-2.4) > 1e-9 || math.Abs(settled.Costs.BilledCost-4.2) > 1e-9 || settled.Costs.AccountCost != 7 {
		t.Fatalf("settlement=%+v", settled.Costs)
	}
	if settled.Usage.ServiceTier != "priority" || settled.Costs.InputCost != 10 {
		t.Fatal("measured usage changed")
	}
	if settled.Metadata["billing.api_key_base_cost"] != "5" || settled.Metadata["billing.charge_addon"] != "0.4" {
		t.Fatal("generic audit fields missing")
	}
	usage.Metadata = map[string]string{"provider": "other", "service_tier": "something-new"}
	other := settleUsage(calc, usage, rates)
	if !reflect.DeepEqual(settled.Costs, other.Costs) {
		t.Fatal("settlement depends on provider metadata")
	}
}

func TestSettlementPreservesExplicitZeroAndRejectsInvalidQuotes(t *testing.T) {
	for _, cost := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		usage := &sdk.Usage{InputCost: 10, Billing: &sdk.BillingAdjustments{APIKeyBaseCost: &cost}}
		got := settleUsage(billing.NewCalculator(), usage, usageSettlementRates{Billing: 0.2, Sell: 3, Account: 0.7})
		want := 6.0
		if cost == 0 {
			want = 0
		}
		if got.Costs.BilledCost != want || got.Costs.ActualCost != 2 || got.Costs.AccountCost != 7 {
			t.Fatalf("cost=%g result=%+v", cost, got.Costs)
		}
	}
}

func TestPluginPricingSettingsAreTrustedAndProviderOpaque(t *testing.T) {
	const price = "X-Airgate-Plugin-Openai-Image-Price-1k"
	const policy = "X-Airgate-Plugin-Openai-Basispoints-Standard-Key-Billing"
	source := http.Header{}
	source.Set(price, "0")
	source.Set(policy, "true")
	settings := map[string]map[string]string{"openai": {"image_price_1k": "0.03", "basispoints_standard_key_billing": "false"}}
	for _, header := range []http.Header{
		buildHeaders(source, &auth.APIKeyInfo{GroupPluginSettings: settings}),
		hostForwardHeaders(hostForwardRequest{Headers: map[string]interface{}{price: "0", policy: "true"}}, routing.Candidate{GroupPluginSettings: settings}),
	} {
		if header.Get(price) != "0.03" || header.Get(policy) != "false" {
			t.Fatalf("untrusted pricing settings: %+v", header)
		}
	}
	if got := buildHeaders(source, &auth.APIKeyInfo{}); got.Get(price) != "" || got.Get(policy) != "" {
		t.Fatal("client injected billing policy")
	}
}
