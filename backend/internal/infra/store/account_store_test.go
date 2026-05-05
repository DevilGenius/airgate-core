package store

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/enttest"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	"github.com/DouDOU-start/airgate-core/internal/app/account"
)

func TestAccountStoreKeywordSearchMatchesOAuthEmail(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	if _, err := db.Account.Create().
		SetName("Claude Key").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"email": "claude@example.com", "access_token": "token"}).
		Save(ctx); err != nil {
		t.Fatalf("create oauth account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("Other Key").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-test"}).
		Save(ctx); err != nil {
		t.Fatalf("create api key account: %v", err)
	}

	store := NewAccountStore(db)
	items, total, err := store.List(ctx, account.ListFilter{Page: 1, PageSize: 20, Keyword: "claude@"})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(items) != 1 || items[0].Name != "Claude Key" {
		t.Fatalf("items = %+v", items)
	}
}

func enttestOpen(t *testing.T) *ent.Client {
	t.Helper()
	return enttest.Open(t, "sqlite3", "file:account_store?mode=memory&cache=shared&_fk=1", enttest.WithMigrateOptions(migrate.WithGlobalUniqueID(false)))
}
