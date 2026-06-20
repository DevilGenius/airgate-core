package store

import (
	"context"
	"errors"
	"testing"
	"time"

	entapikey "github.com/DevilGenius/airgate-core/ent/apikey"
	entuser "github.com/DevilGenius/airgate-core/ent/user"
	appauth "github.com/DevilGenius/airgate-core/internal/app/auth"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	appsubscription "github.com/DevilGenius/airgate-core/internal/app/subscription"
)

func TestAuthStoreFindCreateAndValidateAPIKeySession(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	group, err := db.Group.Create().
		SetName("Auth Group").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	user, err := db.User.Create().
		SetEmail("auth-store@example.com").
		SetPasswordHash("hash").
		SetUsername("Auth User").
		SetRole(entuser.RoleAdmin).
		SetStatus(entuser.StatusActive).
		SetBalance(12.5).
		SetMaxConcurrency(7).
		SetGroupRates(map[int64]float64{int64(group.ID): 2.25}).
		AddAllowedGroupIDs(group.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	store := NewAuthStore(db)
	found, err := store.FindByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("FindByEmail returned error: %v", err)
	}
	if found.ID != user.ID || found.Role != "admin" || found.Status != "active" ||
		found.Balance != 12.5 || found.MaxConcurrency != 7 || found.GroupRates[int64(group.ID)] != 2.25 {
		t.Fatalf("FindByEmail = %+v", found)
	}
	found.GroupRates[int64(group.ID)] = 99
	foundAgain, err := store.FindByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("FindByEmail after clone mutation returned error: %v", err)
	}
	if foundAgain.GroupRates[int64(group.ID)] != 2.25 {
		t.Fatalf("group rates clone leaked mutation: %+v", foundAgain.GroupRates)
	}

	withGroups, err := store.FindByID(ctx, user.ID, true)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if len(withGroups.AllowedGroupIDs) != 1 || withGroups.AllowedGroupIDs[0] != int64(group.ID) {
		t.Fatalf("allowed groups = %+v, want %d", withGroups.AllowedGroupIDs, group.ID)
	}
	if _, err := store.FindByID(ctx, 999999, false); !errors.Is(err, appauth.ErrUserNotFound) {
		t.Fatalf("FindByID missing error = %v, want ErrUserNotFound", err)
	}
	if _, err := store.FindByEmail(ctx, "missing@example.com"); !errors.Is(err, appauth.ErrUserNotFound) {
		t.Fatalf("FindByEmail missing error = %v, want ErrUserNotFound", err)
	}
	exists, err := store.EmailExists(ctx, user.Email)
	if err != nil || !exists {
		t.Fatalf("EmailExists existing = %v err = %v, want true nil", exists, err)
	}
	exists, err = store.EmailExists(ctx, "missing@example.com")
	if err != nil || exists {
		t.Fatalf("EmailExists missing = %v err = %v, want false nil", exists, err)
	}

	created, err := store.Create(ctx, appauth.CreateUserInput{
		Email:          "created-auth@example.com",
		PasswordHash:   "created-hash",
		Username:       "Created",
		Role:           "user",
		Status:         "active",
		Balance:        3.5,
		MaxConcurrency: 4,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.Email != "created-auth@example.com" || created.Balance != 3.5 || created.MaxConcurrency != 4 {
		t.Fatalf("created user = %+v", created)
	}

	activeKey, err := db.APIKey.Create().
		SetName("active-key").
		SetKeyHash("hash-active").
		SetUserID(user.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create active key: %v", err)
	}
	sessionUser, err := store.ValidateAPIKeySession(ctx, user.ID, activeKey.ID)
	if err != nil {
		t.Fatalf("ValidateAPIKeySession active returned error: %v", err)
	}
	if sessionUser.ID != user.ID || sessionUser.Email != user.Email {
		t.Fatalf("session user = %+v, want user %d", sessionUser, user.ID)
	}

	expiredAt := time.Now().Add(-time.Minute)
	expiredKey, err := db.APIKey.Create().
		SetName("expired-key").
		SetKeyHash("hash-expired").
		SetUserID(user.ID).
		SetExpiresAt(expiredAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create expired key: %v", err)
	}
	if _, err := store.ValidateAPIKeySession(ctx, user.ID, expiredKey.ID); !errors.Is(err, appauth.ErrInvalidAPIKeySession) {
		t.Fatalf("expired session error = %v, want ErrInvalidAPIKeySession", err)
	}

	disabledKey, err := db.APIKey.Create().
		SetName("disabled-key").
		SetKeyHash("hash-disabled").
		SetUserID(user.ID).
		SetStatus(entapikey.StatusDisabled).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled key: %v", err)
	}
	if _, err := store.ValidateAPIKeySession(ctx, user.ID, disabledKey.ID); !errors.Is(err, appauth.ErrInvalidAPIKeySession) {
		t.Fatalf("disabled key session error = %v, want ErrInvalidAPIKeySession", err)
	}
	if _, err := store.ValidateAPIKeySession(ctx, user.ID, 999999); !errors.Is(err, appauth.ErrInvalidAPIKeySession) {
		t.Fatalf("missing key session error = %v, want ErrInvalidAPIKeySession", err)
	}

	disabledUser, err := db.User.Create().
		SetEmail("disabled-session@example.com").
		SetPasswordHash("hash").
		SetStatus(entuser.StatusDisabled).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled user: %v", err)
	}
	disabledUserKey, err := db.APIKey.Create().
		SetName("disabled-user-key").
		SetKeyHash("hash-disabled-user").
		SetUserID(disabledUser.ID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled user key: %v", err)
	}
	if _, err := store.ValidateAPIKeySession(ctx, disabledUser.ID, disabledUserKey.ID); !errors.Is(err, appauth.ErrUserDisabled) {
		t.Fatalf("disabled user session error = %v, want ErrUserDisabled", err)
	}
}

