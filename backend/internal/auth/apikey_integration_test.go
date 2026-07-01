package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	entuser "github.com/DevilGenius/airgate-core/ent/user"
	"github.com/DevilGenius/airgate-core/internal/dispatchresolver"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestValidateAPIKeyIncludesUserEmail(t *testing.T) {
	resetAPIKeyTestCache(t)
	db := testdb.OpenMemoryEnt(t, "apikey_validate", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user, err := db.User.Create().
		SetEmail("apikey-user@example.com").
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := db.Group.Create().
		SetName("OpenAI").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("key").
		SetKeyHash(hash).
		SetUser(user).
		SetGroup(group).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	info, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.UserID != user.ID || info.UserEmail != user.Email {
		t.Fatalf("ValidateAPIKey user info = (%d, %q), want (%d, %q)", info.UserID, info.UserEmail, user.ID, user.Email)
	}
	if info.GroupID != group.ID || info.GroupName != group.Name {
		t.Fatalf("ValidateAPIKey group info = (%d, %q), want (%d, %q)", info.GroupID, info.GroupName, group.ID, group.Name)
	}
}

func TestValidateAPIKeyForLogin(t *testing.T) {
	resetAPIKeyTestCache(t)
	db := testdb.OpenMemoryEnt(t, "apikey_login", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createAPIKeyTestUser(t, ctx, db, "login-user@example.com")
	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	created, err := db.APIKey.Create().
		SetName("login key").
		SetKeyHash(hash).
		SetUser(user).
		Save(ctx)
	if err != nil {
		t.Fatalf("create login api key: %v", err)
	}

	info, err := ValidateAPIKeyForLogin(ctx, db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKeyForLogin returned error: %v", err)
	}
	if info.KeyID != created.ID || info.KeyName != "login key" || info.UserID != user.ID || info.UserEmail != user.Email {
		t.Fatalf("login api key info = %+v", info)
	}

	expiredKey, expiredHash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey expired: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("expired").
		SetKeyHash(expiredHash).
		SetUser(user).
		SetExpiresAt(time.Now().Add(-time.Hour)).
		Save(ctx); err != nil {
		t.Fatalf("create expired api key: %v", err)
	}
	if _, err := ValidateAPIKeyForLogin(ctx, db, expiredKey); !errors.Is(err, ErrAPIKeyExpired) {
		t.Fatalf("expired login api key error = %v, want %v", err, ErrAPIKeyExpired)
	}
	if _, err := ValidateAPIKeyForLogin(ctx, db, "sk-missing"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("missing login api key error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func TestValidateAPIKeyRejectsDisabledUser(t *testing.T) {
	resetAPIKeyTestCache(t)
	db := testdb.OpenMemoryEnt(t, "apikey_disabled_user", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createAPIKeyTestUser(t, ctx, db, "disabled-user@example.com")
	if err := db.User.UpdateOneID(user.ID).SetStatus(entuser.StatusDisabled).Exec(ctx); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	group := createAPIKeyTestGroup(t, ctx, db, "OpenAI", "openai")
	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if _, err := db.APIKey.Create().
		SetName("disabled-user-key").
		SetKeyHash(hash).
		SetUser(user).
		SetGroup(group).
		Save(ctx); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if _, err := ValidateAPIKeyForLogin(ctx, db, key); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("disabled user login error = %v, want %v", err, ErrInvalidAPIKey)
	}
	if _, err := ValidateAPIKey(ctx, db, key); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("disabled user validate error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func TestValidateAPIKeyForLoginRejectsMissingUserEdge(t *testing.T) {
	previous := queryAPIKeyForLogin
	t.Cleanup(func() { queryAPIKeyForLogin = previous })
	queryAPIKeyForLogin = func(context.Context, *ent.Client, string) (*ent.APIKey, error) {
		return &ent.APIKey{ID: 1, Name: "edge missing"}, nil
	}

	if _, err := ValidateAPIKeyForLogin(t.Context(), nil, "sk-edge-missing"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("missing user edge error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func TestValidateAPIKeyCoversCacheAndFailureBranches(t *testing.T) {
	resetAPIKeyTestCache(t)
	db := testdb.OpenMemoryEnt(t, "apikey_validate_branches", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	user := createAPIKeyTestUser(t, ctx, db, "full-user@example.com")
	group := createAPIKeyTestGroup(t, ctx, db, "Full", "openai")

	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey full: %v", err)
	}
	created, err := db.APIKey.Create().
		SetName("full").
		SetKeyHash(hash).
		SetUser(user).
		SetGroup(group).
		SetQuotaUsd(10).
		SetUsedQuota(2.5).
		SetSellRate(1.25).
		SetMaxConcurrency(4).
		Save(ctx)
	if err != nil {
		t.Fatalf("create full api key: %v", err)
	}

	info, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("ValidateAPIKey returned error: %v", err)
	}
	if info.KeyID != created.ID || info.UserID != user.ID || info.GroupID != group.ID || info.UserBalance != 25 || info.UserGroupRates[int64(group.ID)] != 1.5 {
		t.Fatalf("api key info = %+v", info)
	}
	if info.QuotaUSD != 10 || info.UsedQuota != 2.5 || info.SellRate != 1.25 || info.KeyMaxConcurrency != 4 || info.UserMaxConcurrency != 6 {
		t.Fatalf("api key limits = %+v", info)
	}
	if info.GroupRateMultiplier != 2 || info.GroupServiceTier != "priority" || info.GroupForceInstructions != "be concise" {
		t.Fatalf("group fields = %+v", info)
	}
	if info.GroupOperationPolicies["responses"] != true || info.GroupPluginSettings["claude"]["claude_code_only"] != "true" {
		t.Fatalf("group policy fields = %+v", info)
	}

	cached, err := ValidateAPIKey(ctx, db, key)
	if err != nil {
		t.Fatalf("cached ValidateAPIKey returned error: %v", err)
	}
	if cached.KeyID != created.ID {
		t.Fatalf("cached key id = %d, want %d", cached.KeyID, created.ID)
	}

	expiredKey := createAPIKeyTestKey(t, ctx, db, user, group, "expired", func(c *ent.APIKeyCreate) {
		c.SetExpiresAt(time.Now().Add(-time.Hour))
	})
	if _, err := ValidateAPIKey(ctx, db, expiredKey); !errors.Is(err, ErrAPIKeyExpired) {
		t.Fatalf("expired api key error = %v, want %v", err, ErrAPIKeyExpired)
	}

	quotaKey := createAPIKeyTestKey(t, ctx, db, user, group, "quota", func(c *ent.APIKeyCreate) {
		c.SetQuotaUsd(1).SetUsedQuota(1)
	})
	if _, err := ValidateAPIKey(ctx, db, quotaKey); !errors.Is(err, ErrAPIKeyQuota) {
		t.Fatalf("quota api key error = %v, want %v", err, ErrAPIKeyQuota)
	}

	unboundKey := createAPIKeyTestKey(t, ctx, db, user, nil, "unbound", nil)
	if _, err := ValidateAPIKey(ctx, db, unboundKey); !errors.Is(err, ErrAPIKeyGroupUnbound) {
		t.Fatalf("unbound api key error = %v, want %v", err, ErrAPIKeyGroupUnbound)
	}

	if _, err := ValidateAPIKey(ctx, db, "sk-missing-validate"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("missing api key error = %v, want %v", err, ErrInvalidAPIKey)
	}
	if _, err := ValidateAPIKey(ctx, db, "sk-missing-validate"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("cached missing api key error = %v, want %v", err, ErrInvalidAPIKey)
	}

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ValidateAPIKey(canceledCtx, db, "sk-context-canceled"); err == nil || errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("context canceled api key error = %v, want wrapped database error", err)
	}
}

func TestValidateAPIKeyRejectsMissingUserEdge(t *testing.T) {
	resetAPIKeyTestCache(t)
	previous := queryAPIKeyForValidation
	t.Cleanup(func() { queryAPIKeyForValidation = previous })
	queryAPIKeyForValidation = func(context.Context, *ent.Client, string) (*ent.APIKey, error) {
		return &ent.APIKey{
			ID:    2,
			Name:  "edge missing",
			Edges: ent.APIKeyEdges{Group: &ent.Group{ID: 9, Name: "group", Platform: "openai"}},
		}, nil
	}

	if _, err := ValidateAPIKey(t.Context(), nil, "sk-edge-missing"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("missing user edge error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func TestHydrateAPIKeyInfoUsesRouteGraph(t *testing.T) {
	hydrateAPIKeyInfo(nil)

	existing := &dispatchresolver.CompiledResolver{}
	alreadyHydrated := &APIKeyInfo{GroupDispatchResolver: existing}
	hydrateAPIKeyInfo(alreadyHydrated)
	if alreadyHydrated.GroupDispatchResolver != existing {
		t.Fatal("already hydrated resolver should not be replaced")
	}

	group := &ent.Group{
		ID:        123,
		Name:      "RouteGraph",
		Platform:  "openai",
		UpdatedAt: time.Now(),
		DispatchDsl: sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
			ID:         "models",
			Candidates: []sdk.DispatchCandidate{{Scheduling: "${model}"}},
		}}},
	}
	restore := routegraph.SetSnapshotForTesting([]*ent.Group{group})
	t.Cleanup(restore)

	info := &APIKeyInfo{GroupID: group.ID}
	hydrateAPIKeyInfo(info)
	if info.GroupDispatchResolver == nil {
		t.Fatal("expected resolver from routegraph snapshot")
	}
}

func TestValidateAdminAPIKey(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "admin_api_key", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	ctx := context.Background()
	key, hash, err := GenerateAdminAPIKey()
	if err != nil {
		t.Fatalf("GenerateAdminAPIKey: %v", err)
	}
	if _, err := db.Setting.Create().
		SetGroup("security").
		SetKey("admin_api_key_hash").
		SetValue(hash).
		Save(ctx); err != nil {
		t.Fatalf("create admin key setting: %v", err)
	}
	if err := ValidateAdminAPIKey(ctx, db, key); err != nil {
		t.Fatalf("ValidateAdminAPIKey returned error: %v", err)
	}
	if err := ValidateAdminAPIKey(ctx, db, "admin-wrong"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("wrong admin key error = %v, want %v", err, ErrInvalidAPIKey)
	}
	if err := db.Setting.UpdateOneID(1).SetValue("").Exec(ctx); err != nil {
		t.Fatalf("clear admin key setting: %v", err)
	}
	if err := ValidateAdminAPIKey(ctx, db, key); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("empty admin key setting error = %v, want %v", err, ErrInvalidAPIKey)
	}

	missingDB := testdb.OpenMemoryEnt(t, "admin_api_key_missing", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := missingDB.Close(); err != nil {
			t.Fatalf("close missing db: %v", err)
		}
	}()
	if err := ValidateAdminAPIKey(ctx, missingDB, key); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("missing admin key setting error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func resetAPIKeyTestCache(t *testing.T) {
	t.Helper()
	previousRedis := apiKeyRedis
	SetAPIKeyCacheRedis(nil)
	apiKeyCache.Range(func(k, _ any) bool {
		apiKeyCache.Delete(k)
		return true
	})
	t.Cleanup(func() {
		SetAPIKeyCacheRedis(previousRedis)
		apiKeyCache.Range(func(k, _ any) bool {
			apiKeyCache.Delete(k)
			return true
		})
	})
}

func createAPIKeyTestUser(t *testing.T, ctx context.Context, db *ent.Client, email string) *ent.User {
	t.Helper()
	user, err := db.User.Create().
		SetEmail(email).
		SetPasswordHash("secret").
		SetBalance(25).
		SetMaxConcurrency(6).
		SetGroupRates(map[int64]float64{1: 1.5}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return user
}

func createAPIKeyTestGroup(t *testing.T, ctx context.Context, db *ent.Client, name, platform string) *ent.Group {
	t.Helper()
	group, err := db.Group.Create().
		SetName(name).
		SetPlatform(platform).
		SetRateMultiplier(2).
		SetServiceTier("priority").
		SetForceInstructions("be concise").
		SetOperationPolicies(map[string]bool{"responses": true}).
		SetPluginSettings(map[string]map[string]string{"claude": {"claude_code_only": "true"}}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group %q: %v", name, err)
	}
	return group
}

func createAPIKeyTestKey(t *testing.T, ctx context.Context, db *ent.Client, user *ent.User, group *ent.Group, name string, mutate func(*ent.APIKeyCreate)) string {
	t.Helper()
	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey %s: %v", name, err)
	}
	create := db.APIKey.Create().
		SetName(name).
		SetKeyHash(hash).
		SetUser(user)
	if group != nil {
		create.SetGroup(group)
	}
	if mutate != nil {
		mutate(create)
	}
	if _, err := create.Save(ctx); err != nil {
		t.Fatalf("create api key %s: %v", name, err)
	}
	return key
}
