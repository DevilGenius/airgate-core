package plugin

import (
	"math"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/billing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestBasispointsUsageBucketsAreChargedOnce(t *testing.T) {
	// Synthetic prices distinguish the buckets; these are not market rates.
	// Upstream input=1000 includes read=200 and creation=300, leaving input=500.
	usage := &sdk.Usage{InputTokens: 500, CachedInputTokens: 200, CacheCreationTokens: 300, OutputTokens: 50,
		InputCost: 0.001, CachedInputCost: 0.00004, CacheCreationCost: 0.00075, OutputCost: 0.0005,
		Metadata: map[string]string{"oauth_transport": "basispoints"}}
	snap := usageSnapshotFromSDK(usage)
	if snap.InputTokens != 500 || snap.CachedInputTokens != 200 || snap.CacheCreationTokens != 300 || snap.OutputTokens != 50 {
		t.Fatalf("usage buckets changed: %+v", snap)
	}
	if snap.InputTokens+snap.CachedInputTokens+snap.CacheCreationTokens+snap.OutputTokens != 1050 {
		t.Fatal("input/cache tokens counted twice")
	}
	got := billing.NewCalculator().Calculate(billing.CalculateInput{
		InputCost: snap.InputCost, CachedInputCost: snap.CachedInputCost, CacheCreationCost: snap.CacheCreationCost, OutputCost: snap.OutputCost,
		BillingRate: 2, SellRate: 3, AccountRate: 0.5,
	})
	const total = 0.00229
	for _, cost := range []struct {
		name      string
		got, want float64
	}{
		{"total", got.TotalCost, total}, {"actual", got.ActualCost, total * 2}, {"billed", got.BilledCost, total * 6}, {"account", got.AccountCost, total * 0.5},
	} {
		if math.Abs(cost.got-cost.want) > 1e-12 {
			t.Errorf("%s=%g want %g", cost.name, cost.got, cost.want)
		}
	}
}
