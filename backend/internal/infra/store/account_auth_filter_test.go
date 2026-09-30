package store

import (
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"testing"
)

func TestAccountAuthFilterIntersectsPlanForListAndExport(t *testing.T) {
	db := enttestOpen(t)
	defer db.Close()
	for _, item := range []struct{ name, auth, plan string }{
		{"plus-oauth", "oauth", "plus"},
		{"free-oauth", "oauth", "free"},
		{"key", "apikey", ""},
	} {
		db.Account.Create().SetName(item.name).SetPlatform("openai").SetType(item.auth).
			SetCredentials(map[string]string{"plan_type": item.plan}).SaveX(t.Context())
	}
	store := NewAccountStore(db)
	for _, test := range []struct {
		auth string
		plan bool
		want int
	}{
		{"oauth", true, 1}, {"oauth,apikey", true, 1}, {"apikey", true, 0},
		{"", true, 1}, {"oauth,apikey", false, 3}, {"apikey", false, 1},
	} {
		filter := appaccount.ListFilter{Page: 1, PageSize: 20, AuthType: test.auth}
		if test.plan {
			filter.Credentials = []appaccount.CredentialStringFilter{{
				Platform: "openai", AccountType: "oauth", Key: "plan_type", Values: []string{"plus"},
			}}
		}
		rows, total, err := store.List(t.Context(), filter)
		if err != nil || len(rows) != test.want || total != int64(test.want) {
			t.Fatalf("list auth=%q plan=%v: rows=%d total=%d err=%v", test.auth, test.plan, len(rows), total, err)
		}
		exported, err := store.ListAll(t.Context(), filter)
		if err != nil || len(exported) != test.want {
			t.Fatalf("export auth=%q plan=%v: rows=%d err=%v", test.auth, test.plan, len(exported), err)
		}
	}
}
