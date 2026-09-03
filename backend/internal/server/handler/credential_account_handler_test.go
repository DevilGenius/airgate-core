package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestCredentialAccountOverviewReturnsSanitizedSnapshot(t *testing.T) {
	ctx := t.Context()
	db := testdb.OpenMemoryEnt(t, "credential_account_overview", schema.WithGlobalUniqueID(false))
	defer func() { _ = db.Close() }()

	if _, err := db.Account.Create().
		SetName("active-team").
		SetEmail("active@example.com").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "secret", "plan_type": "plus"}).
		SetMaxConcurrency(20).
		SetPriority(120).
		SetRateMultiplier(1).
		Save(ctx); err != nil {
		t.Fatalf("create active account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("disabled-team").
		SetEmail("disabled@example.com").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "secret-2", "plan_type": "K12"}).
		SetState(account.StateDisabled).
		SetMaxConcurrency(10).
		SetRateMultiplier(1).
		Save(ctx); err != nil {
		t.Fatalf("create disabled account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("prolite-team").
		SetEmail("prolite@example.com").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "secret-3", "plan_type": "Self_serve_business_prolite"}).
		SetMaxConcurrency(0).
		SetRateMultiplier(1).
		Save(ctx); err != nil {
		t.Fatalf("create ProLite account: %v", err)
	}

	accountService := appaccount.NewService(
		store.NewAccountStore(db),
		accountHandlerPluginCatalogStub{allPluginMetadata: []plugin.PluginMeta{{
			Platform: "openai",
			Metadata: map[string]string{
				"account.oauth_plans": `[
					{"key":"plus","credential_key":"plan_type","matches":["plus"]},
					{"key":"team","credential_key":"plan_type","matches":["team","Team","k12","K12","self_serve_business_prolite","Self_serve_business_prolite"]}
				]`,
			},
		}}},
		scheduler.NewConcurrencyManager(nil),
		nil,
	)
	dashboardService := appdashboard.NewService(store.NewDashboardStore(db, nil))
	handler := NewCredentialAccountHandler(accountService, dashboardService, nil)
	w := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/overview",
		`{"platform":"openai","account_type":"oauth_plan:openai:team,oauth_plan:openai:plus","page":1,"page_size":10}`,
		nil,
		nil,
		handler.GetOverview,
	)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	var envelope struct {
		Data dto.CredentialAccountsOverviewResp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if envelope.Data.SchemaVersion != 4 || envelope.Data.Traffic.Source != "dashboard_stats" ||
		envelope.Data.Traffic.RPM1M != 0 || envelope.Data.Traffic.TPM1M != 0 ||
		envelope.Data.Traffic.RPM10M != 0 || envelope.Data.Traffic.TPM10M != 0 ||
		envelope.Data.Traffic.TPMPerRPMCoefficient1M != 0 || envelope.Data.Traffic.TPMPerRPMCoefficient10M != 0 {
		t.Fatalf("overview metadata = %+v", envelope.Data)
	}
	if envelope.Data.UsageEstimate.StandardCostPerMinute1M != 0 ||
		envelope.Data.UsageEstimate.StandardCostPerMinute10M != 0 ||
		envelope.Data.UsageEstimate.Plus5h.Status != "insufficient" ||
		envelope.Data.UsageEstimate.Pro5h.Status != "insufficient" ||
		envelope.Data.UsageEstimate.Plus7d.Status != "insufficient" ||
		envelope.Data.UsageEstimate.Pro7d.Status != "insufficient" {
		t.Fatalf("usage estimate defaults = %+v", envelope.Data.UsageEstimate)
	}
	if envelope.Data.AccountSummary.Total != 3 ||
		envelope.Data.AccountSummary.ByState["active"] != 2 ||
		envelope.Data.AccountSummary.ByState["disabled"] != 1 ||
		envelope.Data.AccountSummary.ConfiguredCapacity != 20 {
		t.Fatalf("account summary = %+v", envelope.Data.AccountSummary)
	}
	if len(envelope.Data.Accounts.List) != 3 {
		t.Fatalf("account list = %+v", envelope.Data.Accounts)
	}
	byName := make(map[string]dto.CredentialAccountResp, len(envelope.Data.Accounts.List))
	for _, item := range envelope.Data.Accounts.List {
		byName[item.Name] = item
	}
	if byName["active-team"].Email == "" || byName["active-team"].PlanType != "plus" ||
		byName["active-team"].Priority != 120 || byName["disabled-team"].PlanType != "k12" || byName["disabled-team"].Priority != 50 ||
		byName["prolite-team"].PlanType != "prolite" {
		t.Fatalf("account priority mapping = %+v", byName)
	}
	if strings.Contains(w.Body.String(), "access_token") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("overview leaked credentials: %s", w.Body.String())
	}
	if envelope.Data.FreeAccounts != nil {
		t.Fatalf("free account summary should be omitted when Free is not requested: %+v", envelope.Data.FreeAccounts)
	}
}

type credentialOverviewConcurrencyReader struct {
	ids    []int
	counts map[int]int
}

