package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"

	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
)

type captureMonitorRecorder struct {
	records  []monitoring.EventInput
	resolved []monitoring.ResolveQuery
}

func (r *captureMonitorRecorder) Record(_ context.Context, input monitoring.EventInput) {
	r.records = append(r.records, input)
}

func (r *captureMonitorRecorder) ResolveBySubject(_ context.Context, query monitoring.ResolveQuery) {
	r.resolved = append(r.resolved, query)
}

type failingManualStateWriter struct {
	*stubStateWriter
	recoverErr error
	disableErr error
}

func (s *failingManualStateWriter) ManualRecover(ctx context.Context, accountID int) error {
	if s.recoverErr != nil {
		return s.recoverErr
	}
	return s.stubStateWriter.ManualRecover(ctx, accountID)
}

func (s *failingManualStateWriter) ManualDisable(ctx context.Context, accountID int, reason string) error {
	if s.disableErr != nil {
		return s.disableErr
	}
	return s.stubStateWriter.ManualDisable(ctx, accountID, reason)
}

func TestMonitorRecorderHelpersRecordAndResolve(t *testing.T) {
	var nilService *Service
	nilService.SetMonitorRecorder(&captureMonitorRecorder{})

	recorder := &captureMonitorRecorder{}
	service := NewService(stubRepository{}, nil, nil, nil)
	service.SetMonitorRecorder(recorder)

	ctx := t.Context()
	service.recordTokenRefreshFailure(ctx, Account{ID: 0}, "ignored", errors.New("ignored"), monitoring.SeverityError)
	service.recordTokenRefreshFailure(ctx, Account{
		ID: 7, Name: "oauth", Platform: "openai", Type: "oauth", State: "active",
	}, "reauth_required", errors.New("expired"), monitoring.SeverityCritical)
	service.recordConnectivityTestFailure(ctx, Account{
		ID: 8, Name: "checker", Platform: "claude", Type: "apikey", State: "degraded",
	}, "claude-sonnet", "client_error", nil)
	service.resolveAccountMonitorEvents(ctx, 7)
	service.resolveAccountMonitorEvents(ctx, 0)

	if len(recorder.records) != 2 {
		t.Fatalf("records = %d, want 2", len(recorder.records))
	}
	first := recorder.records[0]
	if first.Source != monitoring.SourceTokenRefresh || first.Severity != monitoring.SeverityCritical ||
		first.SubjectID != "7" || first.AccountID == nil || *first.AccountID != 7 ||
		first.ErrorCode != "reauth_required" || first.Message != "expired" ||
		first.Detail["operation"] != "token_refresh" {
		t.Fatalf("token monitor record = %+v", first)
	}
	second := recorder.records[1]
	if second.Source != monitoring.SourceAccountChecker || second.Message != "" ||
		second.Detail["model"] != "claude-sonnet" || second.Detail["operation"] != "connectivity_test" {
		t.Fatalf("connectivity monitor record = %+v", second)
	}
	if len(recorder.resolved) != 1 || recorder.resolved[0].SubjectID != "7" ||
		recorder.resolved[0].AccountID == nil || *recorder.resolved[0].AccountID != 7 {
		t.Fatalf("resolved queries = %+v", recorder.resolved)
	}
}

func TestTokenRefreshLoopBranchesWithoutPlugin(t *testing.T) {
	listErr := errors.New("list failed")
	service := NewService(stubRepository{
		listAll: func(context.Context, ListFilter) ([]Account, error) {
			return nil, listErr
		},
	}, stubPluginCatalog{}, nil, nil)
	service.refreshAllOAuthTokens(t.Context())

	calls := 0
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	service = NewService(stubRepository{
		listAll: func(context.Context, ListFilter) ([]Account, error) {
			calls++
			return []Account{
				{ID: 1, Platform: "openai", Type: "apikey", Credentials: map[string]string{"api_key": "sk"}},
				{ID: 2, Platform: "openai", Type: "oauth"},
				{ID: 3, Platform: "openai", Type: "oauth", Credentials: map[string]string{"access_token": "tok"}},
			}, nil
		},
	}, stubPluginCatalog{}, nil, nil)

	service.runTokenRefreshLoop(canceled)
	if calls != 1 {
		t.Fatalf("runTokenRefreshLoop list calls = %d, want 1", calls)
	}

	_, err := service.RefreshToken(t.Context(), 3)
	if !errors.Is(err, ErrTokenRefreshUnsupported) {
		t.Fatalf("RefreshToken error = %v, want ErrTokenRefreshUnsupported", err)
	}
	if _, err := service.refreshToken(t.Context(), Account{ID: 4, Platform: "openai"}, false); !errors.Is(err, ErrTokenRefreshUnsupported) {
		t.Fatalf("refreshToken unsupported error = %v", err)
	}
	service.triggerUsageProbe(t.Context(), nil, "openai", 4, map[string]string{"access_token": "tok"})
}

