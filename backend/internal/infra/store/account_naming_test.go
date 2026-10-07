package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestAccountNamingScopesRollbackAndManualNames(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	s := NewAccountStore(db)
	ctx := t.Context()
	create := func(input appaccount.CreateInput, want string) appaccount.Account {
		t.Helper()
		got, err := s.Create(ctx, input)
		if err != nil || got.Name != want {
			t.Fatalf("Create() name=%q err=%v, want %q", got.Name, err, want)
		}
		return got
	}
	input := appaccount.CreateInput{AutoName: true, Platform: " OpenAI ", Type: " OAuth ", Credentials: map[string]string{"access_token": "one"}}
	first := create(input, namingToday()+"-Unknown-1")
	manual := input
	manual.Name = "Primary"
	manual.AutoName = false
	create(manual, "Primary")
	failed := input
	failed.GroupIDs = []int64{999999}
	if _, err := s.Create(ctx, failed); err == nil {
		t.Fatal("expected missing group to roll back account and counter")
	}
	create(input, namingToday()+"-Unknown-2")
	if err := s.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	create(input, namingToday()+"-Unknown-3")
	create(appaccount.CreateInput{AutoName: true, Platform: "openai", Credentials: map[string]string{"api_key": "key"}}, namingToday()+"-Unknown-4")
	create(appaccount.CreateInput{AutoName: true, Platform: "claude", Type: "oauth"}, namingToday()+"-Unknown-1")
}

func TestAccountNamingReauthorizationAndRestore(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	s := NewAccountStore(db)
	email := "naming@example.com"
	input := appaccount.CreateInput{AutoName: true, Platform: "openai", Type: "oauth", Email: &email, Credentials: map[string]string{"access_token": "one"}}
	first, err := s.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Credentials["access_token"] = "two"
	again, err := s.Create(t.Context(), input)
	if err != nil || again.ID != first.ID || again.Name != first.Name {
		t.Fatalf("reauthorization changed identity: %+v, %v", again, err)
	}
	if err := s.Delete(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Create(t.Context(), input)
	if err != nil || restored.ID != first.ID || restored.Name != first.Name {
		t.Fatalf("restore changed identity: %+v, %v", restored, err)
	}
	next, err := s.Create(t.Context(), appaccount.CreateInput{AutoName: true, Platform: "openai", Type: "oauth"})
	if err != nil || next.Name != namingToday()+"-Unknown-2" {
		t.Fatalf("reauthorization or restore consumed an index: %q, %v", next.Name, err)
	}
}

func TestAccountNamingPersistsAcrossReopen(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "naming.db"))
	input := appaccount.CreateInput{AutoName: true, Platform: "openai", Type: "oauth"}
	db := testdb.OpenEnt(t, dsn)
	if _, err := NewAccountStore(db).Create(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = testdb.OpenEnt(t, dsn)
	defer func() { _ = db.Close() }()
	got, err := NewAccountStore(db).Create(t.Context(), input)
	if err != nil || got.Name != namingToday()+"-Unknown-2" {
		t.Fatalf("counter lost after reopening: %q, %v", got.Name, err)
	}
}

func TestAccountNamingConcurrentCreation(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	s := NewAccountStore(db)
	const count = 16
	var wg sync.WaitGroup
	results := make(chan string, count)
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.Create(t.Context(), appaccount.CreateInput{AutoName: true, Platform: "openai", Type: "oauth"})
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			results <- got.Name
		}()
	}
	wg.Wait()
	close(results)
	names := map[string]bool{}
	for name := range results {
		if names[name] {
			t.Fatalf("duplicate name: %s", name)
		}
		names[name] = true
	}
	for index := 1; index <= count; index++ {
		if !names[fmt.Sprintf("%s-Unknown-%d", namingToday(), index)] {
			t.Fatalf("missing index %d", index)
		}
	}
}

func namingToday() string {
	return time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("0102")
}

