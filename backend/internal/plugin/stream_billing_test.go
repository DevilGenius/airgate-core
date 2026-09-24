package plugin

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestStreamFinalOutcomeBillingSurvivesCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		kind sdk.OutcomeKind
		err  error
	}{
		{"completed_then_canceled", sdk.OutcomeSuccess, nil},
		{"terminal_write_failed", sdk.OutcomeStreamAborted, io.ErrClosedPipe},
		{"upstream_aborted_with_usage", sdk.OutcomeStreamAborted, nil},
		{"client_error_with_usage", sdk.OutcomeClientError, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			db := testdb.OpenMemoryEnt(t, "stream_billing_"+test.name, schema.WithGlobalUniqueID(false))
			t.Cleanup(func() { _ = db.Close() })
			user, err := db.User.Create().SetEmail(test.name + "@example.com").SetPasswordHash("hash").SetBalance(10).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			group, err := db.Group.Create().SetName(test.name).SetPlatform("openai").Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			account, err := db.Account.Create().SetName(test.name).SetPlatform("openai").SetType("apikey").SetRateMultiplier(1).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			key, err := db.APIKey.Create().SetName(test.name).SetKeyHash(test.name).SetUserID(user.ID).SetGroupID(group.ID).SetSellRate(1).Save(ctx)
			if err != nil {
				t.Fatal(err)
			}
			recorder := billing.NewRecorder(db, 1)
			// Keep the async queue full, forcing Record through its durable sync
			// fallback so assertions examine actual usage rows and balance changes.
			if err := recorder.Record(billing.UsageRecord{UserID: user.ID, AccountID: account.ID, GroupID: group.ID, Model: "prefill", Platform: "openai"}); err != nil {
				t.Fatal(err)
			}
			monitor := &captureRequestMonitorRecorder{}
			f := &Forwarder{scheduler: scheduler.NewScheduler(db, nil), calculator: billing.NewCalculator(), recorder: recorder, requestMonitor: monitor}
			state := &forwardState{startedAt: time.Now(), stream: true, requestPath: "/v1/responses", requestedPlatform: "openai", model: "gpt-6-astra", account: account, plugin: &PluginInstance{Name: "openai", Platform: "openai"}, keyInfo: &auth.APIKeyInfo{UserID: user.ID, UserEmail: user.Email, KeyID: key.ID, GroupID: group.ID, GroupPlatform: "openai", GroupRateMultiplier: 1, SellRate: 1}}
			c, response := pluginTestContext(http.MethodPost, "/v1/responses")
			requestCtx, cancel := context.WithCancel(c.Request.Context())
			c.Request = c.Request.WithContext(requestCtx)
			_, _ = c.Writer.Write([]byte("data: output\n\n"))
			cancel()
			execution := forwardExecution{outcome: sdk.ForwardOutcome{Kind: test.kind, Upstream: sdk.UpstreamResponse{StatusCode: 200}, Usage: &sdk.Usage{Model: "gpt-6-astra", InputTokens: 100, OutputTokens: 7, CachedInputTokens: 20, InputCost: 0.25, OutputCost: 0.75}}, err: test.err, duration: time.Second}
			if !hasForwardResult(execution) {
				t.Fatal("confirmed result would take the 499 early return")
			}
			f.writeResult(c, state, execution)
			rows, err := db.UsageLog.Query().All(ctx)
			if err != nil || len(rows) != 1 {
				t.Fatalf("usage must be recorded exactly once: rows=%d err=%v", len(rows), err)
			}
			row := rows[0]
			if row.InputTokens != 100 || row.OutputTokens != 7 || row.CachedInputTokens != 20 || row.TotalCost != 1 || row.BilledCost != 1 {
				t.Fatalf("unexpected usage: %+v", row)
			}
			after, err := db.User.Get(ctx, user.ID)
			if err != nil || after.Balance != 9 {
				t.Fatalf("balance=%v err=%v, want one charge", after, err)
			}
			keyAfter, err := db.APIKey.Get(ctx, key.ID)
			if err != nil || keyAfter.UsedQuota != 1 || keyAfter.UsedQuotaActual != 1 {
				t.Fatalf("API key quotas must be charged once: key=%v err=%v", keyAfter, err)
			}
			if response.Code != 200 {
				t.Fatalf("committed status changed: %d", response.Code)
			}
			for _, event := range monitor.events {
				if test.kind == sdk.OutcomeSuccess && event.ErrorCode == "client_closed_request" {
					t.Fatal("completed request misclassified as client closed")
				}
			}
		})
	}
}
