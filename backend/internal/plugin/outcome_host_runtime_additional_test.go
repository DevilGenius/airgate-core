package plugin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
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
