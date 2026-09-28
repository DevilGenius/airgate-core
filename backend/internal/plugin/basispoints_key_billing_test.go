package plugin

import (
	"math"
	"net/http"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestBasispointsStandardKeyBillingPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, tier, transport, enabled, bps, quote string
		want                                       bool
	}{
		{"priority", "priority", "basispoints", "true", "true", "5", true},
		{"fast", "fast", "basispoints", "true", "true", "5", true},
		{"flex", "flex", "basispoints", "true", "true", "5", true},
		{"disabled", "priority", "basispoints", "false", "true", "5", false},
		{"bps-disabled", "priority", "basispoints", "true", "false", "5", false},
		{"native", "priority", "native", "true", "true", "5", false},
		{"ordinary", "default", "basispoints", "true", "true", "5", true},
		{"unknown-tier", "future-tier", "basispoints", "true", "true", "5", true},
		{"missing-tier", "", "basispoints", "true", "true", "5", true},
		{"old-plugin", "priority", "basispoints", "true", "true", "", false},
		{"nan", "priority", "basispoints", "true", "true", "NaN", false},
		{"infinite", "priority", "basispoints", "true", "true", "+Inf", false},
		{"negative", "priority", "basispoints", "true", "true", "-1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := 10.0
			if tc.tier == "flex" {
				actual = 2.5
			}
			usage := &sdk.Usage{Model: "gpt-5.6-sol", InputCost: actual, Metadata: map[string]string{"service_tier": tc.tier, "oauth_transport": tc.transport, openAIDefaultTokenCostMetadata: tc.quote}}
			settings := map[string]map[string]string{"openai": {"basispoints": tc.bps, basispointsStandardKeyBillingKey: tc.enabled}}
			input := billing.CalculateInput{InputCost: actual, BillingRate: 0.2, SellRate: 3, AccountRate: 0.7}
			before := billing.NewCalculator().Calculate(input)
			applyUsageBillingCostPolicy(&input, usage, settings, "/v1/responses")
			after := billing.NewCalculator().Calculate(input)
			if (input.APIKeyBaseCostOverride != nil) != tc.want {
				t.Fatalf("override=%v expected=%v", input.APIKeyBaseCostOverride, tc.want)
			}
			wantKey := before.BilledCost
			if tc.want {
				wantKey = 3
			}
			if math.Abs(after.BilledCost-wantKey) > 1e-9 || after.ActualCost != before.ActualCost || after.AccountCost != before.AccountCost {
				t.Fatalf("before=%+v after=%+v", before, after)
			}
			snap := usageSnapshotFromSDK(usage)
			meta := usageBillingMetadata(usage, snap, input)
			if snap.ServiceTier != tc.tier || usage.InputCost != actual {
				t.Fatal("actual usage or tier changed")
			}
			if (meta["api_key.billing_service_tier"] == "default") != tc.want {
				t.Fatal("missing applied policy audit marker")
			}
		})
	}
}

func TestBasispointsKeyPolicyNotControlledByClientHeaders(t *testing.T) {
	const header = "X-Airgate-Plugin-Openai-Basispoints-Standard-Key-Billing"
	source := http.Header{}
	source.Set(header, "true")
	settings := map[string]map[string]string{"openai": {"basispoints": "true", basispointsStandardKeyBillingKey: "true"}}
	got := buildHeaders(source, &auth.APIKeyInfo{GroupPluginSettings: settings})
	if got.Get(header) != "" {
		t.Fatal("Core-only billing policy leaked to plugin")
	}
	host := hostForwardHeaders(hostForwardRequest{Headers: map[string]interface{}{header: "true"}}, routing.Candidate{GroupPluginSettings: settings})
	if host.Get(header) != "" {
		t.Fatal("host forwarding leaked Core-only billing policy")
	}
}