func TestSettingsStoreListAndUpsertMany(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	store := NewSettingsStore(db)
	if err := store.UpsertMany(ctx, []appsettings.ItemInput{
		{Key: "site.name", Value: "AirGate"},
		{Key: "billing.currency", Value: "USD", Group: "billing"},
		{Key: "auth.enabled", Value: "true", Group: "auth"},
	}); err != nil {
		t.Fatalf("UpsertMany create returned error: %v", err)
	}

	all, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List all returned error: %v", err)
	}
	if got := settingPairs(all); got != "auth:auth.enabled=true,billing:billing.currency=USD,general:site.name=AirGate" {
		t.Fatalf("all settings = %s", got)
	}

	billing, err := store.List(ctx, "billing")
	if err != nil {
		t.Fatalf("List billing returned error: %v", err)
	}
	if len(billing) != 1 || billing[0].Key != "billing.currency" || billing[0].Value != "USD" {
		t.Fatalf("billing settings = %+v", billing)
	}

	if err := store.UpsertMany(ctx, []appsettings.ItemInput{
		{Key: "billing.currency", Value: "CNY"},
		{Key: "site.name", Value: "AirGate Core", Group: "system"},
	}); err != nil {
		t.Fatalf("UpsertMany update returned error: %v", err)
	}
	updated, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List updated returned error: %v", err)
	}
	if got := settingPairs(updated); got != "auth:auth.enabled=true,billing:billing.currency=CNY,system:site.name=AirGate Core" {
		t.Fatalf("updated settings = %s", got)
	}
	if err := store.UpsertMany(ctx, nil); err != nil {
		t.Fatalf("UpsertMany empty returned error: %v", err)
	}
}

func settingPairs(items []appsettings.Setting) string {
	result := ""
	for index, item := range items {
		if index > 0 {
			result += ","
		}
		result += item.Group + ":" + item.Key + "=" + item.Value
	}
	return result
}

