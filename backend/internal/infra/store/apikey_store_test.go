package store

import (
	"context"
	"testing"
	"time"

	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
)

// TestAPIKeyStoreListAdminSearchScope 验证 search_scope 控制是否按用户邮箱模糊匹配。
//
// 业务背景：管理员通用搜索想同时支持 name/key_hint/user_email；但
// "Usage 页面通过 API Key 选择器搜索"这一场景里，邮箱模糊匹配会带回大量
// 同邮箱所属的其它 Key，造成噪音。前端在该场景下传 search_scope=api_key
// 让 store 跳过邮箱谓词。
func TestAPIKeyStoreListAdminSearchScope(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()

	user := createTestUser(t, db, "scope-target@example.com")
	if _, err := db.APIKey.Create().
		SetName("billing-runner").
		SetKeyHint("sk-bill-001").
		SetKeyHash("hash-1").
		SetUserID(user.ID).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	store := NewAPIKeyStore(db)

	t.Run("default scope matches user email", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "scope-target",
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 1 {
			t.Fatalf("default scope total = %d, want 1 (email predicate must apply)", total)
		}
	})

	t.Run("api_key scope skips user email predicate", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "scope-target",
			SearchScope: appapikey.SearchScopeAPIKey,
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 0 {
			t.Fatalf("api_key scope total = %d, want 0 (email predicate must be skipped)", total)
		}
	})

	t.Run("api_key scope still matches name", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "billing",
			SearchScope: appapikey.SearchScopeAPIKey,
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 1 {
			t.Fatalf("api_key scope name match total = %d, want 1", total)
		}
	})

	t.Run("api_key scope still matches key_hint", func(t *testing.T) {
		_, total, err := store.ListAdmin(ctx, appapikey.ListFilter{
			Page: 1, PageSize: 20, Keyword: "sk-bill",
			SearchScope: appapikey.SearchScopeAPIKey,
		})
		if err != nil {
			t.Fatalf("ListAdmin returned error: %v", err)
		}
		if total != 1 {
			t.Fatalf("api_key scope key_hint match total = %d, want 1", total)
		}
	})
}

func TestAPIKeyStoreKeyUsageReturnsSalesAndActualCostSums(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	ctx := context.Background()

	user := createTestUser(t, db, "key-usage@example.com")
	key, err := db.APIKey.Create().
		SetName("usage-key").
		SetKeyHash("hash-usage-key").
		SetUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	otherKey, err := db.APIKey.Create().
		SetName("other-key").
		SetKeyHash("hash-other-key").
		SetUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create other api key: %v", err)
	}

	todayStart := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	createUsageLog := func(apiKeyID int, createdAt time.Time, actualCost, billedCost float64) {
		t.Helper()
		if _, err := db.UsageLog.Create().
			SetPlatform("openai").
			SetModel("gpt-test").
			SetUserID(user.ID).
			SetUserIDSnapshot(user.ID).
			SetUserEmailSnapshot(user.Email).
			SetAPIKeyID(apiKeyID).
			SetCreatedAt(createdAt).
			SetActualCost(actualCost).
			SetBilledCost(billedCost).
			Save(ctx); err != nil {
			t.Fatalf("create usage log: %v", err)
		}
	}

	createUsageLog(key.ID, todayStart.Add(2*time.Hour), 1.00, 2.50)
	createUsageLog(key.ID, todayStart.AddDate(0, 0, -5), 2.00, 4.00)
	createUsageLog(key.ID, todayStart.AddDate(0, 0, -30), 30.00, 90.00)
	createUsageLog(otherKey.ID, todayStart.Add(3*time.Hour), 3.00, 7.00)

	usage, err := NewAPIKeyStore(db).KeyUsage(ctx, []int{key.ID, otherKey.ID}, todayStart)
	if err != nil {
		t.Fatalf("KeyUsage returned error: %v", err)
	}
	if usage[key.ID].TodaySalesCost != 2.50 || usage[key.ID].TodayActualCost != 1.00 {
		t.Fatalf("today usage = %+v, want sales/actual 2.50/1.00", usage[key.ID])
	}
	if usage[key.ID].ThirtyDaySalesCost != 6.50 || usage[key.ID].ThirtyDayActualCost != 3.00 {
		t.Fatalf("thirty day usage = %+v, want sales/actual 6.50/3.00", usage[key.ID])
	}
	if usage[otherKey.ID].TodaySalesCost != 7.00 || usage[otherKey.ID].TodayActualCost != 3.00 ||
		usage[otherKey.ID].ThirtyDaySalesCost != 7.00 || usage[otherKey.ID].ThirtyDayActualCost != 3.00 {
		t.Fatalf("other key usage = %+v, want sales/actual 7.00/3.00 for both windows", usage[otherKey.ID])
	}
}
