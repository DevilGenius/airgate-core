package auth

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
)

func TestGenerateAPIKeyPrefixesAndHashes(t *testing.T) {
	t.Parallel()

	key, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey error: %v", err)
	}
	if !strings.HasPrefix(key, apiKeyPrefix) {
		t.Fatalf("API key prefix = %q, want %q", key[:len(apiKeyPrefix)], apiKeyPrefix)
	}
	if hash != HashAPIKey(key) {
		t.Fatalf("hash = %q, want HashAPIKey(key)", hash)
	}

	adminKey, adminHash, err := GenerateAdminAPIKey()
	if err != nil {
		t.Fatalf("GenerateAdminAPIKey error: %v", err)
	}
	if !strings.HasPrefix(adminKey, adminKeyPrefix) {
		t.Fatalf("admin key prefix = %q, want %q", adminKey[:len(adminKeyPrefix)], adminKeyPrefix)
	}
	if adminHash != HashAPIKey(adminKey) {
		t.Fatalf("admin hash = %q, want HashAPIKey(adminKey)", adminHash)
	}
}

func TestGenerateAPIKeyReturnsRandomReaderError(t *testing.T) {
	previous := apiKeyRandom
	t.Cleanup(func() { apiKeyRandom = previous })
	readErr := errors.New("random failed")
	apiKeyRandom = errorReader{err: readErr}

	if _, _, err := GenerateAPIKey(); !errors.Is(err, readErr) {
		t.Fatalf("GenerateAPIKey error = %v, want %v", err, readErr)
	}
}

func TestAPIKeyInfoUserGroupRate(t *testing.T) {
	var nilInfo *APIKeyInfo
	if rate, ok := nilInfo.UserGroupRate(); ok || rate != 0 {
		t.Fatalf("nil UserGroupRate = %v, %v", rate, ok)
	}
	if rate, ok := (&APIKeyInfo{GroupID: 7}).UserGroupRate(); ok || rate != 0 {
		t.Fatalf("nil map UserGroupRate = %v, %v", rate, ok)
	}
	if rate, ok := (&APIKeyInfo{GroupID: 7, UserGroupRates: map[int64]float64{8: 2}}).UserGroupRate(); ok || rate != 0 {
		t.Fatalf("missing group UserGroupRate = %v, %v", rate, ok)
	}
	if rate, ok := (&APIKeyInfo{GroupID: 7, UserGroupRates: map[int64]float64{7: 0}}).UserGroupRate(); ok || rate != 0 {
		t.Fatalf("invalid group UserGroupRate = %v, %v", rate, ok)
	}
	if rate, ok := (&APIKeyInfo{GroupID: 7, UserGroupRates: map[int64]float64{7: 1.5}}).UserGroupRate(); !ok || rate != 1.5 {
		t.Fatalf("valid group UserGroupRate = %v, %v", rate, ok)
	}
}

func TestAdminKeyHelpers(t *testing.T) {
	short := "admin-short"
	if got := AdminKeyHint(short); got != short {
		t.Fatalf("AdminKeyHint short = %q", got)
	}
	long := "admin-1234567890abcdef"
	if got := AdminKeyHint(long); got != "admin-1234...cdef" {
		t.Fatalf("AdminKeyHint long = %q", got)
	}
	if !IsAdminAPIKey("admin-abc") {
		t.Fatal("IsAdminAPIKey admin-abc = false")
	}
	if IsAdminAPIKey("admin-") || IsAdminAPIKey("sk-abc") {
		t.Fatal("IsAdminAPIKey accepted invalid key")
	}
}

func TestAPIKeyCacheErrorMapping(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrInvalidAPIKey, "invalid"},
		{ErrAPIKeyExpired, "expired"},
		{ErrAPIKeyQuota, "quota"},
		{ErrAPIKeyGroupUnbound, "group_unbound"},
		{errors.New("other"), ""},
	}
	for _, tt := range tests {
		if got := apiKeyCacheErrorCode(tt.err); got != tt.code {
			t.Fatalf("apiKeyCacheErrorCode(%v) = %q, want %q", tt.err, got, tt.code)
		}
		if tt.code != "" {
			if got := apiKeyCacheErrorFromCode(tt.code); !errors.Is(got, tt.err) {
				t.Fatalf("apiKeyCacheErrorFromCode(%q) = %v, want %v", tt.code, got, tt.err)
			}
		}
	}
	if got := apiKeyCacheErrorFromCode("unknown"); got != nil {
		t.Fatalf("apiKeyCacheErrorFromCode unknown = %v", got)
	}
	if got := apiKeyRedisCacheKey("hash"); got != "ag:auth:key:hash" {
		t.Fatalf("apiKeyRedisCacheKey = %q", got)
	}
}