func (r *credentialOverviewConcurrencyReader) GetCurrentCounts(_ context.Context, ids []int) map[int]int {
	r.ids = append([]int(nil), ids...)
	return r.counts
}

func (r *credentialOverviewConcurrencyReader) GetWorkingCounts(context.Context) map[int]int {
	return nil
}

func TestCredentialAccountOverviewSeparatesFreeAccountsAndSkipsFreeCapacity(t *testing.T) {
	ctx := t.Context()
	db := testdb.OpenMemoryEnt(t, "credential_account_overview_free", schema.WithGlobalUniqueID(false))
	defer func() { _ = db.Close() }()

	create := func(name, state, reason string, plan string) *ent.Account {
		t.Helper()
		builder := db.Account.Create().
			SetName(name).
			SetEmail(name + "@example.com").
			SetPlatform("openai").
			SetType("oauth").
			SetCredentials(map[string]string{"access_token": "secret", "plan_type": plan}).
			SetMaxConcurrency(10)
		if state != "" {
			builder = builder.SetState(account.State(state))
		}
		if reason != "" {
			builder = builder.SetErrorMsg(reason)
		}
		item, err := builder.Save(ctx)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return item
	}

	paid := create("paid", "active", "", "plus")
	freeActive := create("free-active", "active", "", "free")
	freeUnauthorized := create("free-401", "disabled", "HTTP 401: invalid token", "free")
	_ = create("free-manual", "disabled", "手动关闭", "free")
	_ = create("free-limited", "rate_limited", "quota window saturated", "free")
	_ = create("free-degraded", "degraded", "HTTP 403: forbidden", "free")
	paidUnauthorized := create("paid-401", "disabled", "HTTP 401: invalid token", "plus")

	reader := &credentialOverviewConcurrencyReader{counts: map[int]int{
		paid.ID:             3,
		freeActive.ID:       7,
		freeUnauthorized.ID: 8,
		paidUnauthorized.ID: 9,
	}}
	accountService := appaccount.NewService(store.NewAccountStore(db), nil, reader, nil)
	dashboardService := appdashboard.NewService(store.NewDashboardStore(db, nil))
	handler := NewCredentialAccountHandler(accountService, dashboardService, nil)
	w := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/overview",
		`{"platform":"openai","account_type":"oauth","include_free":true,"page":1,"page_size":10}`,
		nil,
		nil,
		handler.GetOverview,
	)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	var envelope struct {
		Data dto.CredentialAccountsOverviewResp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if envelope.Data.FreeAccounts == nil {
		t.Fatal("free account summary is missing when Free is requested")
	}
	free := *envelope.Data.FreeAccounts
	if free.Total != 5 || free.ByState["active"] != 1 || free.ByState["disabled"] != 2 ||
		free.ByState["rate_limited"] != 1 || free.ByState["degraded"] != 1 || free.UnauthorizedCount != 1 ||
		len(free.UnauthorizedAccounts) != 1 || free.UnauthorizedAccounts[0].Name != "free-401" {
		t.Fatalf("free account summary = %+v", free)
	}
	if envelope.Data.Accounts.Total != 2 || envelope.Data.AccountSummary.Total != 2 ||
		envelope.Data.AccountSummary.ByState["active"] != 1 || envelope.Data.AccountSummary.ByState["disabled"] != 1 ||
		envelope.Data.AccountSummary.ConfiguredCapacity != 10 || envelope.Data.AccountSummary.CurrentConcurrency != 12 {
		t.Fatalf("detailed account summary = %+v", envelope.Data.AccountSummary)
	}
	gotCapacityIDs := make(map[int]struct{}, len(reader.ids))
	for _, id := range reader.ids {
		gotCapacityIDs[id] = struct{}{}
	}
	if len(reader.ids) != 2 || len(gotCapacityIDs) != 2 ||
		!containsIntKey(gotCapacityIDs, paid.ID) || !containsIntKey(gotCapacityIDs, paidUnauthorized.ID) {
		t.Fatalf("capacity IDs = %v, want paid accounts [%d %d]", reader.ids, paid.ID, paidUnauthorized.ID)
	}
	freeJSON, err := json.Marshal(free.UnauthorizedAccounts)
	if err != nil {
		t.Fatalf("encode free account summary: %v", err)
	}
	if strings.Contains(string(freeJSON), "current_concurrency") || strings.Contains(string(freeJSON), "max_concurrency") || strings.Contains(string(freeJSON), "state_reason") {
		t.Fatalf("free account leaked capacity fields: %s", freeJSON)
	}

	reader.ids = nil
	w = invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/overview",
		`{"platform":"openai","account_type":"oauth","include_free":false,"page":1,"page_size":10}`,
		nil,
		nil,
		handler.GetOverview,
	)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if reader.ids == nil {
		t.Fatal("capacity query was unexpectedly skipped for non-Free accounts")
	}
	var withoutFree struct {
		Data dto.CredentialAccountsOverviewResp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &withoutFree); err != nil {
		t.Fatalf("decode response without Free: %v", err)
	}
	if withoutFree.Data.FreeAccounts != nil {
		t.Fatalf("free summary should be omitted when include_free=false: %+v", withoutFree.Data.FreeAccounts)
	}
}