func TestAccountNamingDatePlanAndTimezone(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	tx, err := db.Tx(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	boundary := time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		plan string
		at   time.Time
		want string
	}{
		{"plus", boundary.Add(-time.Second), "1007-Plus-1"},
		{"PLUS", boundary.Add(-time.Second), "1007-Plus-2"},
		{"pro", boundary.Add(-time.Second), "1007-Pro-1"},
		{"plus", boundary, "1008-Plus-1"},
		{"SELF_SERVE_BUSINESS_PROLITE", boundary, "1008-Prolite-1"},
		{"ChatGPT ProLite", boundary, "1008-Prolite-2"},
		{"prolite", boundary, "1008-Prolite-3"},
		{"", boundary, "1008-Unknown-1"},
		{"   ", boundary, "1008-Unknown-2"},
		{"unrecognized-plan", boundary, "1008-Unknown-3"},
		{"???", boundary, "1008-Unknown-4"},
		{"plus", boundary.AddDate(1, 0, 0), "1008-Plus-1"},
	} {
		got, err := allocateAccountNameAt(t.Context(), tx, appaccount.CreateInput{
			AutoName: true, Platform: "openai", Name: "must-be-overridden",
			Credentials: map[string]string{"plan_type": tc.plan},
		}, tc.at)
		if err != nil || got != tc.want {
			t.Fatalf("plan=%s at=%v: got %q err=%v, want %q", tc.plan, tc.at, got, err, tc.want)
		}
	}
}

func TestExistingAccountKeepsOriginalNameWithoutAllocatingNewPlanIndex(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	s := NewAccountStore(db)
	email := "existing@example.com"
	input := appaccount.CreateInput{Platform: "openai", Type: "oauth", Email: &email,
		Name: "Original manual name", Credentials: map[string]string{"access_token": "one", "plan_type": "plus"}}
	first, err := s.Create(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.AutoName = true
	input.Name = "Do not replace existing name"
	input.Credentials["plan_type"] = "prolite"
	for i := 0; i < 3; i++ {
		if i == 1 {
			if err := s.Delete(t.Context(), first.ID); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.Create(t.Context(), input)
		if err != nil || got.ID != first.ID || got.Name != first.Name {
			t.Fatalf("repeat import %d: name=%q id=%d err=%v", i, got.Name, got.ID, err)
		}
	}
	// No email means this is a genuinely new account for the purposes of the
	// current email identity rule. It must receive the first Prolite index.
	input.Email = nil
	delete(input.Credentials, "email")
	got, err := s.Create(t.Context(), input)
	if err != nil || got.Name != namingToday()+"-Prolite-1" {
		t.Fatalf("existing imports consumed indexes: name=%q err=%v", got.Name, err)
	}
}

func TestOrdinaryImportKeepsNamesAndDoesNotAllocate(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	s := appaccount.NewService(NewAccountStore(db), nil, nil, nil)
	input := appaccount.CreateInput{Platform: "openai", Type: "oauth", Name: "Original Import Name", Credentials: map[string]string{"access_token": "token", "plan_type": "plus"}}
	summary := s.ImportConfigured(t.Context(), []appaccount.CreateInput{input})
	if summary.Imported != 1 || summary.Failed != 0 {
		t.Fatalf("ordinary import: %+v", summary)
	}
	item, err := db.Account.Get(t.Context(), summary.SuccessIDs[0])
	if err != nil || item.Name != input.Name {
		t.Fatalf("ordinary import name changed: %v, %v", item, err)
	}
	input.Name = ""
	if summary := s.Import(t.Context(), []appaccount.CreateInput{input}); summary.Failed != 1 {
		t.Fatalf("ordinary unnamed import should fail: %+v", summary)
	}
	input.AutoName = true
	input.Name = "Ignored API Name"
	got, err := NewAccountStore(db).Create(t.Context(), input)
	if err != nil || got.Name != namingToday()+"-Plus-1" {
		t.Fatalf("ordinary import consumed a counter or API name was preserved: %q, %v", got.Name, err)
	}
}