func TestAPIKeyCacheInvalidationWithoutRedis(t *testing.T) {
	SetAPIKeyCacheRedis(nil)
	key := "sk-cache-test"
	hash := HashAPIKey(key)
	apiKeyCache.Store(hash, apiKeyCacheEntry{err: ErrInvalidAPIKey})
	InvalidateAPIKeyCache(key)
	if _, ok := apiKeyCache.Load(hash); ok {
		t.Fatal("InvalidateAPIKeyCache(key) did not delete local entry")
	}

	apiKeyCache.Store("one", apiKeyCacheEntry{})
	apiKeyCache.Store("two", apiKeyCacheEntry{})
	InvalidateAPIKeyCache("")
	if _, ok := apiKeyCache.Load("one"); ok {
		t.Fatal("InvalidateAPIKeyCache(empty) did not delete entry one")
	}
	if _, ok := apiKeyCache.Load("two"); ok {
		t.Fatal("InvalidateAPIKeyCache(empty) did not delete entry two")
	}

	apiKeyCache.Store("hash", apiKeyCacheEntry{})
	InvalidateAPIKeyHashCache("")
	if _, ok := apiKeyCache.Load("hash"); !ok {
		t.Fatal("InvalidateAPIKeyHashCache(empty) should not delete entries")
	}
	InvalidateAPIKeyHashCache("hash")
	if _, ok := apiKeyCache.Load("hash"); ok {
		t.Fatal("InvalidateAPIKeyHashCache(hash) did not delete local entry")
	}
}

