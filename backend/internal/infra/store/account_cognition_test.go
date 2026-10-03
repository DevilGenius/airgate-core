package store

import (
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
)

func TestAccountCognitionFlagPreservesExtra(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	row, err := db.Account.Create().SetName("cognition").SetPlatform("openai").SetType("oauth").SetCredentials(map[string]string{}).SetExtra(map[string]any{"other": "keep"}).Save(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	store := NewAccountStore(db)
	for _, flag := range []bool{true, false} {
		got, err := store.Update(t.Context(), row.ID, appaccount.UpdateInput{CognitionDegraded: &flag})
		if err != nil {
			t.Fatal(err)
		}
		if got.Extra["other"] != "keep" || got.Extra["cognition_degraded"] != flag {
			t.Fatalf("extra=%v", got.Extra)
		}
		cleared, err := store.Update(t.Context(), row.ID, appaccount.UpdateInput{ClearCognitionTest: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := cleared.Extra["cognition_degraded"]; exists || cleared.Extra["other"] != "keep" {
			t.Fatalf("clear result=%v", cleared.Extra)
		}
	}
}
