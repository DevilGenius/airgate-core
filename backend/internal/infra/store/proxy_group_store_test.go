package store

import (
	"errors"
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
)

func TestProxyGroupAllocatesStableSlots(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := t.Context()

	proxyStore := NewProxyStore(db)
	group, err := proxyStore.Create(ctx, appproxy.CreateInput{
		Name: "pool", Mode: appproxy.ModeGroup, Protocol: "http",
		Address: "127.0.0.1", Port: 8080, Password: "secret", SlotStart: 0, SlotEnd: 1,
	})
	if err != nil {
		t.Fatalf("create proxy group: %v", err)
	}
	proxyID := int64(group.ID)
	accountStore := NewAccountStore(db)
	create := func(name string) (appaccount.Account, error) {
		return accountStore.Create(ctx, appaccount.CreateInput{
			Name: name, Platform: "openai", Type: "apikey",
			Credentials: map[string]string{"api_key": name}, ProxyID: &proxyID,
		})
	}

	first, err := create("first")
	if err != nil || first.ProxySlot == nil || *first.ProxySlot != 0 || first.Proxy == nil || first.Proxy.Username != "0000" {
		t.Fatalf("first account = %+v, err=%v", first, err)
	}
	second, err := create("second")
	if err != nil || second.ProxySlot == nil || *second.ProxySlot != 1 || second.Proxy == nil || second.Proxy.Username != "0001" {
		t.Fatalf("second account = %+v, err=%v", second, err)
	}
	third, err := create("full")
	if err != nil || third.ProxySlot == nil || *third.ProxySlot < 0 || *third.ProxySlot > 1 {
		t.Fatalf("full group fallback = %+v, err=%v", third.ProxySlot, err)
	}

	updated, err := accountStore.Update(ctx, first.ID, appaccount.UpdateInput{ProxyID: &proxyID, HasProxyID: true})
	if err != nil || updated.ProxySlot == nil || *updated.ProxySlot != 0 {
		t.Fatalf("stable slot after update = %+v, err=%v", updated.ProxySlot, err)
	}
	listed, _, err := proxyStore.List(ctx, appproxy.ListFilter{Page: 1, PageSize: 20})
	if err != nil || len(listed) != 1 || listed[0].AssignedSlots != 2 {
		t.Fatalf("proxy assigned slots = %+v, err=%v", listed, err)
	}

	single := appproxy.ModeSingle
	if _, err := proxyStore.Update(ctx, group.ID, appproxy.UpdateInput{Mode: &single}); !errors.Is(err, appproxy.ErrProxyGroupHasAccounts) {
		t.Fatalf("group-to-single error = %v", err)
	}
	zero := 0
	if _, err := proxyStore.Update(ctx, group.ID, appproxy.UpdateInput{SlotEnd: &zero}); !errors.Is(err, appproxy.ErrProxySlotRangeInUse) {
		t.Fatalf("range shrink error = %v", err)
	}

	if err := proxyStore.Delete(ctx, group.ID); err != nil {
		t.Fatalf("delete proxy group: %v", err)
	}
	for _, accountID := range []int{first.ID, second.ID, third.ID} {
		item, err := db.Account.Get(ctx, accountID)
		if err != nil || item.ProxySlot != nil {
			t.Fatalf("account %d slot after proxy delete = %+v, err=%v", accountID, item.ProxySlot, err)
		}
		hasProxy, err := item.QueryProxy().Exist(ctx)
		if err != nil || hasProxy {
			t.Fatalf("account %d proxy after delete = %v, err=%v", accountID, hasProxy, err)
		}
	}
}

func TestSingleProxyKeepsConfiguredUsernameWithoutSlot(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := t.Context()
	proxy, err := NewProxyStore(db).Create(ctx, appproxy.CreateInput{
		Name: "single", Protocol: "http", Address: "127.0.0.1", Port: 8080,
		Username: "alice", Password: "secret",
	})
	if err != nil {
		t.Fatalf("create single proxy: %v", err)
	}
	proxyID := int64(proxy.ID)
	account, err := NewAccountStore(db).Create(ctx, appaccount.CreateInput{
		Name: "single-account", Platform: "openai", Type: "apikey",
		Credentials: map[string]string{"api_key": "key"}, ProxyID: &proxyID,
	})
	if err != nil || account.ProxySlot != nil || account.Proxy == nil || account.Proxy.Username != "alice" {
		t.Fatalf("single proxy account = %+v, err=%v", account, err)
	}
}

