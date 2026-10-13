package plugin

import (
	"math"
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestForwardUsageBillingWithoutKeyOrGroup(t *testing.T) {
	ctx := t.Context()
	db := testdb.OpenMemoryEnt(t, "forward_usage_no_group", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	user, err := db.User.Create().SetEmail("test-billing@example.com").SetPasswordHash("hash").SetBalance(10).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.Account.Create().SetName("test").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := routegraph.RefreshUser(ctx, db, user.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { routegraph.RemoveUser(user.ID) })
	recorder := billing.NewRecorder(db, 1)
	t.Cleanup(recorder.Stop)
	biller := NewForwardUsageBilling(billing.NewCalculator(), recorder, nil)
	email, release, err := biller.Begin(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	addon := 0.25
	usage := &sdk.Usage{
		Model: "actual-model", InputTokens: 100, OutputTokens: 20,
		InputPrice: 2, OutputPrice: 4, InputCost: 1, OutputCost: 0.5,
		FirstEventMs: 12, FirstTokenMs: 18,
		Billing: &sdk.BillingAdjustments{ChargeAddon: &addon},
	}
	id, err := biller.Record(ctx, billing.UsageRecord{
		UserID: user.ID, UserEmail: email, AccountID: account.ID, Platform: "openai",
		Model: "requested-model", Endpoint: "/v1/chat/completions", Stream: true, DurationMs: 100,
	}, usage, ForwardUsageRates{Billing: 1, Sell: 1, Account: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	log, err := db.UsageLog.Query().WithGroup().WithAPIKey().Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if log.ID != id || log.Edges.Group != nil || log.Edges.APIKey != nil {
		t.Fatalf("unexpected usage identity: %+v", log)
	}
	if log.Model != "actual-model" || log.InputTokens != 100 || log.OutputTokens != 20 || log.InputPrice != 2 || log.OutputPrice != 4 || log.FirstEventMs != 12 || log.FirstTokenMs != 18 {
		t.Fatalf("lost usage fields: %+v", log)
	}
	if log.ActualCost != 1.75 || log.BilledCost != 1.75 || log.AccountCost != 0.75 || log.RateMultiplier != 1 || log.SellRate != 1 || log.UserEmailSnapshot != email {
		t.Fatalf("incorrect settlement: %+v", log)
	}
	updated, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(updated.Balance-8.25) > 1e-9 {
		t.Fatalf("balance=%v", updated.Balance)
	}

	if _, err := db.User.UpdateOneID(user.ID).SetBalance(0).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := routegraph.RefreshUser(ctx, db, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, release, err := biller.Begin(user.ID); err == nil {
		release()
		t.Fatal("accepted user with no balance")
	}
}
