package plugin

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/ent/usagelog"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestForwarderWriteResultOutcomeBranchesWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_write_result_outcomes", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	accountEnt, err := db.Account.Create().
		SetName("writer").
		SetPlatform("openai").
		SetType("apikey").
		SetRateMultiplier(1.1).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	forwarder := &Forwarder{
		scheduler:  scheduler.NewScheduler(db, nil),
		calculator: billing.NewCalculator(),
	}
	state := &forwardState{
		startedAt:         time.Now().Add(-20 * time.Millisecond),
		requestPath:       "/v1/chat/completions",
		requestID:         "req-write-result",
		model:             "gpt-4.1",
		requestedPlatform: "openai",
		sessionID:         "session-1",
		plugin:            &PluginInstance{Name: "openai", Platform: "openai"},
		keyInfo: &auth.APIKeyInfo{
			UserID:              10,
			UserEmail:           "user@example.com",
			KeyID:               11,
			GroupID:             12,
			GroupPlatform:       "openai",
			GroupRateMultiplier: 1,
			SellRate:            1,
		},
		account: accountEnt,
	}

	c, recorder := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	forwarder.writeResult(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind: sdk.OutcomeSuccess,
			Upstream: sdk.UpstreamResponse{
				StatusCode: http.StatusCreated,
				Headers: http.Header{
					"Content-Type": {"application/json"},
					"X-Trace":      {"result"},
				},
				Body: []byte(`{"id":"resp_success","ok":true}`),
			},
		},
		duration: 20 * time.Millisecond,
	})
	if recorder.Code != http.StatusCreated || recorder.Body.String() != `{"id":"resp_success","ok":true}` {
		t.Fatalf("success response status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Trace"); got != "result" {
		t.Fatalf("success X-Trace = %q", got)
	}

	c, recorder = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	forwarder.writeResult(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind:   sdk.OutcomeClientError,
			Reason: "context_length exceeded",
		},
		duration: 5 * time.Millisecond,
	})
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), contextTooLargeMessage) {
		t.Fatalf("client error status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	c, recorder = pluginTestContext(http.MethodPost, "/v1/chat/completions")
	forwarder.writeResult(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{
			Kind:       sdk.OutcomeAccountRateLimited,
			Reason:     "too many requests",
			RetryAfter: 1500 * time.Millisecond,
			Upstream:   sdk.UpstreamResponse{StatusCode: http.StatusTooManyRequests},
		},
		duration: 10 * time.Millisecond,
	})
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "2" {
		t.Fatalf("rate limit status=%d retry-after=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "upstream_rate_limit") {
		t.Fatalf("rate limit body = %s", recorder.Body.String())
	}
}

func TestForwarderUpdateAccountCredentialsMergesExistingValues(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_update_account_credentials", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	accountEnt, err := db.Account.Create().
		SetName("credential-merge").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
			"keep":          "unchanged",
		}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	forwarder := &Forwarder{db: db}
	forwarder.updateAccountCredentials(accountEnt.ID, map[string]string{
		"access_token": "new-access",
		"email":        " OAuth.User@Example.COM ",
		"expires_at":   "2026-06-20T00:00:00Z",
	})

	updated, err := db.Account.Get(ctx, accountEnt.ID)
	if err != nil {
		t.Fatalf("get updated account: %v", err)
	}
	want := map[string]string{
		"access_token":  "new-access",
		"refresh_token": "old-refresh",
		"keep":          "unchanged",
		"expires_at":    "2026-06-20T00:00:00Z",
	}
	for key, value := range want {
		if updated.Credentials[key] != value {
			t.Fatalf("credential %s = %q, want %q in %#v", key, updated.Credentials[key], value, updated.Credentials)
		}
	}
	if updated.Email == nil || *updated.Email != "oauth.user@example.com" || updated.Credentials["email"] != "oauth.user@example.com" {
		t.Fatalf("account email = %#v credentials=%#v", updated.Email, updated.Credentials)
	}
	forwarder.updateAccountCredentials(accountEnt.ID, map[string]string{
		"access_token": "newer-access",
		"email":        "not-an-email",
	})
	updated, err = db.Account.Get(ctx, accountEnt.ID)
	if err != nil {
		t.Fatalf("get account after invalid plugin email: %v", err)
	}
	if updated.Credentials["access_token"] != "newer-access" || updated.Credentials["email"] != "oauth.user@example.com" ||
		updated.Email == nil || *updated.Email != "oauth.user@example.com" {
		t.Fatalf("invalid plugin email should preserve identity and update other credentials: %+v", updated)
	}

	forwarder.updateAccountCredentials(accountEnt.ID+999, map[string]string{"access_token": "ignored"})

	forwarder.credentialPersistSem = make(chan struct{}, 1)
	forwarder.persistUpdatedCredentials(accountEnt.ID, map[string]string{"refresh_token": "async-refresh"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		updated, err = db.Account.Get(ctx, accountEnt.ID)
		if err != nil {
			t.Fatalf("get async updated account: %v", err)
		}
		if updated.Credentials["refresh_token"] == "async-refresh" {
			if updated.Credentials["email"] != "oauth.user@example.com" {
				t.Fatalf("async update lost credentials.email: %#v", updated.Credentials)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("async credential update did not persist: %#v", updated.Credentials)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := db.Account.UpdateOneID(accountEnt.ID).SetDeletedAt(time.Now()).Exec(ctx); err != nil {
		t.Fatalf("soft delete account: %v", err)
	}
	credentialsBeforeDeletedUpdate := maps.Clone(updated.Credentials)
	forwarder.updateAccountCredentials(accountEnt.ID, map[string]string{"access_token": "must-not-change"})
	updated, err = db.Account.Get(ctx, accountEnt.ID)
	if err != nil {
		t.Fatalf("get soft-deleted account: %v", err)
	}
	if !maps.Equal(updated.Credentials, credentialsBeforeDeletedUpdate) {
		t.Fatalf("soft-deleted credentials changed: before=%#v after=%#v", credentialsBeforeDeletedUpdate, updated.Credentials)
	}
}

func TestForwarderRecordUsagePersistsFallbackRecord(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_record_usage_fallback", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	user, err := db.User.Create().
		SetEmail("record-usage@example.com").
		SetPasswordHash("hash").
		SetBalance(10).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	group, err := db.Group.Create().
		SetName("record-usage").
		SetPlatform("openai").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	accountEnt, err := db.Account.Create().
		SetName("record-usage").
		SetPlatform("openai").
		SetType("apikey").
		SetRateMultiplier(1.5).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	key, err := db.APIKey.Create().
		SetName("record-usage").
		SetKeyHash("hash-record-usage").
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetSellRate(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	recorder := billing.NewRecorder(db, 1)
	recorder.Record(billing.UsageRecord{
		BillingEventID: "prefill-record-usage",
		UserID:         user.ID,
		UserEmail:      user.Email,
		AccountID:      accountEnt.ID,
		GroupID:        group.ID,
		Platform:       "openai",
		Model:          "prefill",
	})
	forwarder := &Forwarder{
		scheduler:  scheduler.NewScheduler(db, nil),
		calculator: billing.NewCalculator(),
		recorder:   recorder,
	}
	state := &forwardState{
		requestPath:       "/v1/responses",
		requestedPlatform: "openai",
		model:             "gpt-4.1",
		reasoningEffort:   "low",
		stream:            true,
		plugin:            &PluginInstance{Name: "openai", Platform: "openai"},
		account:           accountEnt,
		keyInfo: &auth.APIKeyInfo{
			UserID:              user.ID,
			UserEmail:           user.Email,
			KeyID:               key.ID,
			GroupID:             group.ID,
			GroupPlatform:       "openai",
			GroupRateMultiplier: 1,
			SellRate:            2,
		},
	}
	c, _ := pluginTestContext(http.MethodPost, "/v1/responses")
	c.Request.Header.Set("User-Agent", "record-usage-test")
	c.Request.Header.Set("X-Forwarded-For", "198.51.100.30, 172.18.0.1")
	c.Request.RemoteAddr = "172.18.0.1:4567"

	forwarder.recordUsage(c, state, forwardExecution{
		outcome: sdk.ForwardOutcome{Usage: &sdk.Usage{
			Model:           "gpt-4.1-mini",
			InputTokens:     100,
			OutputTokens:    20,
			InputCost:       0.25,
			OutputCost:      0.75,
			FirstEventMs:    123,
			FirstTokenMs:    456,
			WSDialMs:        23,
			ReasoningEffort: "medium",
			Metadata:        map[string]string{"openai.response_id": "resp_usage"},
		}},
		duration: 1500 * time.Millisecond,
	})

	count, err := db.UsageLog.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count usage logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("usage log count = %d, want fallback persisted one record", count)
	}
	log, err := db.UsageLog.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query usage log: %v", err)
	}
	if log.Model != "gpt-4.1-mini" || log.Endpoint != "/v1/responses" || log.ReasoningEffort != "medium" {
		t.Fatalf("usage log core fields = model:%q endpoint:%q reasoning:%q", log.Model, log.Endpoint, log.ReasoningEffort)
	}
	if log.InputTokens != 100 || log.OutputTokens != 20 || log.FirstEventMs != 123 || log.FirstTokenMs != 456 || log.WsDialMs != 23 || !log.Stream {
		t.Fatalf("usage log tokens/timing = input:%d output:%d event:%d token:%d dial:%d stream:%v", log.InputTokens, log.OutputTokens, log.FirstEventMs, log.FirstTokenMs, log.WsDialMs, log.Stream)
	}
	if log.TotalCost != 1 || log.ActualCost != 1 || log.BilledCost != 2 || log.AccountCost != 1.5 {
		t.Fatalf("usage log costs = total:%v actual:%v billed:%v account:%v", log.TotalCost, log.ActualCost, log.BilledCost, log.AccountCost)
	}
	if log.UsageMetadata[responseIDUsageMetadataKey] != "resp_usage" {
		t.Fatalf("usage metadata = %#v", log.UsageMetadata)
	}
	if log.IPAddress != "198.51.100.30" {
		t.Fatalf("usage log ip = %q, want forwarded client ip", log.IPAddress)
	}
	if exists, err := db.UsageLog.Query().Where(usagelog.BillingEventIDEQ("prefill-record-usage")).Exist(ctx); err != nil || exists {
		t.Fatalf("prefill queued record persisted = %v/%v, want false nil", exists, err)
	}
}

func TestHostServiceOutcomeAndCapacityBranchesWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_host_outcome_capacity", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	accountEnt, err := db.Account.Create().
		SetName("host-capacity").
		SetPlatform("openai").
		SetType("apikey").
		SetMaxConcurrency(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	var nilHost *HostService
	release, ok := nilHost.acquireHostForwardAccountCapacity(ctx, accountEnt)
	if !ok || release == nil {
		t.Fatalf("nil host capacity ok=%v releaseNil=%v", ok, release == nil)
	}
	release()

	host := &HostService{
		scheduler:   scheduler.NewScheduler(db, nil),
		concurrency: scheduler.NewConcurrencyManager(nil),
	}
	release, ok = host.acquireHostForwardAccountCapacity(ctx, accountEnt)
	if !ok || release == nil {
		t.Fatalf("host capacity ok=%v releaseNil=%v", ok, release == nil)
	}
	release()
	if got := hostForwardMaxConcurrency(&ent.Account{MaxConcurrency: 0}); got != scheduler.DefaultAccountMaxConcurrency {
		t.Fatalf("default host max concurrency = %d", got)
	}
	if got := hostForwardMaxConcurrency(accountEnt); got != 2 {
		t.Fatalf("host max concurrency = %d, want 2", got)
	}

	host.applyHostOutcome(ctx, accountEnt.ID, accountEnt, "gpt-4.1", sdk.ForwardOutcome{
		Kind:     sdk.OutcomeAccountDead,
		Reason:   "invalid credentials",
		Upstream: sdk.UpstreamResponse{StatusCode: http.StatusUnauthorized},
	}, 25*time.Millisecond)
	updated, err := db.Account.Get(ctx, accountEnt.ID)
	if err != nil {
		t.Fatalf("get updated account: %v", err)
	}
	if updated.State != entaccount.StateDisabled || !strings.Contains(updated.ErrorMsg, "[gpt-4.1] invalid credentials") {
		t.Fatalf("updated account state=%s error=%q", updated.State, updated.ErrorMsg)
	}
}