func TestProxyGroupCustomAndRandomAssignment(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	ctx := t.Context()
	proxy, err := NewProxyStore(db).Create(ctx, appproxy.CreateInput{
		Name: "assignments", Mode: appproxy.ModeGroup, Protocol: "http",
		Address: "127.0.0.1", Port: 8080, SlotStart: 0x10, SlotEnd: 0x12,
	})
	if err != nil {
		t.Fatalf("create proxy group: %v", err)
	}
	store := NewAccountStore(db)
	createUnbound := func(name string) appaccount.Account {
		item, err := store.Create(ctx, appaccount.CreateInput{
			Name: name, Platform: "openai", Type: "apikey",
			Credentials: map[string]string{"api_key": name},
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return item
	}
	first := createUnbound("custom")
	second := createUnbound("custom-reuse")
	third := createUnbound("random-first")
	fourth := createUnbound("random-last-unused")
	fifth := createUnbound("random-after-full")
	proxyID := int64(proxy.ID)
	customSlot := 0x11
	custom, err := store.Update(ctx, first.ID, appaccount.UpdateInput{
		ProxyID: &proxyID, HasProxyID: true,
		ProxyAssignment: appproxy.AssignmentCustom, ProxySlot: &customSlot,
	})
	if err != nil || custom.ProxySlot == nil || *custom.ProxySlot != customSlot || custom.Proxy.Username != "0011" {
		t.Fatalf("custom assignment = %+v, err=%v", custom, err)
	}
	customReuse, err := store.Update(ctx, second.ID, appaccount.UpdateInput{
		ProxyID: &proxyID, HasProxyID: true,
		ProxyAssignment: appproxy.AssignmentCustom, ProxySlot: &customSlot,
	})
	if err != nil || customReuse.ProxySlot == nil || *customReuse.ProxySlot != customSlot {
		t.Fatalf("custom slot reuse = %+v, err=%v", customReuse.ProxySlot, err)
	}
	randomAccount, err := store.Update(ctx, third.ID, appaccount.UpdateInput{
		ProxyID: &proxyID, HasProxyID: true, ProxyAssignment: appproxy.AssignmentRandom,
	})
	if err != nil || randomAccount.ProxySlot == nil || *randomAccount.ProxySlot == customSlot {
		t.Fatalf("random assignment = %+v, err=%v", randomAccount.ProxySlot, err)
	}
	lastUnused, err := store.Update(ctx, fourth.ID, appaccount.UpdateInput{
		ProxyID: &proxyID, HasProxyID: true, ProxyAssignment: appproxy.AssignmentRandom,
	})
	if err != nil || lastUnused.ProxySlot == nil || *lastUnused.ProxySlot == customSlot || *lastUnused.ProxySlot == *randomAccount.ProxySlot {
		t.Fatalf("last unused assignment = %+v, err=%v", lastUnused.ProxySlot, err)
	}
	afterFull, err := store.Update(ctx, fifth.ID, appaccount.UpdateInput{
		ProxyID: &proxyID, HasProxyID: true, ProxyAssignment: appproxy.AssignmentRandom,
	})
	if err != nil || afterFull.ProxySlot == nil || *afterFull.ProxySlot < 0x10 || *afterFull.ProxySlot > 0x12 {
		t.Fatalf("random reuse after full = %+v, err=%v", afterFull.ProxySlot, err)
	}
	assigned := *afterFull.ProxySlot
	stable, err := store.Update(ctx, fifth.ID, appaccount.UpdateInput{ProxyID: &proxyID, HasProxyID: true})
	if err != nil || stable.ProxySlot == nil || *stable.ProxySlot != assigned {
		t.Fatalf("stable random assignment = %+v, err=%v", stable.ProxySlot, err)
	}
}