func containsIntKey(values map[int]struct{}, key int) bool {
	_, ok := values[key]
	return ok
}

func TestCredentialAccountOverviewHTTP401ReasonMatching(t *testing.T) {
	for _, test := range []struct {
		reason string
		want   bool
	}{
		{reason: "HTTP 401: invalid token", want: true},
		{reason: "401 unauthorized", want: true},
		{reason: "status 401 from upstream", want: true},
		{reason: "HTTP 1401: not a status", want: false},
		{reason: "HTTP 403: forbidden", want: false},
	} {
		if got := isHTTP401Reason(test.reason); got != test.want {
			t.Fatalf("isHTTP401Reason(%q) = %v, want %v", test.reason, got, test.want)
		}
	}
}

func TestCredentialOverviewFreePlanClassification(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	if !isFreeCredentialAccountAt(appaccount.Account{
		Type:        "oauth",
		Credentials: map[string]string{"plan_type": "ChatGPT Free"},
	}, now) {
		t.Fatal("explicit free plan was not classified as free")
	}
	if !isFreeCredentialAccountAt(appaccount.Account{
		Type: "oauth",
		Credentials: map[string]string{
			"plan_type":                 "plus",
			"subscription_active_until": "2026-08-29T12:00:00Z",
		},
	}, now) {
		t.Fatal("expired oauth subscription was not classified as free")
	}
	if isFreeCredentialAccountAt(appaccount.Account{
		Type: "oauth",
		Credentials: map[string]string{
			"plan_type":                 "plus",
			"subscription_active_until": "2026-08-31T12:00:00Z",
		},
	}, now) {
		t.Fatal("active oauth subscription was incorrectly classified as free")
	}
	if isFreeCredentialAccountAt(appaccount.Account{
		Type: "oauth",
		Credentials: map[string]string{
			"plan_type":                 "Self_serve_business_prolite",
			"subscription_active_until": "2026-08-29T12:00:00Z",
		},
	}, now) {
		t.Fatal("ProLite was incorrectly downgraded to free by subscription_active_until")
	}
	for _, plan := range []string{"team", "k12"} {
		if isFreeCredentialAccountAt(appaccount.Account{
			Type: "oauth",
			Credentials: map[string]string{
				"plan_type":                 plan,
				"subscription_active_until": "2026-08-29T12:00:00Z",
			},
		}, now) {
			t.Fatalf("%s was incorrectly downgraded to free by subscription_active_until", plan)
		}
	}
}

func TestCredentialUsageEstimateRespMapsAllWindows(t *testing.T) {
	plus5Minutes, plus5Cost := 30.0, 300.0
	pro5Minutes, pro5Cost := 60.0, 600.0
	plus7Minutes, plus7Cost := 90.0, 900.0
	pro7Minutes, pro7Cost := 120.0, 1200.0
	result := credentialUsageEstimateResp(appdashboard.Stats{
		AccountCostPerMinute1M:  10.25,
		AccountCostPerMinute10M: 4.78,
		UsageEstimates: []appdashboard.UsageEstimate{
			{Plan: "plus", Windows: []appdashboard.UsageEstimateWindow{
				{Window: "5h", Status: "ready", RemainingMinutes: &plus5Minutes, RemainingCost: &plus5Cost},
				{Window: "7d", Status: "ready", RemainingMinutes: &plus7Minutes, RemainingCost: &plus7Cost},
			}},
			{Plan: "pro", Windows: []appdashboard.UsageEstimateWindow{
				{Window: "5h", Status: "ready", RemainingMinutes: &pro5Minutes, RemainingCost: &pro5Cost},
				{Window: "7d", Status: "ready", RemainingMinutes: &pro7Minutes, RemainingCost: &pro7Cost},
			}},
		},
	})
	if result.StandardCostPerMinute1M != 10.25 || result.StandardCostPerMinute10M != 4.78 ||
		result.Plus5h.AvailableMinutes == nil || *result.Plus5h.AvailableMinutes != plus5Minutes ||
		result.Pro5h.AvailableStandardCost == nil || *result.Pro5h.AvailableStandardCost != pro5Cost ||
		result.Plus7d.AvailableMinutes == nil || *result.Plus7d.AvailableMinutes != plus7Minutes ||
		result.Pro7d.AvailableStandardCost == nil || *result.Pro7d.AvailableStandardCost != pro7Cost {
		t.Fatalf("mapped usage estimate = %+v", result)
	}
}

func TestCredentialAccountOverviewRequiresPlatform(t *testing.T) {
	handler := NewCredentialAccountHandler(
		appaccount.NewService(nil, nil, nil, nil),
		appdashboard.NewService(nil),
		nil,
	)
	w := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/overview",
		`{"account_type":"oauth"}`,
		nil,
		nil,
		handler.GetOverview,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
}