func TestToggleSchedulingBranches(t *testing.T) {
	writer := newStubStateWriter()
	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, _ LoadOptions) (Account, error) {
			if id == 1 {
				return Account{ID: id, State: "disabled"}, nil
			}
			return Account{ID: id, State: "active"}, nil
		},
	}, nil, nil, writer)

	got, err := service.ToggleScheduling(t.Context(), 1)
	if err != nil || got.State != "active" || !writer.recovered[1] {
		t.Fatalf("ToggleScheduling disabled result=%+v err=%v recovered=%+v", got, err, writer.recovered)
	}
	got, err = service.ToggleScheduling(t.Context(), 2)
	if err != nil || got.State != "disabled" || writer.disabled[2] != "手动关闭" {
		t.Fatalf("ToggleScheduling active result=%+v err=%v disabled=%+v", got, err, writer.disabled)
	}

	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{}, errors.New("missing")
		},
	}, nil, nil, nil)
	if _, err := service.ToggleScheduling(t.Context(), 9); err == nil {
		t.Fatal("ToggleScheduling should return repository error")
	}

	stateWriter := &failingManualStateWriter{stubStateWriter: newStubStateWriter(), recoverErr: errors.New("recover failed")}
	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 5, State: "disabled"}, nil
		},
	}, nil, nil, stateWriter)
	if _, err := service.ToggleScheduling(t.Context(), 5); err == nil {
		t.Fatal("ToggleScheduling should return ManualRecover error")
	}

	stateWriter = &failingManualStateWriter{stubStateWriter: newStubStateWriter(), disableErr: errors.New("disable failed")}
	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 6, State: "active"}, nil
		},
	}, nil, nil, stateWriter)
	if _, err := service.ToggleScheduling(t.Context(), 6); err == nil {
		t.Fatal("ToggleScheduling should return ManualDisable error")
	}
}

func TestPrepareConnectivityTestValidationBranches(t *testing.T) {
	repoErr := errors.New("lookup failed")
	service := NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{}, repoErr
		},
	}, stubPluginCatalog{}, nil, nil)
	if _, err := service.PrepareConnectivityTest(t.Context(), 1, ""); !errors.Is(err, repoErr) {
		t.Fatalf("PrepareConnectivityTest lookup error = %v, want %v", err, repoErr)
	}

	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 1, Platform: "openai", Type: "oauth"}, nil
		},
	}, stubPluginCatalog{}, nil, nil)
	if _, err := service.PrepareConnectivityTest(t.Context(), 1, ""); !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("PrepareConnectivityTest plugin error = %v, want ErrPluginNotFound", err)
	}

	ct := &ConnectivityTest{run: func(context.Context, http.ResponseWriter) (ConnectivityTestTiming, error) {
		return ConnectivityTestTiming{}, errors.New("run failed")
	}}
	if err := ct.Run(t.Context(), nil); err == nil {
		t.Fatal("ConnectivityTest.Run should return runner error")
	}
}

