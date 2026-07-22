package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestAccountAuxiliaryRoutesSuccessWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_account_auxiliary_routes", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	plugins := accountHandlerPluginCatalogStub{
		models: []sdk.ModelInfo{{ID: "model-test", Name: "Model Test"}},
		accountTypes: []sdk.AccountType{{
			Key:         "apikey",
			Label:       "API Key",
			Description: "API key account",
			Fields:      []sdk.CredentialField{{Key: "token", Label: "Token", Type: "password", Required: true}},
		}},
	}
	accountService := appaccount.NewService(store.NewAccountStore(db), plugins, scheduler.NewConcurrencyManager(nil), nil)
	accountHandler := NewAccountHandler(accountService, scheduler.NewScheduler(db, nil))

	createBody := `{"name":"primary","platform":"custom","type":"apikey","credentials":{"token":"secret"},"priority":2,"max_concurrency":5,"rate_multiplier":1.2,"upstream_is_pool":true,"extra":{"region":"us"}}`
	w := invokeHandlerForValidation(http.MethodPost, "/accounts", createBody, nil, nil, accountHandler.CreateAccount)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"name":"primary"`) || !strings.Contains(w.Body.String(), `"rate_multiplier":1.2`) {
		t.Fatalf("create account body = %s", w.Body.String())
	}
	accountID, err := db.Account.Query().OnlyID(ctx)
	if err != nil {
		t.Fatalf("query account id: %v", err)
	}
	accountIDString := intToString(accountID)
	accountParams := gin.Params{{Key: "id", Value: accountIDString}}

	routes := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		fn     func(*gin.Context)
		want   string
	}{
		{name: "list", method: http.MethodGet, target: "/accounts?page=1&page_size=10&platform=custom&sort_by=priority&sort_dir=asc", fn: accountHandler.ListAccounts, want: `"total":1`},
		{name: "export", method: http.MethodGet, target: "/accounts/export?platform=custom", fn: accountHandler.ExportAccounts, want: `"version":2`},
		{name: "update", method: http.MethodPut, target: "/accounts/" + accountIDString, params: accountParams, body: `{"name":"primary-updated","priority":99996,"max_concurrency":8,"rate_multiplier":1.4,"extra":{"region":"eu"}}`, fn: accountHandler.UpdateAccount, want: `"name":"primary-updated"`},
		{name: "models", method: http.MethodGet, target: "/accounts/" + accountIDString + "/models", params: accountParams, fn: accountHandler.GetAccountModels, want: `"id":"model-test"`},
		{name: "usage", method: http.MethodGet, target: "/accounts/usage?platform=custom&ids=" + accountIDString, fn: accountHandler.GetAccountUsage, want: `"refreshing":false`},
		{name: "capacity", method: http.MethodGet, target: "/accounts/capacity?ids=" + accountIDString + "," + accountIDString, fn: accountHandler.GetAccountCapacity, want: accountIDString},
		{name: "single usage", method: http.MethodGet, target: "/accounts/" + accountIDString + "/usage", params: accountParams, fn: accountHandler.GetSingleAccountUsage, want: `"data":{`},
		{name: "schema", method: http.MethodGet, target: "/accounts/schema/custom", params: gin.Params{{Key: "platform", Value: "custom"}}, fn: accountHandler.GetCredentialsSchema, want: `"key":"token"`},
		{name: "stats", method: http.MethodGet, target: "/accounts/" + accountIDString + "/stats?tz=UTC", params: accountParams, fn: accountHandler.GetAccountStats, want: `"account_id":` + accountIDString},
		{name: "bulk update", method: http.MethodPatch, target: "/accounts/bulk", body: fmt.Sprintf(`{"account_ids":[%d],"priority_offset":3,"max_concurrency":9}`, accountID), fn: accountHandler.BulkUpdateAccounts, want: `"success":1`},
		{name: "bulk clear cooldowns", method: http.MethodPost, target: "/accounts/cooldowns", body: fmt.Sprintf(`{"account_ids":[%d]}`, accountID), fn: accountHandler.BulkClearFamilyCooldowns, want: `"success":1`},
	}
	for _, tt := range routes {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, nil, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
			if tt.want != "" && !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.want)
			}
		})
	}
	updatedPriority, err := db.Account.Query().Only(ctx)
	if err != nil {
		t.Fatalf("query updated account: %v", err)
	}
	if updatedPriority.Priority != 99999 {
		t.Fatalf("bulk priority offset result = %d, want 99999", updatedPriority.Priority)
	}

	w = invokeHandlerForValidation(http.MethodPost, "/accounts/"+accountIDString+"/refresh-token", "", accountParams, nil, accountHandler.RefreshToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("refresh token status = %d, body=%s", w.Code, w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodPost, "/accounts/"+accountIDString+"/test", `{"model_id":"model-test"}`, accountParams, nil, accountHandler.TestAccount)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("test account status = %d, body=%s", w.Code, w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodPost, "/accounts/bulk-refresh-token", fmt.Sprintf(`{"account_ids":[%d]}`, accountID), nil, nil, accountHandler.BulkRefreshToken)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"failed":1`) {
		t.Fatalf("bulk refresh token body = status %d %s", w.Code, w.Body.String())
	}

	importBody := `{"version":1,"accounts":[{"name":"imported","platform":"custom","type":"oauth","credentials":{"token":"imported","email":" Legacy.Import@Example.COM "},"rate_multiplier":1.1}]}`
	w = invokeHandlerForValidation(http.MethodPost, "/accounts/import", importBody, nil, nil, accountHandler.ImportAccounts)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"imported":1`) {
		t.Fatalf("import body = %s", w.Body.String())
	}

	allIDs, err := db.Account.Query().IDs(ctx)
	if err != nil {
		t.Fatalf("query account ids: %v", err)
	}
	var importedID int
	for _, id := range allIDs {
		if id != accountID {
			importedID = id
			break
		}
	}
	if importedID == 0 {
		t.Fatalf("imported account id not found in %v", allIDs)
	}
	importedAccount, err := db.Account.Get(ctx, importedID)
	if err != nil {
		t.Fatalf("load imported account: %v", err)
	}
	if importedAccount.Email == nil || *importedAccount.Email != "legacy.import@example.com" || importedAccount.Credentials["email"] != "legacy.import@example.com" {
		t.Fatalf("legacy import identity = %+v", importedAccount)
	}
	w = invokeHandlerForValidation(http.MethodDelete, "/accounts/bulk", fmt.Sprintf(`{"account_ids":[%d]}`, importedID), nil, nil, accountHandler.BulkDeleteAccounts)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"success":1`) {
		t.Fatalf("bulk delete body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodDelete, "/accounts/"+accountIDString, "", accountParams, nil, accountHandler.DeleteAccount)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	deletedAccounts, err := db.Account.Query().All(ctx)
	if err != nil {
		t.Fatalf("query soft-deleted accounts: %v", err)
	}
	if len(deletedAccounts) != 2 {
		t.Fatalf("account rows after delete = %d, want 2", len(deletedAccounts))
	}
	for _, item := range deletedAccounts {
		if item.DeletedAt == nil || len(item.Credentials) == 0 {
			t.Fatalf("soft-deleted account lost data: %+v", item)
		}
	}
}

type accountHandlerPluginCatalogStub struct {
	models            []sdk.ModelInfo
	accountTypes      []sdk.AccountType
	credentialFields  []sdk.CredentialField
	allPluginMetadata []plugin.PluginMeta
}

func (s accountHandlerPluginCatalogStub) GetPluginByPlatform(string) *plugin.PluginInstance {
	return nil
}

func (s accountHandlerPluginCatalogStub) GetModels(string) []sdk.ModelInfo {
	return append([]sdk.ModelInfo(nil), s.models...)
}

func (s accountHandlerPluginCatalogStub) GetAccountTypes(string) []sdk.AccountType {
	return append([]sdk.AccountType(nil), s.accountTypes...)
}

func (s accountHandlerPluginCatalogStub) GetCredentialFields(string) []sdk.CredentialField {
	return append([]sdk.CredentialField(nil), s.credentialFields...)
}

func (s accountHandlerPluginCatalogStub) GetAllPluginMeta() []plugin.PluginMeta {
	return append([]plugin.PluginMeta(nil), s.allPluginMetadata...)
}