func TestProxyStoreCRUDAndFilters(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	store := NewProxyStore(db)
	created, err := store.Create(ctx, appproxy.CreateInput{
		Name:     "Primary Proxy",
		Protocol: "http",
		Address:  "127.0.0.1",
		Port:     8080,
		Username: "alice",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.Name != "Primary Proxy" || created.Protocol != "http" || created.Username != "alice" || created.Status != "active" {
		t.Fatalf("created proxy = %+v", created)
	}
	if _, err := store.Create(ctx, appproxy.CreateInput{
		Name: "Disabled Proxy", Protocol: "socks5", Address: "10.0.0.2", Port: 1080,
	}); err != nil {
		t.Fatalf("create second proxy: %v", err)
	}

	disabledStatus := "disabled"
	name := "Updated Proxy"
	protocol := "socks5"
	address := "192.168.1.10"
	port := 1081
	username := "bob"
	password := "changed"
	updated, err := store.Update(ctx, created.ID, appproxy.UpdateInput{
		Name: &name, Protocol: &protocol, Address: &address, Port: &port,
		Username: &username, Password: &password, Status: &disabledStatus,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Name != name || updated.Protocol != protocol || updated.Address != address ||
		updated.Port != port || updated.Username != username || updated.Password != password || updated.Status != disabledStatus {
		t.Fatalf("updated proxy = %+v", updated)
	}

	found, err := store.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if found.Name != name || found.Protocol != protocol {
		t.Fatalf("found proxy = %+v", found)
	}
	if _, err := store.FindByID(ctx, 999999); !errors.Is(err, appproxy.ErrProxyNotFound) {
		t.Fatalf("FindByID missing error = %v, want ErrProxyNotFound", err)
	}

	list, total, err := store.List(ctx, appproxy.ListFilter{Page: 1, PageSize: 20, Keyword: "Updated", Status: "disabled"})
	if err != nil {
		t.Fatalf("List filtered returned error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("filtered list = total %d list %+v, want updated proxy", total, list)
	}
	list, total, err = store.List(ctx, appproxy.ListFilter{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("List paged returned error: %v", err)
	}
	if total != 2 || len(list) != 1 {
		t.Fatalf("paged list = total %d len %d, want total 2 len 1", total, len(list))
	}

	if _, err := store.Update(ctx, 999999, appproxy.UpdateInput{Name: &name}); !errors.Is(err, appproxy.ErrProxyNotFound) {
		t.Fatalf("Update missing error = %v, want ErrProxyNotFound", err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, appproxy.ErrProxyNotFound) {
		t.Fatalf("Delete missing error = %v, want ErrProxyNotFound", err)
	}
}

func TestSubscriptionStoreCRUDListAndUsageClone(t *testing.T) {
	db := enttestOpen(t)
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createTestUser(t, db, "subscription-user@example.com")
	otherUser := createTestUser(t, db, "subscription-other@example.com")
	group, err := db.Group.Create().SetName("Subscription Group").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	otherGroup, err := db.Group.Create().SetName("Other Group").SetPlatform("claude").Save(ctx)
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}

	store := NewSubscriptionStore(db)
	effectiveAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	expiresAt := effectiveAt.Add(24 * time.Hour)
	created, err := store.Create(ctx, appsubscription.CreateInput{
		UserID: user.ID, GroupID: group.ID, EffectiveAt: effectiveAt, ExpiresAt: expiresAt, Status: "active",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if created.UserID != user.ID || created.GroupID != group.ID || created.GroupName != group.Name || created.Status != "active" {
		t.Fatalf("created subscription = %+v", created)
	}

	if _, err := db.UserSubscription.UpdateOneID(created.ID).
		SetUsage(map[string]any{"daily": map[string]any{"used": float64(2)}}).
		Save(ctx); err != nil {
		t.Fatalf("set subscription usage: %v", err)
	}
	count, err := store.BulkCreate(ctx, appsubscription.BulkCreateInput{
		UserIDs: []int{user.ID, otherUser.ID}, GroupID: otherGroup.ID,
		EffectiveAt: effectiveAt, ExpiresAt: expiresAt.Add(48 * time.Hour), Status: "expired",
	})
	if err != nil {
		t.Fatalf("BulkCreate returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("BulkCreate count = %d, want 2", count)
	}

	userSubs, total, err := store.ListByUser(ctx, appsubscription.UserListFilter{UserID: user.ID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListByUser returned error: %v", err)
	}
	if total != 2 || len(userSubs) != 2 {
		t.Fatalf("ListByUser total = %d list = %+v, want two subscriptions", total, userSubs)
	}
	var activeSub appsubscription.Subscription
	for _, item := range userSubs {
		if item.ID == created.ID {
			activeSub = item
		}
	}
	if activeSub.ID == 0 || activeSub.UserID != user.ID || activeSub.Usage["daily"] == nil {
		t.Fatalf("active subscription from ListByUser = %+v", activeSub)
	}
	activeSub.Usage["daily"] = "mutated"
	again, _, err := store.ListByUser(ctx, appsubscription.UserListFilter{UserID: user.ID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListByUser after clone mutation returned error: %v", err)
	}
	for _, item := range again {
		if item.ID == created.ID && item.Usage["daily"] == "mutated" {
			t.Fatalf("subscription usage clone leaked mutation: %+v", item.Usage)
		}
	}

	active, err := store.ListActiveByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListActiveByUser returned error: %v", err)
	}
	if len(active) != 1 || active[0].ID != created.ID {
		t.Fatalf("active subscriptions = %+v, want created active subscription", active)
	}

	adminByStatus, total, err := store.ListAdmin(ctx, appsubscription.AdminListFilter{Page: 1, PageSize: 20, Status: "expired"})
	if err != nil {
		t.Fatalf("ListAdmin status returned error: %v", err)
	}
	if total != 2 || len(adminByStatus) != 2 {
		t.Fatalf("admin expired = total %d list %+v, want two expired subscriptions", total, adminByStatus)
	}
	adminByUser, total, err := store.ListAdmin(ctx, appsubscription.AdminListFilter{Page: 1, PageSize: 20, UserID: &otherUser.ID})
	if err != nil {
		t.Fatalf("ListAdmin user returned error: %v", err)
	}
	if total != 1 || len(adminByUser) != 1 || adminByUser[0].UserID != otherUser.ID || adminByUser[0].GroupName != otherGroup.Name {
		t.Fatalf("admin by user = total %d list %+v, want other user's subscription", total, adminByUser)
	}

	newExpiresAt := expiresAt.Add(72 * time.Hour)
	newStatus := "suspended"
	updated, err := store.Update(ctx, created.ID, appsubscription.UpdateInput{ExpiresAt: &newExpiresAt, Status: &newStatus})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if updated.Status != newStatus || !updated.ExpiresAt.Equal(newExpiresAt) {
		t.Fatalf("updated subscription = %+v", updated)
	}
	if _, err := store.Update(ctx, 999999, appsubscription.UpdateInput{Status: &newStatus}); !errors.Is(err, appsubscription.ErrSubscriptionNotFound) {
		t.Fatalf("Update missing error = %v, want ErrSubscriptionNotFound", err)
	}
	if got := mapSubscriptionUsage(nil); got != nil {
		t.Fatalf("mapSubscriptionUsage(nil) = %+v, want nil", got)
	}
}
