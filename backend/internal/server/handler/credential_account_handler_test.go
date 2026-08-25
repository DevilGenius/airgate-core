package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent/account"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
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
		SetCredentials(map[string]string{"access_token": "secret", "plan_type": "ChatGPT Plus"}).
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

	accountService := appaccount.NewService(
		store.NewAccountStore(db),
		nil,
		scheduler.NewConcurrencyManager(nil),
		nil,
	)
	dashboardService := appdashboard.NewService(store.NewDashboardStore(db, nil))
	handler := NewCredentialAccountHandler(accountService, dashboardService, nil)
	w := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/overview",
		`{"platform":"openai","account_type":"oauth","page":1,"page_size":10}`,
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
	if envelope.Data.SchemaVersion != 2 || envelope.Data.Traffic.Source != "dashboard_stats" ||
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
	if envelope.Data.AccountSummary.Total != 2 ||
		envelope.Data.AccountSummary.ByState["active"] != 1 ||
		envelope.Data.AccountSummary.ByState["disabled"] != 1 ||
		envelope.Data.AccountSummary.ConfiguredCapacity != 20 {
		t.Fatalf("account summary = %+v", envelope.Data.AccountSummary)
	}
	if len(envelope.Data.Accounts.List) != 2 {
		t.Fatalf("account list = %+v", envelope.Data.Accounts)
	}
	byName := make(map[string]dto.CredentialAccountResp, len(envelope.Data.Accounts.List))
	for _, item := range envelope.Data.Accounts.List {
		byName[item.Name] = item
	}
	if byName["active-team"].Email == "" || byName["active-team"].PlanType != "plus" ||
		byName["active-team"].Priority != 120 || byName["disabled-team"].PlanType != "k12" || byName["disabled-team"].Priority != 50 {
		t.Fatalf("account priority mapping = %+v", byName)
	}
	if strings.Contains(w.Body.String(), "access_token") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("overview leaked credentials: %s", w.Body.String())
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
