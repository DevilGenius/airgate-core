package billing

import (
	"context"
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/usagelog"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestRecordSyncPersistsUserEmailSnapshot(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "billing_recorder", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-snapshot@example.com")
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("acc").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	recorder := NewRecorder(db, 0)
	usageID, err := recorder.RecordSync(ctx, UsageRecord{
		UserID:    user.ID,
		UserEmail: user.Email,
		AccountID: account.ID,
		GroupID:   group.ID,
		Platform:  "openai",
		Model:     "gpt-5",
	})
	if err != nil {
		t.Fatalf("RecordSync returned error: %v", err)
	}

	log, err := db.UsageLog.Get(ctx, usageID)
	if err != nil {
		t.Fatalf("get usage log: %v", err)
	}
	if log.UserIDSnapshot != user.ID || log.UserEmailSnapshot != user.Email {
		t.Fatalf("usage snapshot = (%d, %q), want (%d, %q)", log.UserIDSnapshot, log.UserEmailSnapshot, user.ID, user.Email)
	}
	if log.BillingEventID == "" {
		t.Fatalf("billing_event_id should be generated")
	}
	if log.RateMultiplier != 1 || log.AccountRateMultiplier != 1 {
		t.Fatalf("rate snapshots = (%v, %v), want defaults (1, 1)", log.RateMultiplier, log.AccountRateMultiplier)
	}
}

func TestBatchInsertIsIdempotentByBillingEventID(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "billing_recorder_idempotent", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createBillingTestUser(t, ctx, db, "billing-idempotent@example.com")
	if err := db.User.UpdateOneID(user.ID).SetBalance(10).Exec(ctx); err != nil {
		t.Fatalf("set user balance: %v", err)
	}
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("acc").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("key").
		SetKeyHash("hash-idempotent").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	recorder := NewRecorder(db, 0)
	record := UsageRecord{
		BillingEventID: "bill_test_idempotent",
		UserID:         user.ID,
		UserEmail:      user.Email,
		APIKeyID:       key.ID,
		AccountID:      account.ID,
		GroupID:        group.ID,
		Platform:       "openai",
		Model:          "gpt-5",
		ActualCost:     1.25,
		BilledCost:     2.50,
		AccountCost:    0.75,
	}
	if err := recorder.batchInsert(ctx, []UsageRecord{record}); err != nil {
		t.Fatalf("first batchInsert: %v", err)
	}
	if err := recorder.batchInsert(ctx, []UsageRecord{record}); err != nil {
		t.Fatalf("duplicate batchInsert: %v", err)
	}

	count, err := db.UsageLog.Query().
		Where(usagelog.BillingEventIDEQ(record.BillingEventID)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count usage logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("usage log count = %d, want 1", count)
	}
	userAfter, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if userAfter.Balance != 8.75 {
		t.Fatalf("user balance = %.2f, want 8.75", userAfter.Balance)
	}
	keyAfter, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatalf("get api key: %v", err)
	}
	if keyAfter.UsedQuota != 2.50 || keyAfter.UsedQuotaActual != 1.25 {
		t.Fatalf("api key usage = (%.2f, %.2f), want (2.50, 1.25)", keyAfter.UsedQuota, keyAfter.UsedQuotaActual)
	}
}

func createBillingTestUser(t *testing.T, ctx context.Context, db *ent.Client, email string) *ent.User {
	t.Helper()
	user, err := db.User.Create().
		SetEmail(email).
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}
