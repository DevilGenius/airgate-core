package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	entuser "github.com/DevilGenius/airgate-core/ent/user"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	appsubscription "github.com/DevilGenius/airgate-core/internal/app/subscription"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestUserRoutesSuccessWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_user_success", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	settingsService := appsettings.NewService(store.NewSettingsStore(db))
	userHandler := NewUserHandler(appuser.NewService(store.NewUserStore(db)), settingsService, nil)

	createBody := `{"email":"user-routes@example.com","password":"password123","username":"initial","role":"user","max_concurrency":3}`
	w := invokeHandlerForValidation(http.MethodPost, "/users", createBody, nil, nil, userHandler.CreateUser)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	userID, err := db.User.Query().Where(entuser.Email("user-routes@example.com")).OnlyID(ctx)
	if err != nil {
		t.Fatalf("query created user: %v", err)
	}

	withUser := func(c *gin.Context) { c.Set("user_id", userID) }
	for _, tt := range []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		want   string
	}{
		{name: "get me", method: http.MethodGet, target: "/users/me", setup: withUser, fn: userHandler.GetMe, want: "user-routes@example.com"},
		{name: "update profile", method: http.MethodPut, target: "/users/me", body: `{"username":"updated"}`, setup: withUser, fn: userHandler.UpdateProfile, want: `"username":"updated"`},
		{name: "balance alert", method: http.MethodPut, target: "/users/me/balance-alert", body: `{"threshold":2.5}`, setup: withUser, fn: userHandler.UpdateBalanceAlert},
		{name: "change password", method: http.MethodPost, target: "/users/me/password", body: `{"old_password":"password123","new_password":"password456"}`, setup: withUser, fn: userHandler.ChangePassword},
		{name: "my balance history", method: http.MethodGet, target: "/users/me/balance-history?page=1&page_size=10", setup: withUser, fn: userHandler.GetMyBalanceHistory},
		{name: "list users", method: http.MethodGet, target: "/users?page=1&page_size=10&role=user&status=active", fn: userHandler.ListUsers, want: `"total":1`},
		{name: "update user", method: http.MethodPut, target: "/users/1", params: gin.Params{{Key: "id", Value: fmt.Sprint(userID)}}, body: `{"username":"admin-updated","status":"active","group_rates":{}}`, fn: userHandler.UpdateUser, want: "admin-updated"},
		{name: "adjust balance", method: http.MethodPost, target: "/users/1/balance", params: gin.Params{{Key: "id", Value: fmt.Sprint(userID)}}, body: `{"action":"add","amount":5,"remark":"test credit"}`, fn: userHandler.AdjustBalance, want: `"balance":5`},
		{name: "user balance history", method: http.MethodGet, target: "/users/1/balance-history?page=1&page_size=10", params: gin.Params{{Key: "id", Value: fmt.Sprint(userID)}}, fn: userHandler.GetUserBalanceHistory, want: "test credit"},
		{name: "toggle user", method: http.MethodPatch, target: "/users/1/toggle", params: gin.Params{{Key: "id", Value: fmt.Sprint(userID)}}, fn: userHandler.ToggleUserStatus, want: `"status":"disabled"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
			if tt.want != "" && !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.want)
			}
		})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("delete-me"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash delete user password: %v", err)
	}
	deleteID, err := db.User.Create().
		SetEmail("delete-user@example.com").
		SetPasswordHash(string(hash)).
		SetUsername("delete").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create delete user: %v", err)
	}
	w = invokeHandlerForValidation(http.MethodDelete, "/users/delete", "", gin.Params{{Key: "id", Value: fmt.Sprint(deleteID.ID)}}, nil, userHandler.DeleteUser)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
}

func TestAPIKeyAndSubscriptionRoutesSuccessWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_key_subscription_success", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userEnt, err := db.User.Create().
		SetEmail("keys@example.com").
		SetPasswordHash(string(hash)).
		SetUsername("keys").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	groupEnt, err := db.Group.Create().
		SetName("API Group").
		SetPlatform("openai").
		SetSubscriptionType("standard").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	apiKeyHandler := NewAPIKeyHandler(appapikey.NewService(store.NewAPIKeyStore(db), strings.Repeat("b", 64)), nil)
	withUser := func(c *gin.Context) { c.Set("user_id", userEnt.ID) }
	createKey := fmt.Sprintf(`{"name":"client","group_id":%d,"quota_usd":12,"sell_rate":1.1,"max_concurrency":2,"balance_alert_enabled":true,"balance_alert_email":"keys@example.com","balance_alert_threshold":3}`, groupEnt.ID)
	w := invokeHandlerForValidation(http.MethodPost, "/api-keys", createKey, nil, withUser, apiKeyHandler.CreateKey)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"key":"sk-`) {
		t.Fatalf("create key body = %s", w.Body.String())
	}
	keyID, err := db.APIKey.Query().OnlyID(ctx)
	if err != nil {
		t.Fatalf("query api key: %v", err)
	}

	keyRoutes := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		want   string
	}{
		{name: "list owned", method: http.MethodGet, target: "/api-keys?page=1&page_size=10&include_usage=true", setup: withUser, fn: apiKeyHandler.ListKeys, want: `"total":1`},
		{name: "admin list", method: http.MethodGet, target: "/admin/api-keys?page=1&page_size=10&include_usage=true", fn: apiKeyHandler.AdminListKeys, want: `"total":1`},
		{name: "reveal", method: http.MethodGet, target: "/api-keys/1/reveal", params: gin.Params{{Key: "id", Value: fmt.Sprint(keyID)}}, setup: withUser, fn: apiKeyHandler.RevealKey, want: `"key":"sk-`},
		{name: "update owned", method: http.MethodPut, target: "/api-keys/1", params: gin.Params{{Key: "id", Value: fmt.Sprint(keyID)}}, setup: withUser, body: `{"name":"client-updated","status":"disabled","quota_usd":20}`, fn: apiKeyHandler.UpdateKey, want: "client-updated"},
		{name: "admin update", method: http.MethodPut, target: "/admin/api-keys/1", params: gin.Params{{Key: "id", Value: fmt.Sprint(keyID)}}, body: `{"status":"active","max_concurrency":4}`, fn: apiKeyHandler.AdminUpdateKey, want: `"status":"active"`},
		{name: "admin reset", method: http.MethodPost, target: "/admin/api-keys/1/reset", params: gin.Params{{Key: "id", Value: fmt.Sprint(keyID)}}, fn: apiKeyHandler.AdminResetKeyUsage, want: `"used_quota":0`},
	}
	for _, tt := range keyRoutes {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
			if tt.want != "" && !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.want)
			}
		})
	}

	userHandler := NewUserHandler(appuser.NewService(store.NewUserStore(db)), nil, nil)
	w = invokeHandlerForValidation(http.MethodGet, "/users/1/api-keys?page=1&page_size=10", "", gin.Params{{Key: "id", Value: fmt.Sprint(userEnt.ID)}}, nil, userHandler.AdminListUserKeys)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	subscriptionHandler := NewSubscriptionHandler(appsubscription.NewService(store.NewSubscriptionStore(db)))
	expires := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	assign := fmt.Sprintf(`{"user_id":%d,"group_id":%d,"expires_at":%q}`, userEnt.ID, groupEnt.ID, expires)
	w = invokeHandlerForValidation(http.MethodPost, "/admin/subscriptions/assign", assign, nil, nil, subscriptionHandler.AdminAssign)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	subID, err := db.UserSubscription.Query().OnlyID(ctx)
	if err != nil {
		t.Fatalf("query subscription: %v", err)
	}

	subscriptionRoutes := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		want   string
	}{
		{name: "user list", method: http.MethodGet, target: "/subscriptions?page=1&page_size=10", setup: withUser, fn: subscriptionHandler.UserSubscriptions, want: `"total":1`},
		{name: "active", method: http.MethodGet, target: "/subscriptions/active", setup: withUser, fn: subscriptionHandler.ActiveSubscriptions, want: "API Group"},
		{name: "progress", method: http.MethodGet, target: "/subscriptions/progress", setup: withUser, fn: subscriptionHandler.SubscriptionProgress},
		{name: "admin list", method: http.MethodGet, target: "/admin/subscriptions?page=1&page_size=10&status=active", fn: subscriptionHandler.AdminListSubscriptions, want: `"total":1`},
		{name: "bulk assign", method: http.MethodPost, target: "/admin/subscriptions/bulk", body: fmt.Sprintf(`{"user_ids":[%d],"group_id":%d,"expires_at":%q}`, userEnt.ID, groupEnt.ID, expires), fn: subscriptionHandler.AdminBulkAssign, want: `"created":1`},
		{name: "adjust", method: http.MethodPut, target: "/admin/subscriptions/1", params: gin.Params{{Key: "id", Value: fmt.Sprint(subID)}}, body: `{"status":"suspended"}`, fn: subscriptionHandler.AdminAdjust, want: `"status":"suspended"`},
	}
	for _, tt := range subscriptionRoutes {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
			if tt.want != "" && !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.want)
			}
		})
	}

	w = invokeHandlerForValidation(http.MethodDelete, "/api-keys/1", "", gin.Params{{Key: "id", Value: fmt.Sprint(keyID)}}, withUser, apiKeyHandler.DeleteKey)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
}