func TestRedisUsageCacheReadWriteAndInvalidation(t *testing.T) {
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	info := AccountUsageInfo{
		Windows: []AccountUsageWindow{{
			Key:         "5h",
			Label:       "5h",
			UsedPercent: 25,
			ResetAt:     now.Add(time.Hour).Format(time.RFC3339),
		}},
	}
	payload, err := json.Marshal(newAccountUsageCachePayload(info, now))
	if err != nil {
		t.Fatalf("marshal usage payload: %v", err)
	}

	rdb, mock := redismock.NewClientMock()
	service := NewService(stubRepository{}, nil, nil, nil)
	service.now = func() time.Time { return now }
	service.SetUsageCacheRedis(rdb)

	mock.ExpectGet(accountcache.UsageKey(7)).SetVal(string(payload))
	got, ok := service.getUsageInfoForAccount(t.Context(), 7)
	if !ok || len(got.Windows) != 1 {
		t.Fatalf("getUsageInfoForAccount = %+v ok=%v", got, ok)
	}

	mock.ExpectGet(accountcache.UsageKey(8)).SetVal(`{bad`)
	mock.ExpectDel(accountcache.UsageKey(8)).SetVal(1)
	if _, ok := service.getUsageInfoForAccount(t.Context(), 8); ok {
		t.Fatal("invalid Redis payload should miss")
	}

	mock.ExpectMGet(accountcache.UsageKey(7), accountcache.UsageKey(8), accountcache.UsageKey(9)).SetVal([]interface{}{string(payload), `{bad`, nil})
	mock.ExpectDel(accountcache.UsageKey(8)).SetVal(1)
	infos, missing := service.getUsageInfosForAccounts(t.Context(), "openai", []Account{
		{ID: 7, Platform: "openai", Type: "oauth"},
		{ID: 8, Platform: "openai", Type: "oauth"},
		{ID: 9, Platform: "openai", Type: "apikey"},
	})
	if len(infos) != 1 || len(missing) != 1 || missing[0].ID != 8 {
		t.Fatalf("getUsageInfosForAccounts infos=%+v missing=%+v", infos, missing)
	}

	mock.ExpectMGet(accountcache.UsageKey(7)).SetVal([]interface{}{string(payload)})
	existing := service.getUsageInfosForCacheWrites(t.Context(), []accountUsageCacheWrite{{account: Account{ID: 7}, info: info}}, now)
	if len(existing) != 1 || len(existing[7].Windows) != 1 {
		t.Fatalf("getUsageInfosForCacheWrites redis existing = %+v", existing)
	}

	mock.Regexp().ExpectSet(accountcache.UsageKey(7), ".*", time.Hour).SetVal("OK")
	service.writeUsageInfoCache(t.Context(), "openai", 7, info, now)

	mock.ExpectDel(accountcache.UsageKey(7)).SetVal(1)
	service.writeUsageInfoCache(t.Context(), "openai", 7, AccountUsageInfo{}, now)

	mock.ExpectSMembers(accountcache.PlatformKey("openai")).SetVal([]string{"7", "bad", "0", "8"})
	mock.ExpectDel(accountcache.UsageKey(7), accountcache.UsageKey(8)).SetVal(2)
	service.InvalidateUsageCache("openai")

	mock.ExpectScan(0, accountcache.UsagePattern(), 50).SetVal([]string{accountcache.UsageKey(7)}, 5)
	mock.ExpectDel(accountcache.UsageKey(7)).SetVal(1)
	mock.ExpectScan(5, accountcache.UsagePattern(), 50).SetVal(nil, 0)
	service.InvalidateUsageCache("")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestRedisStatsAndProfileCaches(t *testing.T) {
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	day := accountcache.Day(now)
	rdb, mock := redismock.NewClientMock()
	service := NewService(stubRepository{}, nil, nil, nil)
	service.now = func() time.Time { return now }
	service.SetUsageCacheRedis(rdb)

	todayKey := accountcache.TodayStatsKey(day)
	mock.ExpectHMGet(todayKey, accountcache.TodayStatsFields(7)...).SetVal([]interface{}{"3", "100", "1.25", "2.5", now.Format(time.RFC3339)})
	stats, missing := service.loadTodayStatsCache(t.Context(), day, []int{7})
	if len(missing) != 0 || stats[7].Requests != 3 || stats[7].Tokens != 100 {
		t.Fatalf("loadTodayStatsCache stats=%+v missing=%v", stats, missing)
	}

	mock.ExpectHSetNX(todayKey, accountcache.TodayStatsField(7, "requests"), int64(4)).SetVal(true)
	mock.ExpectHSetNX(todayKey, accountcache.TodayStatsField(7, "tokens"), int64(120)).SetVal(true)
	mock.ExpectHSetNX(todayKey, accountcache.TodayStatsField(7, "account_cost"), 1.5).SetVal(true)
	mock.ExpectHSetNX(todayKey, accountcache.TodayStatsField(7, "user_cost"), 3.0).SetVal(true)
	mock.ExpectHSetNX(todayKey, accountcache.TodayStatsField(7, "updated_at"), now.UTC().Format(time.RFC3339)).SetVal(true)
	mock.ExpectExpire(todayKey, accountcache.TodayStatsTTL).SetVal(true)
	service.writeTodayStatsCache(t.Context(), day, 7, AccountWindowStats{Requests: 4, Tokens: 120, AccountCost: 1.5, UserCost: 3})

	mock.ExpectMGet(accountcache.ImageTotalKey(7), accountcache.ImageTotalKey(8)).SetVal([]interface{}{"10", nil})
	mock.ExpectMGet(accountcache.ImageTodayKey(day, 7), accountcache.ImageTodayKey(day, 8)).SetVal([]interface{}{"2", "1"})
	imageStats, imageMissing := service.loadImageStatsCache(t.Context(), day, []int{7, 8})
	if len(imageStats) != 1 || imageStats[7].TotalCount != 10 || imageStats[7].TodayCount != 2 ||
		len(imageMissing) != 1 || imageMissing[0] != 8 {
		t.Fatalf("loadImageStatsCache stats=%+v missing=%v", imageStats, imageMissing)
	}

	mock.ExpectSet(accountcache.ImageTotalKey(7), int64(11), accountcache.ImageTotalTTL).SetVal("OK")
	mock.ExpectSet(accountcache.ImageTodayKey(day, 7), int64(3), accountcache.TodayStatsTTL).SetVal("OK")
	service.writeImageStatsCache(t.Context(), day, 7, AccountImageStats{TodayCount: 3, TotalCount: 11})

	oldPayload, err := json.Marshal(accountProfileCachePayload{ID: 7, Platform: "old"})
	if err != nil {
		t.Fatalf("marshal old profile: %v", err)
	}
	badPayload := []byte(`{"id":999}`)
	freshAccount := Account{
		ID: 7, Name: "fresh", Platform: "openai", Type: "oauth", State: "active",
		CreatedAt: now, UpdatedAt: now, RateMultiplier: 1,
	}
	freshPayload, err := json.Marshal(accountProfileCacheFromAccount(freshAccount))
	if err != nil {
		t.Fatalf("marshal fresh profile: %v", err)
	}

	mock.ExpectMGet(accountcache.ProfileKey(7), accountcache.ProfileKey(8), accountcache.ProfileKey(9)).SetVal([]interface{}{string(freshPayload), string(badPayload), nil})
	mock.ExpectDel(accountcache.ProfileKey(8)).SetVal(1)
	accounts, profileMissing := service.loadAccountProfilesForUsage(t.Context(), "openai", []int{7, 8, 9})
	if len(accounts) != 1 || accounts[0].ID != 7 || len(profileMissing) != 2 {
		t.Fatalf("loadAccountProfilesForUsage accounts=%+v missing=%v", accounts, profileMissing)
	}

	mock.ExpectMGet(accountcache.ProfileKey(7)).SetVal([]interface{}{string(oldPayload)})
	mock.ExpectSRem(accountcache.PlatformKey("old"), 7).SetVal(1)
	mock.Regexp().ExpectSet(accountcache.ProfileKey(7), ".*", accountcache.ProfileTTL).SetVal("OK")
	mock.ExpectSAdd(accountcache.PlatformKey("openai"), 7).SetVal(1)
	mock.ExpectExpire(accountcache.PlatformKey("openai"), accountcache.ProfileTTL).SetVal(true)
	service.cacheAccountProfiles(t.Context(), []Account{{ID: 0}, freshAccount})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestDeleteAccountCacheKeysUsesProfilePlatform(t *testing.T) {
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	day := accountcache.Day(now)
	rdb, mock := redismock.NewClientMock()
	service := NewService(stubRepository{}, nil, nil, nil)
	service.now = func() time.Time { return now }
	service.SetUsageCacheRedis(rdb)

	profileBody, err := json.Marshal(accountProfileCachePayload{ID: 7, Platform: "openai"})
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	mock.ExpectGet(accountcache.ProfileKey(7)).SetVal(string(profileBody))
	mock.ExpectSRem(accountcache.PlatformKey("openai"), 7).SetVal(1)
	mock.ExpectDel(accountcache.ProfileKey(7), accountcache.UsageKey(7), accountcache.ImageTotalKey(7), accountcache.ImageTodayKey(day, 7)).SetVal(4)
	mock.ExpectHDel(todayKeyForTest(day), accountcache.TodayStatsFields(7)...).SetVal(5)

	service.deleteAccountCacheKeys([]int{0, 7})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func todayKeyForTest(day string) string {
	return accountcache.TodayStatsKey(day)
}