func TestAPIKeyRedisCacheStoreAndLoad(t *testing.T) {
	resetAPIKeyTestCache(t)
	client, mock := redismock.NewClientMock()
	SetAPIKeyCacheRedis(client)

	info := &APIKeyInfo{KeyID: 7, KeyName: "cached", UserID: 3, GroupID: 5}
	infoRaw, err := json.Marshal(apiKeyRedisEntry{Info: info})
	if err != nil {
		t.Fatalf("marshal info cache entry: %v", err)
	}
	mock.ExpectSet(apiKeyRedisCacheKey("hash-info"), infoRaw, apiKeyRedisCacheTTL).SetVal("OK")
	storeAPIKeyRedisCache("hash-info", info, nil)

	errRaw, err := json.Marshal(apiKeyRedisEntry{Err: "expired"})
	if err != nil {
		t.Fatalf("marshal error cache entry: %v", err)
	}
	mock.ExpectSet(apiKeyRedisCacheKey("hash-err"), errRaw, apiKeyRedisCacheTTL).SetVal("OK")
	storeAPIKeyRedisCache("hash-err", nil, ErrAPIKeyExpired)

	storeAPIKeyRedisCache("hash-empty", nil, nil)
	storeAPIKeyRedisCache("hash-unknown-err", nil, errors.New("unknown"))
	storeAPIKeyRedisCache("hash-marshal-error", &APIKeyInfo{QuotaUSD: math.NaN()}, nil)

	mock.ExpectGet(apiKeyRedisCacheKey("hash-info")).SetVal(string(infoRaw))
	loaded, loadErr, ok := loadAPIKeyCacheFromRedis(context.Background(), "hash-info")
	if !ok || loadErr != nil || loaded == nil || loaded.KeyID != 7 {
		t.Fatalf("load info = %+v, %v, %v", loaded, loadErr, ok)
	}

	mock.ExpectGet(apiKeyRedisCacheKey("hash-err")).SetVal(string(errRaw))
	loaded, loadErr, ok = loadAPIKeyCacheFromRedis(context.Background(), "hash-err")
	if !ok || !errors.Is(loadErr, ErrAPIKeyExpired) || loaded != nil {
		t.Fatalf("load err = %+v, %v, %v", loaded, loadErr, ok)
	}

	unknownRaw, err := json.Marshal(apiKeyRedisEntry{Err: "unknown"})
	if err != nil {
		t.Fatalf("marshal unknown cache entry: %v", err)
	}
	mock.ExpectGet(apiKeyRedisCacheKey("hash-unknown-code")).SetVal(string(unknownRaw))
	loaded, loadErr, ok = loadAPIKeyCacheFromRedis(context.Background(), "hash-unknown-code")
	if ok || loadErr != nil || loaded != nil {
		t.Fatalf("load unknown code = %+v, %v, %v", loaded, loadErr, ok)
	}

	mock.ExpectGet(apiKeyRedisCacheKey("hash-bad-json")).SetVal("{")
	mock.ExpectDel(apiKeyRedisCacheKey("hash-bad-json")).SetVal(1)
	loaded, loadErr, ok = loadAPIKeyCacheFromRedis(context.Background(), "hash-bad-json")
	if ok || loadErr != nil || loaded != nil {
		t.Fatalf("load bad json = %+v, %v, %v", loaded, loadErr, ok)
	}

	mock.ExpectGet(apiKeyRedisCacheKey("hash-missing")).RedisNil()
	loaded, loadErr, ok = loadAPIKeyCacheFromRedis(context.Background(), "hash-missing")
	if ok || loadErr != nil || loaded != nil {
		t.Fatalf("load missing = %+v, %v, %v", loaded, loadErr, ok)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestAPIKeyRedisInvalidation(t *testing.T) {
	resetAPIKeyTestCache(t)
	client, mock := redismock.NewClientMock()
	SetAPIKeyCacheRedis(client)

	key := "sk-cache-delete"
	mock.ExpectDel(apiKeyRedisCacheKey(HashAPIKey(key))).SetVal(1)
	InvalidateAPIKeyCache(key)

	mock.ExpectDel(apiKeyRedisCacheKey("hash-delete")).SetVal(1)
	InvalidateAPIKeyHashCache("hash-delete")

	mock.ExpectScan(0, "ag:auth:key:*", 100).SetVal([]string{"ag:auth:key:a", "ag:auth:key:b"}, 2)
	mock.ExpectDel("ag:auth:key:a", "ag:auth:key:b").SetVal(2)
	mock.ExpectScan(2, "ag:auth:key:*", 100).SetVal(nil, 0)
	InvalidateAPIKeyCache("")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestValidateAPIKeyUsesSharedRedisCache(t *testing.T) {
	resetAPIKeyTestCache(t)
	client, mock := redismock.NewClientMock()
	SetAPIKeyCacheRedis(client)

	infoKey := "sk-shared-info"
	infoHash := HashAPIKey(infoKey)
	apiKeyCache.Store(infoHash, apiKeyCacheEntry{
		info:      &APIKeyInfo{KeyID: 1},
		expiresAt: time.Now().Add(-time.Second),
	})
	infoRaw, err := json.Marshal(apiKeyRedisEntry{Info: &APIKeyInfo{KeyID: 12, GroupID: 3}})
	if err != nil {
		t.Fatalf("marshal shared info: %v", err)
	}
	mock.ExpectGet(apiKeyRedisCacheKey(infoHash)).SetVal(string(infoRaw))
	info, err := ValidateAPIKey(context.Background(), nil, infoKey)
	if err != nil || info == nil || info.KeyID != 12 {
		t.Fatalf("shared info = %+v, %v", info, err)
	}

	errKey := "sk-shared-err"
	errHash := HashAPIKey(errKey)
	errRaw, err := json.Marshal(apiKeyRedisEntry{Err: "quota"})
	if err != nil {
		t.Fatalf("marshal shared error: %v", err)
	}
	mock.ExpectGet(apiKeyRedisCacheKey(errHash)).SetVal(string(errRaw))
	info, err = ValidateAPIKey(context.Background(), nil, errKey)
	if info != nil || !errors.Is(err, ErrAPIKeyQuota) {
		t.Fatalf("shared err = %+v, %v", info, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestDeleteAllAPIKeyRedisCacheStopsOnScanError(t *testing.T) {
	resetAPIKeyTestCache(t)
	client, mock := redismock.NewClientMock()
	SetAPIKeyCacheRedis(client)

	mock.ExpectScan(0, "ag:auth:key:*", 100).SetErr(errors.New("scan failed"))
	deleteAllAPIKeyRedisCache()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}
