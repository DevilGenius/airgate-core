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
		SetCredentials(map[string]string{"access_token": "secret"}).
		SetMaxConcurrency(20).
		SetRateMultiplier(1).
		Save(ctx); err != nil {
		t.Fatalf("create active account: %v", err)
	}
	if _, err := db.Account.Create().
		SetName("disabled-team").
		SetEmail("disabled@example.com").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "secret-2"}).
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
	if envelope.Data.SchemaVersion != 1 || envelope.Data.Traffic.Source != "dashboard_stats" ||
		envelope.Data.Traffic.RPM1M != 0 || envelope.Data.Traffic.TPM1M != 0 ||
		envelope.Data.Traffic.RPM10M != 0 || envelope.Data.Traffic.TPM10M != 0 {
		t.Fatalf("overview metadata = %+v", envelope.Data)
	}
	if envelope.Data.AccountSummary.Total != 2 ||
		envelope.Data.AccountSummary.ByState["active"] != 1 ||
		envelope.Data.AccountSummary.ByState["disabled"] != 1 ||
		envelope.Data.AccountSummary.ConfiguredCapacity != 20 {
		t.Fatalf("account summary = %+v", envelope.Data.AccountSummary)
	}
	if len(envelope.Data.Accounts.List) != 2 || envelope.Data.Accounts.List[0].Email == "" {
		t.Fatalf("account list = %+v", envelope.Data.Accounts)
	}
	if strings.Contains(w.Body.String(), "access_token") || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("overview leaked credentials: %s", w.Body.String())
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
