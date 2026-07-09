package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
)

func TestAccountStoreCRUDListsAndAggregates(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().
		SetName("Account Group").
		SetPlatform("openai").
		SetRateMultiplier(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	otherGroup, err := db.Group.Create().
		SetName("Other Account Group").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}
	proxy, err := db.Proxy.Create().
		SetName("Account Proxy").
		SetProtocol("http").
		SetAddress("127.0.0.1").
		SetPort(8080).
		SetUsername("proxy-user").
		SetPassword("proxy-pass").
		Save(ctx)
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	otherProxy, err := db.Proxy.Create().
		SetName("Other Account Proxy").
		SetProtocol("socks5").
		SetAddress("127.0.0.2").
		SetPort(1080).
		Save(ctx)
	if err != nil {
		t.Fatalf("create other proxy: %v", err)
	}

	store := NewAccountStore(db)
	proxyID := int64(proxy.ID)
	rate := 1.75
	email := "account@example.com"
	created, err := store.Create(ctx, appaccount.CreateInput{
		Name:           "Primary Account",
		Email:          &email,
		Platform:       "openai",
		Type:           "oauth",
		Credentials:    map[string]string{"access_token": "token"},
		ModelPolicy:    modelpolicy.Policy{Allow: []string{"gpt-*"}},
		Priority:       80,
		MaxConcurrency: 12,
		ProxyID:        &proxyID,
		RateMultiplier: &rate,
		GroupIDs:       []int64{int64(group.ID)},
		UpstreamIsPool: true,
		Extra:          map[string]any{"max_rpm": float64(60)},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.RateMultiplier != rate || created.Proxy == nil || created.Proxy.ID != proxy.ID ||
		len(created.GroupIDs) != 1 || created.GroupIDs[0] != int64(group.ID) ||
		created.Email == nil || *created.Email != email || created.Credentials["email"] != email || created.ModelPolicy.Allow[0] != "gpt-*" ||
		!created.UpstreamIsPool || created.Extra["max_rpm"] != float64(60) {
		t.Fatalf("created account = %+v", created)
	}
	*created.Email = "mutated@example.com"
	created.ModelPolicy.Allow[0] = "mutated"
	created.Extra["max_rpm"] = float64(0)
	found, err := store.FindByID(ctx, created.ID, appaccount.LoadOptions{WithGroups: true, WithProxy: true})
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if found.Email == nil || *found.Email != email || found.ModelPolicy.Allow[0] != "gpt-*" ||
		found.Extra["max_rpm"] != float64(60) {
		t.Fatalf("account clone leaked mutation: %+v", found)
	}

	defaulted, err := store.Create(ctx, appaccount.CreateInput{
		Name: "Primary Account", Platform: "openai", Type: "apikey",
		Credentials: map[string]string{"api_key": "sk-ungrouped"},
	})
	if err != nil {
		t.Fatalf("Create ungrouped returned error: %v", err)
	}
	if defaulted.RateMultiplier != 1 || defaulted.Email != nil {
		t.Fatalf("default rate multiplier = %v, want 1", defaulted.RateMultiplier)
	}

	lastUsedAt := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	stateUntil := lastUsedAt.Add(time.Hour)
	if _, err := db.Account.UpdateOneID(created.ID).
		SetLastUsedAt(lastUsedAt).
		SetStateUntil(stateUntil).
		Save(ctx); err != nil {
		t.Fatalf("set account pointer fields: %v", err)
	}
	found, err = store.FindByID(ctx, created.ID, appaccount.LoadOptions{})
	if err != nil {
		t.Fatalf("FindByID without edges returned error: %v", err)
	}
	if found.LastUsedAt == nil || !found.LastUsedAt.Equal(lastUsedAt) ||
		found.StateUntil == nil || !found.StateUntil.Equal(stateUntil) {
		t.Fatalf("pointer fields = last %v until %v", found.LastUsedAt, found.StateUntil)
	}
	if _, err := store.FindByID(ctx, 999999, appaccount.LoadOptions{}); !errors.Is(err, appaccount.ErrAccountNotFound) {
		t.Fatalf("FindByID missing error = %v, want ErrAccountNotFound", err)
	}

	groupID := group.ID
	proxyFilterID := proxy.ID
	list, total, err := store.List(ctx, appaccount.ListFilter{
		Page: 1, PageSize: 20, GroupID: &groupID, ProxyID: &proxyFilterID, State: "active", IDs: []int{created.ID},
	})
	if err != nil {
		t.Fatalf("List grouped/proxied returned error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("group/proxy list = total %d list %+v, want created account", total, list)
	}
	ungrouped, total, err := store.List(ctx, appaccount.ListFilter{Page: 1, PageSize: 20, Ungrouped: true})
	if err != nil {
		t.Fatalf("List ungrouped returned error: %v", err)
	}
	if total != 1 || len(ungrouped) != 1 || ungrouped[0].ID != defaulted.ID {
		t.Fatalf("ungrouped list = total %d list %+v, want defaulted account", total, ungrouped)
	}
	platformAccounts, err := store.ListByPlatform(ctx, "openai")
	if err != nil {
		t.Fatalf("ListByPlatform returned error: %v", err)
	}
	if len(platformAccounts) != 2 {
		t.Fatalf("ListByPlatform len = %d, want 2", len(platformAccounts))
	}
	clearedEmail, err := store.Update(ctx, created.ID, appaccount.UpdateInput{HasEmail: true})
	if err != nil || clearedEmail.Email != nil {
		t.Fatalf("clear account email = %+v, err %v", clearedEmail.Email, err)
	}
	if _, ok := clearedEmail.Credentials["email"]; ok {
		t.Fatalf("cleared account credentials still contain email: %+v", clearedEmail.Credentials)
	}
	restoredEmail, err := store.Update(ctx, created.ID, appaccount.UpdateInput{Email: &email, HasEmail: true})
	if err != nil || restoredEmail.Email == nil || *restoredEmail.Email != email || restoredEmail.Credentials["email"] != email {
		t.Fatalf("restore account email = %+v, err %v", restoredEmail.Email, err)
	}
	if _, err := store.Update(ctx, defaulted.ID, appaccount.UpdateInput{Email: &email, HasEmail: true}); !errors.Is(err, appaccount.ErrAccountEmailExists) {
		t.Fatalf("duplicate update email error = %v, want ErrAccountEmailExists", err)
	}

	newName := "Updated Account"
	newType := "apikey"
	newCredentials := map[string]string{"api_key": "sk-updated"}
	newPolicy := modelpolicy.Policy{Deny: []string{"gpt-4*"}}
	newState := "disabled"
	newPriority := 7
	newMaxConcurrency := 2
	newRate := 2.5
	newPool := false
	otherProxyID := int64(otherProxy.ID)
	updated, err := store.Update(ctx, created.ID, appaccount.UpdateInput{
		Name: &newName, Type: &newType, Credentials: newCredentials, ModelPolicy: &newPolicy,
		State: &newState, Priority: &newPriority, MaxConcurrency: &newMaxConcurrency,
		RateMultiplier: &newRate, UpstreamIsPool: &newPool,
		GroupIDs: []int64{int64(otherGroup.ID)}, HasGroupIDs: true,
		ProxyID: &otherProxyID, HasProxyID: true,
		Extra: map[string]any{"max_sessions": float64(3)}, HasExtra: true,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Name != newName || updated.Type != newType || updated.State != newState ||
		updated.Priority != newPriority || updated.MaxConcurrency != newMaxConcurrency ||
		updated.RateMultiplier != newRate || updated.UpstreamIsPool ||
		len(updated.GroupIDs) != 1 || updated.GroupIDs[0] != int64(otherGroup.ID) ||
		updated.Proxy == nil || updated.Proxy.ID != otherProxy.ID ||
		updated.Extra["max_sessions"] != float64(3) {
		t.Fatalf("updated account = %+v", updated)
	}
	cleared, err := store.Update(ctx, created.ID, appaccount.UpdateInput{
		HasGroupIDs: true,
		HasProxyID:  true,
		HasExtra:    true,
	})
	if err != nil {
		t.Fatalf("Update clear returned error: %v", err)
	}
	if len(cleared.GroupIDs) != 0 || cleared.Proxy != nil || len(cleared.Extra) != 0 {
		t.Fatalf("cleared account = %+v", cleared)
	}
	if _, err := store.Update(ctx, 999999, appaccount.UpdateInput{Name: &newName}); !errors.Is(err, appaccount.ErrAccountNotFound) {
		t.Fatalf("Update missing error = %v, want ErrAccountNotFound", err)
	}

	saved, err := store.Update(ctx, created.ID, appaccount.UpdateInput{Credentials: map[string]string{"api_key": "sk-saved"}})
	if err != nil {
		t.Fatalf("Update credentials returned error: %v", err)
	}
	if saved.Credentials["api_key"] != "sk-saved" || saved.Credentials["email"] != email || saved.Email == nil || *saved.Email != email {
		t.Fatalf("saved credentials = %+v", saved.Credentials)
	}

	user := createTestUser(t, db, "account-usage@example.com")
	key, err := db.APIKey.Create().
		SetName("account-usage-key").
		SetKeyHash("hash-account-usage").
		SetUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create usage api key: %v", err)
	}
	todayStart := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	createAccountUsageLog(t, db, "account_usage_chat", user.ID, key.ID, created.ID, group.ID, "gpt-5", todayStart.Add(time.Hour), 10, 20, 3, 7, 1.25, 1.50, 2.00)
	createAccountUsageLog(t, db, "account_usage_image_today", user.ID, key.ID, created.ID, group.ID, "gpt-image-1", todayStart.Add(2*time.Hour), 5, 15, 0, 0, 2.00, 2.50, 3.00)
	createAccountUsageLog(t, db, "account_usage_image_old", user.ID, key.ID, created.ID, group.ID, "gpt-image-1", todayStart.AddDate(0, 0, -2), 1, 2, 0, 0, 3.00, 3.50, 4.00)

	logs, err := store.FindUsageLogs(ctx, created.ID, todayStart, todayStart.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("FindUsageLogs returned error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("FindUsageLogs len = %d, want 2: %+v", len(logs), logs)
	}
	window, err := store.BatchWindowStats(ctx, []int{created.ID, defaulted.ID}, todayStart)
	if err != nil {
		t.Fatalf("BatchWindowStats returned error: %v", err)
	}
	if got := window[created.ID]; got.Requests != 2 || got.Tokens != 60 || got.AccountCost != 5.00 || got.UserCost != 4.00 {
		t.Fatalf("window stats = %+v", got)
	}
	emptyWindow, err := store.BatchWindowStats(ctx, nil, todayStart)
	if err != nil || len(emptyWindow) != 0 {
		t.Fatalf("empty BatchWindowStats = %+v err %v, want empty nil", emptyWindow, err)
	}
	imageStats, err := store.BatchImageStats(ctx, []int{created.ID, defaulted.ID}, todayStart)
	if err != nil {
		t.Fatalf("BatchImageStats returned error: %v", err)
	}
	if got := imageStats[created.ID]; got.TodayCount != 1 || got.TotalCount != 2 {
		t.Fatalf("image stats = %+v, want today 1 total 2", got)
	}
	emptyImages, err := store.BatchImageStats(ctx, nil, todayStart)
	if err != nil || len(emptyImages) != 0 {
		t.Fatalf("empty BatchImageStats = %+v err %v, want empty nil", emptyImages, err)
	}

	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	rawDeleted, err := db.Account.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("load soft-deleted account: %v", err)
	}
	if rawDeleted.DeletedAt == nil || rawDeleted.State != entaccount.StateDisabled ||
		rawDeleted.Email == nil || *rawDeleted.Email != email || rawDeleted.Credentials["api_key"] != "sk-saved" || rawDeleted.Credentials["email"] != email {
		t.Fatalf("soft-deleted account = %+v", rawDeleted)
	}
	if count, err := rawDeleted.QueryUsageLogs().Count(ctx); err != nil || count != 3 {
		t.Fatalf("soft-deleted account usage logs = %d, err %v, want 3", count, err)
	}
	if _, err := store.FindByID(ctx, created.ID, appaccount.LoadOptions{}); !errors.Is(err, appaccount.ErrAccountNotFound) {
		t.Fatalf("FindByID soft-deleted error = %v, want ErrAccountNotFound", err)
	}
	historical, err := store.FindByID(ctx, created.ID, appaccount.LoadOptions{IncludeDeleted: true})
	if err != nil || historical.DeletedAt == nil || historical.Email == nil || *historical.Email != email || historical.Credentials["api_key"] != "sk-saved" {
		t.Fatalf("FindByID IncludeDeleted = %+v, err %v", historical, err)
	}
	visible, total, err := store.List(ctx, appaccount.ListFilter{Page: 1, PageSize: 20, IDs: []int{created.ID}})
	if err != nil || total != 0 || len(visible) != 0 {
		t.Fatalf("List soft-deleted = total %d items %+v err %v", total, visible, err)
	}
	if _, err := store.Update(ctx, created.ID, appaccount.UpdateInput{Name: &newName}); !errors.Is(err, appaccount.ErrAccountNotFound) {
		t.Fatalf("Update soft-deleted error = %v, want ErrAccountNotFound", err)
	}
	accountID := int64(created.ID)
	usageRecords, _, _, err := NewUsageStore(db).ListAdmin(ctx, appusage.ListFilter{Page: 1, PageSize: 10, AccountID: &accountID})
	if err != nil || len(usageRecords) != 3 {
		t.Fatalf("usage records after soft delete = %+v, err %v", usageRecords, err)
	}
	for _, record := range usageRecords {
		if !record.AccountDeleted || record.AccountID != accountID || record.AccountName != newName || record.AccountEmail != "account@example.com" {
			t.Fatalf("usage record soft-deleted account = %+v", record)
		}
	}
	restored, err := store.Create(ctx, appaccount.CreateInput{
		Name:           "Restored Account",
		Platform:       "openai",
		Type:           "oauth",
		Credentials:    map[string]string{"access_token": "restored-token", "email": " Account@Example.COM "},
		Priority:       90,
		MaxConcurrency: 8,
	})
	if err != nil {
		t.Fatalf("restore soft-deleted account: %v", err)
	}
	if restored.ID != created.ID || restored.DeletedAt != nil || restored.State != entaccount.StateActive.String() ||
		restored.Email == nil || *restored.Email != email || restored.Credentials["access_token"] != "restored-token" ||
		restored.Credentials["email"] != email ||
		len(restored.GroupIDs) != 0 || restored.Proxy != nil || len(restored.Extra) != 0 {
		t.Fatalf("restored account = %+v", restored)
	}
	if _, err := store.Create(ctx, appaccount.CreateInput{
		Name: "Duplicate Name Allowed", Email: &email, Platform: "openai", Type: "oauth", Credentials: map[string]string{"access_token": "duplicate"},
	}); !errors.Is(err, appaccount.ErrAccountEmailExists) {
		t.Fatalf("active duplicate email error = %v, want ErrAccountEmailExists", err)
	}
	usageRecords, _, _, err = NewUsageStore(db).ListAdmin(ctx, appusage.ListFilter{Page: 1, PageSize: 10, AccountID: &accountID})
	if err != nil || len(usageRecords) != 3 || usageRecords[0].AccountDeleted || usageRecords[0].AccountName != "Restored Account" {
		t.Fatalf("usage records after restore = %+v, err %v", usageRecords, err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete restored account: %v", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, appaccount.ErrAccountNotFound) {
		t.Fatalf("Delete missing error = %v, want ErrAccountNotFound", err)
	}
}

func TestAPIKeyStoreMutationsAccessAndUsage(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().
		SetName("API Key Group").
		SetPlatform("openai").
		SetRateMultiplier(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	exclusive, err := db.Group.Create().
		SetName("Exclusive Group").
		SetPlatform("openai").
		SetIsExclusive(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create exclusive group: %v", err)
	}
	allowedExclusive, err := db.Group.Create().
		SetName("Allowed Exclusive Group").
		SetPlatform("openai").
		SetIsExclusive(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create allowed exclusive group: %v", err)
	}
	user, err := db.User.Create().
		SetEmail("apikey-owner@example.com").
		SetPasswordHash("hash").
		SetGroupRates(map[int64]float64{int64(group.ID): 3.5}).
		AddAllowedGroupIDs(allowedExclusive.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	otherUser := createTestUser(t, db, "apikey-other@example.com")

	store := NewAPIKeyStore(db)
	expiresAt := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	created, err := store.Create(ctx, appapikey.Mutation{
		Name:                  storePtr("Primary Key"),
		KeyHint:               storePtr("sk-live-1234"),
		KeyHash:               storePtr("hash-primary-key"),
		KeyEncrypted:          storePtr("encrypted-key"),
		UserID:                storePtr(user.ID),
		GroupID:               storePtr(group.ID),
		IPWhitelist:           []string{"127.0.0.1"},
		HasIPWhitelist:        true,
		IPBlacklist:           []string{"10.0.0.1"},
		HasIPBlacklist:        true,
		QuotaUSD:              storePtr(100.0),
		SellRate:              storePtr(1.25),
		MaxConcurrency:        storePtr(8),
		BalanceAlertEnabled:   storePtr(true),
		BalanceAlertEmail:     storePtr("alerts@example.com"),
		BalanceAlertThreshold: storePtr(15.0),
		ExpiresAt:             &expiresAt,
		HasExpiresAt:          true,
		Status:                storePtr("active"),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.UserID != user.ID || created.GroupID == nil || *created.GroupID != group.ID ||
		created.GroupRate != 3.5 || created.KeyEncrypted != "encrypted-key" ||
		created.QuotaUSD != 100 || created.SellRate != 1.25 || created.MaxConcurrency != 8 ||
		!created.BalanceAlertEnabled || created.BalanceAlertEmail != "alerts@example.com" ||
		created.ExpiresAt == nil || !created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("created key = %+v", created)
	}
	created.IPWhitelist[0] = "mutated"
	found, err := store.FindOwned(ctx, user.ID, created.ID)
	if err != nil {
		t.Fatalf("FindOwned returned error: %v", err)
	}
	if found.IPWhitelist[0] != "127.0.0.1" {
		t.Fatalf("IPWhitelist clone leaked mutation: %+v", found.IPWhitelist)
	}

	byUser, total, err := store.ListByUser(ctx, user.ID, appapikey.ListFilter{
		Page: 1, PageSize: 20, Keyword: fmt.Sprint(created.ID), SearchScope: appapikey.SearchScopeAPIKey,
	})
	if err != nil {
		t.Fatalf("ListByUser returned error: %v", err)
	}
	if total != 1 || len(byUser) != 1 || byUser[0].ID != created.ID {
		t.Fatalf("ListByUser = total %d list %+v, want created key", total, byUser)
	}
	adminList, total, err := store.ListAdmin(ctx, appapikey.ListFilter{Page: 1, PageSize: 1, Keyword: "Primary"})
	if err != nil {
		t.Fatalf("ListAdmin returned error: %v", err)
	}
	if total != 1 || len(adminList) != 1 {
		t.Fatalf("ListAdmin = total %d list %+v, want one key", total, adminList)
	}

	for _, item := range []struct {
		name  string
		id    int
		exist bool
		allow bool
	}{
		{name: "missing", id: 999999, exist: false, allow: false},
		{name: "public", id: group.ID, exist: true, allow: true},
		{name: "exclusive denied", id: exclusive.ID, exist: true, allow: false},
		{name: "exclusive allowed", id: allowedExclusive.ID, exist: true, allow: true},
	} {
		access, err := store.GetGroupAccess(ctx, user.ID, item.id)
		if err != nil {
			t.Fatalf("GetGroupAccess %s returned error: %v", item.name, err)
		}
		if access.Exists != item.exist || access.Allowed != item.allow {
			t.Fatalf("GetGroupAccess %s = %+v, want exists %v allowed %v", item.name, access, item.exist, item.allow)
		}
	}

	if _, err := db.APIKey.UpdateOneID(created.ID).
		SetUsedQuota(30).
		SetUsedQuotaActual(12).
		SetBalanceAlertNotified(true).
		Save(ctx); err != nil {
		t.Fatalf("seed used quota: %v", err)
	}
	updatedExpiresAt := expiresAt.Add(24 * time.Hour)
	updated, err := store.UpdateOwned(ctx, user.ID, created.ID, appapikey.Mutation{
		Name:                  storePtr("Updated Key"),
		GroupID:               storePtr(allowedExclusive.ID),
		IPWhitelist:           []string{"192.168.1.1"},
		HasIPWhitelist:        true,
		IPBlacklist:           []string{"172.16.0.1"},
		HasIPBlacklist:        true,
		QuotaUSD:              storePtr(150.0),
		SellRate:              storePtr(2.0),
		MaxConcurrency:        storePtr(2),
		BalanceAlertEnabled:   storePtr(true),
		BalanceAlertEmail:     storePtr("new-alerts@example.com"),
		BalanceAlertThreshold: storePtr(25.0),
		ExpiresAt:             &updatedExpiresAt,
		HasExpiresAt:          true,
		Status:                storePtr("disabled"),
	})
	if err != nil {
		t.Fatalf("UpdateOwned returned error: %v", err)
	}
	if updated.Name != "Updated Key" || updated.GroupID == nil || *updated.GroupID != allowedExclusive.ID ||
		updated.IPWhitelist[0] != "192.168.1.1" || updated.IPBlacklist[0] != "172.16.0.1" ||
		updated.QuotaUSD != 150 || updated.SellRate != 2 || updated.MaxConcurrency != 2 ||
		updated.BalanceAlertNotified || updated.Status != "disabled" ||
		updated.ExpiresAt == nil || !updated.ExpiresAt.Equal(updatedExpiresAt) {
		t.Fatalf("updated key = %+v", updated)
	}
	if _, err := store.UpdateOwned(ctx, otherUser.ID, created.ID, appapikey.Mutation{Name: storePtr("bad owner")}); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("UpdateOwned wrong owner error = %v, want ErrKeyNotFound", err)
	}

	adminUpdated, err := store.UpdateAdmin(ctx, created.ID, appapikey.Mutation{
		Name:         storePtr("Admin Updated Key"),
		HasExpiresAt: true,
	})
	if err != nil {
		t.Fatalf("UpdateAdmin returned error: %v", err)
	}
	if adminUpdated.Name != "Admin Updated Key" || adminUpdated.ExpiresAt != nil {
		t.Fatalf("admin updated key = %+v", adminUpdated)
	}
	if _, err := store.UpdateAdmin(ctx, 999999, appapikey.Mutation{Name: storePtr("missing")}); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("UpdateAdmin missing error = %v, want ErrKeyNotFound", err)
	}

	if _, err := db.APIKey.UpdateOneID(created.ID).
		SetUsedQuota(22).
		SetUsedQuotaActual(11).
		SetBalanceAlertNotified(true).
		Save(ctx); err != nil {
		t.Fatalf("seed quota before reset: %v", err)
	}
	reset, err := store.ResetUsageAdmin(ctx, created.ID)
	if err != nil {
		t.Fatalf("ResetUsageAdmin returned error: %v", err)
	}
	if reset.UsedQuota != 0 || reset.UsedQuotaActual != 0 || reset.BalanceAlertNotified {
		t.Fatalf("reset key = %+v", reset)
	}
	if _, err := store.ResetUsageAdmin(ctx, 999999); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("ResetUsageAdmin missing error = %v, want ErrKeyNotFound", err)
	}

	todayStart := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	createAccountUsageLog(t, db, "apikey_delete_usage", user.ID, created.ID, 0, group.ID, "gpt-5", todayStart, 1, 2, 0, 0, 0.5, 0.75, 1.5)
	if _, err := store.DeleteOwned(ctx, otherUser.ID, created.ID); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("DeleteOwned wrong owner error = %v, want ErrKeyNotFound", err)
	}
	if deleted, err := store.DeleteOwned(ctx, user.ID, created.ID); err != nil {
		t.Fatalf("DeleteOwned returned error: %v", err)
	} else if deleted.KeyHash == "" {
		t.Fatalf("DeleteOwned returned empty key hash: %+v", deleted)
	}
	if _, err := store.FindOwned(ctx, user.ID, created.ID); !errors.Is(err, appapikey.ErrKeyNotFound) {
		t.Fatalf("FindOwned deleted error = %v, want ErrKeyNotFound", err)
	}
	usageLog, err := db.UsageLog.Query().Where().Only(ctx)
	if err != nil {
		t.Fatalf("query usage log after key delete: %v", err)
	}
	if hasKey, err := usageLog.QueryAPIKey().Exist(ctx); err != nil || hasKey {
		t.Fatalf("usage log has API key = %v err = %v, want false nil", hasKey, err)
	}
}

func TestUsageStoreSummariesStatsAndCursorFilters(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "usage-store@example.com")
	otherUser := createTestUser(t, db, "usage-other@example.com")
	group, err := db.Group.Create().SetName("Usage Group").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create usage group: %v", err)
	}
	account, err := db.Account.Create().
		SetName("Usage Account").
		SetEmail("usage-account@example.com").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create usage account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("usage-key").
		SetKeyHint("sk-usage").
		SetKeyHash("hash-usage-store").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create usage key: %v", err)
	}
	otherKey, err := db.APIKey.Create().
		SetName("usage-other-key").
		SetKeyHash("hash-usage-other").
		SetUserID(otherUser.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create other usage key: %v", err)
	}

	day := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	createAccountUsageLog(t, db, "usage_store_a", user.ID, key.ID, account.ID, group.ID, "gpt-5", day.Add(10*time.Hour), 10, 20, 5, 2, 1.00, 1.50, 2.00)
	createAccountUsageLog(t, db, "usage_store_b", user.ID, key.ID, account.ID, group.ID, "gpt-5-mini", day.Add(11*time.Hour), 3, 7, 0, 0, 2.00, 2.50, 3.00)
	createAccountUsageLog(t, db, "usage_store_other", otherUser.ID, otherKey.ID, 0, 0, "claude-3", day.Add(12*time.Hour), 1, 1, 0, 0, 9.00, 9.50, 10.00)

	store := NewUsageStore(db)
	apiKeyID := int64(key.ID)
	accountID := int64(account.ID)
	groupID := int64(group.ID)
	filter := appusage.ListFilter{
		PageSize:  1,
		APIKeyID:  &apiKeyID,
		AccountID: &accountID,
		GroupID:   &groupID,
		Platform:  "openai",
		Model:     "gpt-5",
		StartDate: "2026-06-20",
		EndDate:   "2026-06-20",
		TZ:        "UTC",
	}
	records, hasMore, next, err := store.ListUser(ctx, int64(user.ID), filter)
	if err != nil {
		t.Fatalf("ListUser returned error: %v", err)
	}
	if len(records) != 1 || !hasMore || next == nil || records[0].APIKeyName != "usage-key" ||
		records[0].AccountEmail != "usage-account@example.com" || records[0].GroupID != int64(group.ID) {
		t.Fatalf("ListUser records = %+v hasMore %v next %v", records, hasMore, next)
	}
	filter.BeforeID = *next
	nextPage, hasMore, next, err := store.ListUser(ctx, int64(user.ID), filter)
	if err != nil {
		t.Fatalf("ListUser next page returned error: %v", err)
	}
	if len(nextPage) != 1 || hasMore || next != nil {
		t.Fatalf("ListUser next page = %+v hasMore %v next %v", nextPage, hasMore, next)
	}

	adminRecords, hasMore, next, err := store.ListAdmin(ctx, appusage.ListFilter{PageSize: 10, UserID: storePtr(int64(otherUser.ID))})
	if err != nil {
		t.Fatalf("ListAdmin returned error: %v", err)
	}
	if len(adminRecords) != 1 || hasMore || next != nil || adminRecords[0].UserEmail != otherUser.Email ||
		adminRecords[0].APIKeyDeleted || adminRecords[0].AccountName != "-" {
		t.Fatalf("ListAdmin records = %+v hasMore %v next %v", adminRecords, hasMore, next)
	}

	summary, err := store.SummaryUser(ctx, int64(user.ID), appusage.StatsFilter{
		APIKeyID: &apiKeyID, Platform: "openai", Model: "gpt-5", StartDate: "2026-06-20", EndDate: "2026-06-20", TZ: "UTC",
	})
	if err != nil {
		t.Fatalf("SummaryUser returned error: %v", err)
	}
	if summary.TotalRequests != 2 || summary.TotalTokens != 47 || summary.TotalCost != 3.00 ||
		summary.TotalActualCost != 4.00 || summary.TotalBilledCost != 5.00 {
		t.Fatalf("SummaryUser = %+v", summary)
	}
	adminSummary, err := store.SummaryAdmin(ctx, appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("SummaryAdmin returned error: %v", err)
	}
	if adminSummary.TotalRequests != 3 || adminSummary.TotalCost != 12.00 {
		t.Fatalf("SummaryAdmin = %+v", adminSummary)
	}

	models, err := store.StatsByModel(ctx, appusage.StatsFilter{UserID: storePtr(int64(user.ID))})
	if err != nil {
		t.Fatalf("StatsByModel returned error: %v", err)
	}
	if len(models) != 2 || models[0].Requests != 1 || models[0].Tokens == 0 {
		t.Fatalf("model stats = %+v", models)
	}
	users, err := store.StatsByUser(ctx, appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("StatsByUser returned error: %v", err)
	}
	if len(users) != 2 || users[0].Requests < users[1].Requests {
		t.Fatalf("user stats = %+v", users)
	}
	accounts, err := store.StatsByAccount(ctx, appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("StatsByAccount returned error: %v", err)
	}
	if len(accounts) == 0 || accounts[0].AccountID != int64(account.ID) || accounts[0].Name != account.Name {
		t.Fatalf("account stats = %+v", accounts)
	}
	groups, err := store.StatsByGroup(ctx, appusage.StatsFilter{})
	if err != nil {
		t.Fatalf("StatsByGroup returned error: %v", err)
	}
	if len(groups) == 0 || groups[0].GroupID != int64(group.ID) || groups[0].Name != group.Name {
		t.Fatalf("group stats = %+v", groups)
	}
	trend, err := store.TrendEntries(ctx, appusage.TrendFilter{StatsFilter: appusage.StatsFilter{UserID: storePtr(int64(user.ID))}})
	if err != nil {
		t.Fatalf("TrendEntries returned error: %v", err)
	}
	if len(trend) != 2 || trend[0].CreatedAt == "" {
		t.Fatalf("trend entries = %+v", trend)
	}
	recent, err := store.TrendEntries(ctx, appusage.TrendFilter{DefaultRecentHours: 24 * 365 * 100})
	if err != nil {
		t.Fatalf("TrendEntries recent returned error: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("recent trend entries = %+v, want all three rows in the wide recent window", recent)
	}
}

func createAccountUsageLog(
	t *testing.T,
	db *ent.Client,
	billingEventID string,
	userID int,
	apiKeyID int,
	accountID int,
	groupID int,
	model string,
	createdAt time.Time,
	inputTokens int,
	outputTokens int,
	cachedInputTokens int,
	cacheCreationTokens int,
	totalCost float64,
	actualCost float64,
	billedCost float64,
) {
	t.Helper()
	builder := db.UsageLog.Create().
		SetBillingEventID(billingEventID).
		SetPlatform("openai").
		SetModel(model).
		SetInputTokens(inputTokens).
		SetOutputTokens(outputTokens).
		SetCachedInputTokens(cachedInputTokens).
		SetCacheCreationTokens(cacheCreationTokens).
		SetTotalCost(totalCost).
		SetActualCost(actualCost).
		SetBilledCost(billedCost).
		SetAccountCost(billedCost).
		SetDurationMs(123).
		SetFirstTokenMs(45).
		SetUserAgent("store-test").
		SetIPAddress("127.0.0.1").
		SetEndpoint("/v1/chat/completions").
		SetReasoningEffort("medium").
		SetUsageMetadata(map[string]string{"source": "store-test"}).
		SetUserIDSnapshot(userID).
		SetUserEmailSnapshot(fmt.Sprintf("user-%d@example.com", userID)).
		SetCreatedAt(createdAt)
	if userID > 0 {
		builder = builder.SetUserID(userID)
	}
	if apiKeyID > 0 {
		builder = builder.SetAPIKeyID(apiKeyID)
	}
	if accountID > 0 {
		builder = builder.SetAccountID(accountID)
	}
	if groupID > 0 {
		builder = builder.SetGroupID(groupID)
	}
	if _, err := builder.Save(context.Background()); err != nil {
		t.Fatalf("create usage log %q: %v", billingEventID, err)
	}
}

func storePtr[T any](value T) *T {
	return &value
}
