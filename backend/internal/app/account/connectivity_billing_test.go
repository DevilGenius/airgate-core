package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type testConnectivityBilling struct {
	userID     int
	released   bool
	beginErr   error
	records    []billing.UsageRecord
	rates      []plugin.ForwardUsageRates
	contextErr error
}

func (b *testConnectivityBilling) Begin(userID int) (string, func(), error) {
	b.userID = userID
	return "admin@example.com", func() { b.released = true }, b.beginErr
}

func (b *testConnectivityBilling) Record(ctx context.Context, record billing.UsageRecord, _ *sdk.Usage, rates plugin.ForwardUsageRates) (int, error) {
	b.contextErr = ctx.Err()
	b.records = append(b.records, record)
	b.rates = append(b.rates, rates)
	return 1, nil
}

func TestConnectivityUsageSettlement(t *testing.T) {
	for _, name := range []string{"success", "degraded", "cancelled", "no-usage", "admission-denied"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			biller := &testConnectivityBilling{}
			if name == "admission-denied" {
				biller.beginErr = billing.ErrBillingBusy
			}
			runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
				platform: "openai",
				handle: func(context.Context, string, string, string, http.Header, []byte) (int, http.Header, []byte, error) {
					if name == "degraded" {
						return 200, nil, []byte(`{"enabled":true,"prompt":"compute","pattern":"^42$"}`), nil
					}
					return 200, nil, []byte(`{"enabled":false}`), nil
				},
				forward: func(context.Context, *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
					calls++
					outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: &sdk.Usage{InputCost: 1}}
					if name == "cancelled" {
						cancel()
						outcome.Kind = sdk.OutcomeStreamAborted
					}
					if name == "no-usage" {
						outcome.Usage = nil
					}
					return outcome, nil
				},
			})
			defer runtime.cleanup()
			service := NewService(stubRepository{
				findByID: func(context.Context, int, LoadOptions) (Account, error) {
					return Account{ID: 11, Platform: "openai", RateMultiplier: 0.5}, nil
				},
				update: func(context.Context, int, UpdateInput) (Account, error) { return Account{ID: 11}, nil },
			}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, nil)
			service.SetConnectivityTestBilling(biller)
			plan, err := service.PrepareConnectivityTest(ctx, 11, "model-test", 7)
			if err != nil {
				t.Fatal(err)
			}
			_, err = plan.RunWithTiming(ctx, httptest.NewRecorder())
			if name == "admission-denied" {
				if !errors.Is(err, billing.ErrBillingBusy) || calls != 0 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
				return
			}
			if biller.userID != 7 || !biller.released {
				t.Fatalf("billing lifecycle: %+v", biller)
			}
			if name == "degraded" && err == nil {
				t.Fatal("expected cognition failure")
			}
			if name == "no-usage" {
				if len(biller.records) != 0 {
					t.Fatal("invented usage")
				}
				return
			}
			if len(biller.records) != 1 || biller.contextErr != nil {
				t.Fatalf("billing=%+v", biller)
			}
			record := biller.records[0]
			if record.UserID != 7 || record.AccountID != 11 || record.APIKeyID != 0 || record.GroupID != 0 || record.UserEmail != "admin@example.com" {
				t.Fatalf("record=%+v", record)
			}
			if biller.rates[0] != (plugin.ForwardUsageRates{Billing: 1, Sell: 1, Account: 0.5}) {
				t.Fatalf("rates=%+v", biller.rates)
			}
		})
	}
}