func TestUserGroupRateOverrideValidationAndErrors(t *testing.T) {
	emptyHandler := NewUserHandler(nil, nil, nil)
	for _, tt := range []struct {
		name   string
		method string
		body   string
		params gin.Params
		fn     func(*gin.Context)
	}{
		{name: "list bad group", method: http.MethodGet, params: gin.Params{{Key: "id", Value: "bad"}}, fn: emptyHandler.ListGroupRateOverrides},
		{name: "set bad group", method: http.MethodPut, body: `{"rate":1.2}`, params: gin.Params{{Key: "id", Value: "0"}, {Key: "userId", Value: "1"}}, fn: emptyHandler.SetGroupRateOverride},
		{name: "set bad user", method: http.MethodPut, body: `{"rate":1.2}`, params: gin.Params{{Key: "id", Value: "1"}, {Key: "userId", Value: "bad"}}, fn: emptyHandler.SetGroupRateOverride},
		{name: "set bad json", method: http.MethodPut, body: `{`, params: gin.Params{{Key: "id", Value: "1"}, {Key: "userId", Value: "1"}}, fn: emptyHandler.SetGroupRateOverride},
		{name: "set missing rate", method: http.MethodPut, body: `{}`, params: gin.Params{{Key: "id", Value: "1"}, {Key: "userId", Value: "1"}}, fn: emptyHandler.SetGroupRateOverride},
		{name: "delete bad group", method: http.MethodDelete, params: gin.Params{{Key: "id", Value: "bad"}, {Key: "userId", Value: "1"}}, fn: emptyHandler.DeleteGroupRateOverride},
		{name: "delete bad user", method: http.MethodDelete, params: gin.Params{{Key: "id", Value: "1"}, {Key: "userId", Value: "0"}}, fn: emptyHandler.DeleteGroupRateOverride},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, "/group-rate", tt.body, tt.params, nil, tt.fn)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s status = %d, body=%s", tt.name, w.Code, w.Body.String())
			}
		})
	}

	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_user_group_rate_errors", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()
	userEnt, err := db.User.Create().
		SetEmail("group-rate@example.com").
		SetPasswordHash("hash").
		SetUsername("group-rate").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	handler := NewUserHandler(appuser.NewService(store.NewUserStore(db)), nil, nil)

	w := invokeHandlerForValidation(http.MethodPut, "/group-rate", `{"rate":0}`, gin.Params{{Key: "id", Value: "1"}, {Key: "userId", Value: fmt.Sprint(userEnt.ID)}}, nil, handler.SetGroupRateOverride)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid rate status = %d, body=%s", w.Code, w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodDelete, "/group-rate", "", gin.Params{{Key: "id", Value: "1"}, {Key: "userId", Value: "9999"}}, nil, handler.DeleteGroupRateOverride)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing user status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestUserAPIKeyAndSubscriptionRouteServiceErrorsWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_user_key_subscription_errors", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userEnt, err := db.User.Create().
		SetEmail("route-errors@example.com").
		SetPasswordHash(string(hash)).
		SetUsername("route-errors").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	adminEnt, err := db.User.Create().
		SetEmail("admin-route-errors@example.com").
		SetPasswordHash(string(hash)).
		SetUsername("admin-route-errors").
		SetRole("admin").
		Save(ctx)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	groupEnt, err := db.Group.Create().
		SetName("Route Error Group").
		SetPlatform("openai").
		SetSubscriptionType("standard").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	userHandler := NewUserHandler(appuser.NewService(store.NewUserStore(db)), nil, nil)
	withMissingUser := func(c *gin.Context) { c.Set("user_id", 9999) }
	withUser := func(c *gin.Context) { c.Set("user_id", userEnt.ID) }

	userCases := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		status int
	}{
		{name: "get me missing", method: http.MethodGet, target: "/users/me", setup: withMissingUser, fn: userHandler.GetMe, status: http.StatusNotFound},
		{name: "update profile missing", method: http.MethodPut, target: "/users/me", body: `{"username":"x"}`, setup: withMissingUser, fn: userHandler.UpdateProfile, status: http.StatusNotFound},
		{name: "balance alert missing", method: http.MethodPut, target: "/users/me/balance-alert", body: `{"threshold":1}`, setup: withMissingUser, fn: userHandler.UpdateBalanceAlert, status: http.StatusInternalServerError},
		{name: "change password mismatch", method: http.MethodPost, target: "/users/me/password", body: `{"old_password":"wrong","new_password":"password456"}`, setup: withUser, fn: userHandler.ChangePassword, status: http.StatusBadRequest},
		{name: "create duplicate email", method: http.MethodPost, target: "/users", body: `{"email":"route-errors@example.com","password":"password123","role":"user"}`, fn: userHandler.CreateUser, status: http.StatusBadRequest},
		{name: "update missing user", method: http.MethodPut, target: "/users/9999", params: gin.Params{{Key: "id", Value: "9999"}}, body: `{"username":"missing"}`, fn: userHandler.UpdateUser, status: http.StatusNotFound},
		{name: "update invalid group rate", method: http.MethodPut, target: "/users/1", params: gin.Params{{Key: "id", Value: fmt.Sprint(userEnt.ID)}}, body: `{"group_rates":{"1":0}}`, fn: userHandler.UpdateUser, status: http.StatusBadRequest},
		{name: "adjust invalid action", method: http.MethodPost, target: "/users/1/balance", params: gin.Params{{Key: "id", Value: fmt.Sprint(userEnt.ID)}}, body: `{"action":"bad","amount":1}`, fn: userHandler.AdjustBalance, status: http.StatusBadRequest},
		{name: "adjust insufficient balance", method: http.MethodPost, target: "/users/1/balance", params: gin.Params{{Key: "id", Value: fmt.Sprint(userEnt.ID)}}, body: `{"action":"subtract","amount":1}`, fn: userHandler.AdjustBalance, status: http.StatusBadRequest},
		{name: "delete admin forbidden", method: http.MethodDelete, target: "/users/admin", params: gin.Params{{Key: "id", Value: fmt.Sprint(adminEnt.ID)}}, fn: userHandler.DeleteUser, status: http.StatusBadRequest},
		{name: "delete missing user", method: http.MethodDelete, target: "/users/9999", params: gin.Params{{Key: "id", Value: "9999"}}, fn: userHandler.DeleteUser, status: http.StatusNotFound},
		{name: "toggle missing user", method: http.MethodPatch, target: "/users/9999/toggle", params: gin.Params{{Key: "id", Value: "9999"}}, fn: userHandler.ToggleUserStatus, status: http.StatusNotFound},
	}
	for _, tt := range userCases {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	apiKeyHandler := NewAPIKeyHandler(appapikey.NewService(store.NewAPIKeyStore(db), strings.Repeat("c", 64)), nil)
	apiKeyCases := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
		status int
	}{
		{name: "create missing group", method: http.MethodPost, target: "/api-keys", body: `{"name":"missing","group_id":9999}`, setup: withUser, fn: apiKeyHandler.CreateKey, status: http.StatusNotFound},
		{name: "create invalid expires", method: http.MethodPost, target: "/api-keys", body: fmt.Sprintf(`{"name":"expired","group_id":%d,"expires_at":"bad-time"}`, groupEnt.ID), setup: withUser, fn: apiKeyHandler.CreateKey, status: http.StatusBadRequest},
		{name: "update missing owned", method: http.MethodPut, target: "/api-keys/9999", params: gin.Params{{Key: "id", Value: "9999"}}, body: `{"name":"missing"}`, setup: withUser, fn: apiKeyHandler.UpdateKey, status: http.StatusNotFound},
		{name: "delete missing owned", method: http.MethodDelete, target: "/api-keys/9999", params: gin.Params{{Key: "id", Value: "9999"}}, setup: withUser, fn: apiKeyHandler.DeleteKey, status: http.StatusNotFound},
		{name: "admin update missing", method: http.MethodPut, target: "/admin/api-keys/9999", params: gin.Params{{Key: "id", Value: "9999"}}, body: `{"name":"missing"}`, fn: apiKeyHandler.AdminUpdateKey, status: http.StatusNotFound},
		{name: "admin reset missing", method: http.MethodPost, target: "/admin/api-keys/9999/reset", params: gin.Params{{Key: "id", Value: "9999"}}, fn: apiKeyHandler.AdminResetKeyUsage, status: http.StatusNotFound},
		{name: "reveal missing", method: http.MethodGet, target: "/api-keys/9999/reveal", params: gin.Params{{Key: "id", Value: "9999"}}, setup: withUser, fn: apiKeyHandler.RevealKey, status: http.StatusNotFound},
	}
	for _, tt := range apiKeyCases {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, tt.setup, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	subscriptionHandler := NewSubscriptionHandler(appsubscription.NewService(store.NewSubscriptionStore(db)))
	subscriptionCases := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		fn     func(*gin.Context)
		status int
	}{
		{name: "assign invalid expires", method: http.MethodPost, target: "/admin/subscriptions/assign", body: fmt.Sprintf(`{"user_id":%d,"group_id":%d,"expires_at":"bad-time"}`, userEnt.ID, groupEnt.ID), fn: subscriptionHandler.AdminAssign, status: http.StatusBadRequest},
		{name: "bulk assign invalid expires", method: http.MethodPost, target: "/admin/subscriptions/bulk", body: fmt.Sprintf(`{"user_ids":[%d],"group_id":%d,"expires_at":"bad-time"}`, userEnt.ID, groupEnt.ID), fn: subscriptionHandler.AdminBulkAssign, status: http.StatusBadRequest},
		{name: "adjust missing", method: http.MethodPut, target: "/admin/subscriptions/9999", params: gin.Params{{Key: "id", Value: "9999"}}, body: `{"status":"active"}`, fn: subscriptionHandler.AdminAdjust, status: http.StatusNotFound},
		{name: "adjust invalid expires", method: http.MethodPut, target: "/admin/subscriptions/9999", params: gin.Params{{Key: "id", Value: "9999"}}, body: `{"expires_at":"bad-time"}`, fn: subscriptionHandler.AdminAdjust, status: http.StatusBadRequest},
	}
	for _, tt := range subscriptionCases {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, nil, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestListStyleRoutesPropagateRepositoryErrorsWithClosedSQLite(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "handler_list_route_repo_errors", schema.WithGlobalUniqueID(false))
	userHandler := NewUserHandler(appuser.NewService(store.NewUserStore(db)), nil, nil)
	apiKeyHandler := NewAPIKeyHandler(appapikey.NewService(store.NewAPIKeyStore(db), strings.Repeat("d", 64)), nil)
	subscriptionHandler := NewSubscriptionHandler(appsubscription.NewService(store.NewSubscriptionStore(db)))
	if err := db.Close(); err != nil {
		t.Fatalf("close db before route calls: %v", err)
	}

	withUser := func(c *gin.Context) { c.Set("user_id", 7) }
	tests := []struct {
		name   string
		target string
		params gin.Params
		setup  func(*gin.Context)
		fn     func(*gin.Context)
	}{
		{name: "my balance history", target: "/users/me/balance-history?page=1&page_size=10", setup: withUser, fn: userHandler.GetMyBalanceHistory},
		{name: "list users", target: "/users?page=1&page_size=10", fn: userHandler.ListUsers},
		{name: "user balance history", target: "/users/7/balance-history?page=1&page_size=10", params: gin.Params{{Key: "id", Value: "7"}}, fn: userHandler.GetUserBalanceHistory},
		{name: "admin list user keys", target: "/users/7/api-keys?page=1&page_size=10", params: gin.Params{{Key: "id", Value: "7"}}, fn: userHandler.AdminListUserKeys},
		{name: "list owned api keys", target: "/api-keys?page=1&page_size=10", setup: withUser, fn: apiKeyHandler.ListKeys},
		{name: "admin list api keys", target: "/admin/api-keys?page=1&page_size=10", fn: apiKeyHandler.AdminListKeys},
		{name: "user subscriptions", target: "/subscriptions?page=1&page_size=10", setup: withUser, fn: subscriptionHandler.UserSubscriptions},
		{name: "active subscriptions", target: "/subscriptions/active", setup: withUser, fn: subscriptionHandler.ActiveSubscriptions},
		{name: "admin subscriptions", target: "/admin/subscriptions?page=1&page_size=10", fn: subscriptionHandler.AdminListSubscriptions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(http.MethodGet, tt.target, "", tt.params, tt.setup, tt.fn)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
			}
		})
	}
}
