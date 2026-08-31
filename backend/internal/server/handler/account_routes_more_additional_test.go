package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/accountimportdsl"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestImportAccountsAppliesConfiguredDSL(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_account_import_dsl", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	group, err := db.Group.Create().SetName("Plus Pool").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	for index, priority := range []int{1000, 990} {
		if _, err := db.Account.Create().
			SetName(fmt.Sprintf("occupied-%d", index)).
			SetPlatform("openai").
			SetType("oauth").
			SetCredentials(map[string]string{"access_token": fmt.Sprintf("occupied-%d", index)}).
			SetPriority(priority).
			SetMaxConcurrency(1).
			SetRateMultiplier(1).
			Save(ctx); err != nil {
			t.Fatalf("create occupied account: %v", err)
		}
	}
	if _, err := db.Account.Create().
		SetName("disabled-occupied").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "disabled-occupied"}).
		SetPriority(1000).
		SetState(account.StateDisabled).
		SetMaxConcurrency(1).
		SetRateMultiplier(1).
		Save(ctx); err != nil {
		t.Fatalf("create disabled occupied account: %v", err)
	}
	settingsService := appsettings.NewService(store.NewSettingsStore(db))
	dsl := fmt.Sprintf(`{
  "version": 1,
  "rules": [{
    "name": "OpenAI OAuth Plus",
    "when": [
      {"field":"platform","op":"eq","value":"openai"},
      {"field":"type","op":"eq","value":"oauth"},
      {"field":"credentials.plan_type","op":"in","values":["plus"]}
    ],
    "set": {
      "max_concurrency": 20,
      "model_downgrade_threshold": 0,
      "priority": {"mode":"sequence","initial":1000,"step":-10,"group_size":2},
      "group_ids": [%d]
    }
  }]
}`, group.ID)
	if err := settingsService.Update(ctx, []appsettings.ItemInput{{
		Key: accountimportdsl.SettingKey, Value: dsl, Group: accountimportdsl.SettingGroup,
	}}); err != nil {
		t.Fatalf("save import DSL: %v", err)
	}

	accountService := appaccount.NewService(store.NewAccountStore(db), accountHandlerPluginCatalogStub{}, scheduler.NewConcurrencyManager(nil), nil)
	accountHandler := NewAccountHandler(accountService, scheduler.NewScheduler(db, nil), settingsService)
	importBody := `{"version":2,"accounts":[{"name":"plus-import","platform":"openai","type":"oauth","credentials":{"access_token":"token","plan_type":"Plus"},"priority":1,"max_concurrency":1,"rate_multiplier":1}]}`
	w := invokeHandlerForValidation(http.MethodPost, "/accounts/import", importBody, nil, nil, accountHandler.ImportAccounts)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	imported, err := db.Account.Query().Where(account.NameEQ("plus-import")).WithGroups().Only(ctx)
	if err != nil {
		t.Fatalf("load imported account: %v", err)
	}
	if imported.Priority != 1000 || imported.MaxConcurrency != 20 {
		t.Fatalf("configured priority/capacity = %d/%d", imported.Priority, imported.MaxConcurrency)
	}
	groups, err := imported.QueryGroups().IDs(ctx)
	if err != nil {
		t.Fatalf("load imported groups: %v", err)
	}
	if len(groups) != 1 || groups[0] != group.ID {
		t.Fatalf("configured groups = %v, want [%d]", groups, group.ID)
	}
}

func TestBulkUpdateAccountsCanEditDisabledAccount(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_bulk_update_disabled_account", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	item, err := db.Account.Create().
		SetName("disabled-account").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "disabled-account"}).
		SetState(account.StateDisabled).
		SetPriority(100).
		Save(ctx)
	if err != nil {
		t.Fatalf("create disabled account: %v", err)
	}

	accountService := appaccount.NewService(store.NewAccountStore(db), nil, scheduler.NewConcurrencyManager(nil), nil)
	accountHandler := NewAccountHandler(accountService, nil)
	priority := 321
	w := invokeHandlerForValidation(
		http.MethodPatch,
		"/accounts/bulk",
		fmt.Sprintf(`{"account_ids":[%d],"priority":%d}`, item.ID, priority),
		nil,
		nil,
		accountHandler.BulkUpdateAccounts,
	)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"success":1`) {
		t.Fatalf("bulk update disabled account body = %s", w.Body.String())
	}

	updated, err := db.Account.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("load disabled account: %v", err)
	}
	if updated.State != account.StateDisabled || updated.Priority != priority {
		t.Fatalf("disabled account after bulk update = state %q priority %d", updated.State, updated.Priority)
	}
}

func TestBulkUpdateAccountsCanClearProxy(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_bulk_update_clear_proxy", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	proxy, err := store.NewProxyStore(db).Create(ctx, appproxy.CreateInput{
		Name: "bound-proxy", Protocol: "http", Address: "127.0.0.1", Port: 8080,
	})
	if err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	proxyID := int64(proxy.ID)
	accountStore := store.NewAccountStore(db)
	accountIDs := make([]int, 0, 2)
	for _, name := range []string{"clear-one", "clear-two"} {
		item, createErr := accountStore.Create(ctx, appaccount.CreateInput{
			Name: name, Platform: "openai", Type: "apikey",
			Credentials: map[string]string{"api_key": name}, ProxyID: &proxyID,
		})
		if createErr != nil {
			t.Fatalf("create account %s: %v", name, createErr)
		}
		accountIDs = append(accountIDs, item.ID)
	}

	accountHandler := NewAccountHandler(
		appaccount.NewService(accountStore, nil, scheduler.NewConcurrencyManager(nil), nil),
		nil,
	)
	w := invokeHandlerForValidation(
		http.MethodPost,
		"/accounts/bulk-update",
		fmt.Sprintf(`{"account_ids":[%d,%d],"proxy_id":null}`, accountIDs[0], accountIDs[1]),
		nil,
		nil,
		accountHandler.BulkUpdateAccounts,
	)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"success":2`) {
		t.Fatalf("bulk proxy clear body = %s", w.Body.String())
	}
	for _, id := range accountIDs {
		hasProxy, queryErr := db.Account.Query().Where(account.IDEQ(id)).QueryProxy().Exist(ctx)
		if queryErr != nil {
			t.Fatalf("query proxy for account %d: %v", id, queryErr)
		}
		if hasProxy {
			t.Fatalf("account %d still has a proxy after bulk clear", id)
		}
	}
}

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
