package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	entuser "github.com/DevilGenius/airgate-core/ent/user"
	appauth "github.com/DevilGenius/airgate-core/internal/app/auth"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	coreauth "github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/infra/mailer"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestAuthRegisterLoginAndRefreshRoutesWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_auth_routes", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	jwtMgr := coreauth.NewJWTManager("test-secret", 1)
	settingsService := appsettings.NewService(store.NewSettingsStore(db))
	authService := appauth.NewService(store.NewAuthStore(db), jwtMgr)
	codeStore := mailer.NewVerifyCodeStore()
	handler := NewAuthHandler(authService, settingsService, nil, codeStore, db, jwtMgr)

	if err := settingsService.Update(ctx, []appsettings.ItemInput{{Key: "registration_enabled", Value: "false", Group: "registration"}}); err != nil {
		t.Fatalf("disable registration: %v", err)
	}
	w := invokeHandlerForValidation(http.MethodPost, "/register", `{"email":"closed@example.com","password":"password123"}`, nil, nil, handler.Register)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled registration status = %d, body=%s", w.Code, w.Body.String())
	}

	if err := settingsService.Update(ctx, []appsettings.ItemInput{
		{Key: "registration_enabled", Value: "true", Group: "registration"},
		{Key: "email_verify_enabled", Value: "false", Group: "registration"},
		{Key: "default_balance", Value: "12.5", Group: "defaults"},
		{Key: "default_concurrency", Value: "7", Group: "defaults"},
	}); err != nil {
		t.Fatalf("enable registration: %v", err)
	}
	registerBody := `{"email":"open@example.com","password":"password123","username":"open"}`
	w = invokeHandlerForValidation(http.MethodPost, "/register", registerBody, nil, nil, handler.Register)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"email":"open@example.com"`) || !strings.Contains(w.Body.String(), `"token":"`) {
		t.Fatalf("register body = %s", w.Body.String())
	}
	userID, err := db.User.Query().Where(entuser.Email("open@example.com")).OnlyID(ctx)
	if err != nil {
		t.Fatalf("query registered user: %v", err)
	}
	created, err := db.User.Get(ctx, userID)
	if err != nil {
		t.Fatalf("get registered user: %v", err)
	}
	if created.Balance != 12.5 || created.MaxConcurrency != 7 {
		t.Fatalf("registered defaults balance=%v concurrency=%v", created.Balance, created.MaxConcurrency)
	}

	w = invokeHandlerForValidation(http.MethodPost, "/login", `{"email":"open@example.com","password":"password123"}`, nil, nil, handler.Login)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	for _, tt := range []struct {
		name string
		body string
		want int
	}{
		{name: "bad json", body: `{`, want: http.StatusBadRequest},
		{name: "missing user", body: `{"email":"missing@example.com","password":"password123"}`, want: http.StatusUnauthorized},
		{name: "wrong password", body: `{"email":"open@example.com","password":"wrong-password"}`, want: http.StatusUnauthorized},
	} {
		t.Run("login "+tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(http.MethodPost, "/login", tt.body, nil, nil, handler.Login)
			if w.Code != tt.want {
				t.Fatalf("Login %s status = %d, want %d; body=%s", tt.name, w.Code, tt.want, w.Body.String())
			}
		})
	}

	token, err := jwtMgr.GenerateToken(userID, "user", "open@example.com")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	w = invokeHandlerForValidation(http.MethodPost, "/refresh", "", nil, nil, handler.RefreshToken)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh missing token status = %d, body=%s", w.Code, w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodPost, "/refresh", "", nil, func(c *gin.Context) {
		c.Request.Header.Set("Authorization", "Bearer invalid-token")
	}, handler.RefreshToken)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh invalid token status = %d, body=%s", w.Code, w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodPost, "/refresh", "", nil, func(c *gin.Context) {
		c.Request.Header.Set("Authorization", "Bearer "+token)
	}, handler.RefreshToken)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	if err := db.User.UpdateOneID(userID).SetStatus(entuser.StatusDisabled).Exec(ctx); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	w = invokeHandlerForValidation(http.MethodPost, "/login", `{"email":"open@example.com","password":"password123"}`, nil, nil, handler.Login)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled login status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := db.User.UpdateOneID(userID).SetStatus(entuser.StatusActive).Exec(ctx); err != nil {
		t.Fatalf("reactivate user: %v", err)
	}

	w = invokeHandlerForValidation(http.MethodPost, "/register", registerBody, nil, nil, handler.Register)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("duplicate register status = %d, body=%s", w.Code, w.Body.String())
	}

	if err := settingsService.Update(ctx, []appsettings.ItemInput{{Key: "email_verify_enabled", Value: "true", Group: "registration"}}); err != nil {
		t.Fatalf("enable email verify: %v", err)
	}
	w = invokeHandlerForValidation(http.MethodPost, "/register", `{"email":"need-code@example.com","password":"password123"}`, nil, nil, handler.Register)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing verify code status = %d, body=%s", w.Code, w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodPost, "/register", `{"email":"bad-code@example.com","password":"password123","verify_code":"000000"}`, nil, nil, handler.Register)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid verify code status = %d, body=%s", w.Code, w.Body.String())
	}

	verifyOnlyCode := codeStore.Generate("check-code@example.com")
	w = invokeHandlerForValidation(http.MethodPost, "/verify-code", `{"email":"check-code@example.com","code":"bad"}`, nil, nil, handler.VerifyCode)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode invalid status = %d, body=%s", w.Code, w.Body.String())
	}
	w = invokeHandlerForValidation(http.MethodPost, "/verify-code", `{"email":"check-code@example.com","code":"`+verifyOnlyCode+`"}`, nil, nil, handler.VerifyCode)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	code := codeStore.Generate("verified@example.com")
	verifiedBody := `{"email":"verified@example.com","password":"password123","username":"verified","verify_code":"` + code + `"}`
	w = invokeHandlerForValidation(http.MethodPost, "/register", verifiedBody, nil, nil, handler.Register)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
}

func TestAuthSettingHelpersAndLogOnlyHelpers(t *testing.T) {
	errSettings := errors.New("settings down")
	errorSettings := appsettings.NewService(handlerSettingsRepoStub{err: errSettings})
	handler := NewAuthHandler(nil, errorSettings, nil, mailer.NewVerifyCodeStore(), nil, nil)
	c, _ := newHandlerTestContext()

	if !handler.isRegistrationEnabled(c) {
		t.Fatal("registration should default enabled when settings fail")
	}
	if handler.isEmailVerifyEnabled(c) {
		t.Fatal("email verify should default disabled when settings fail")
	}
	balance, concurrency := handler.getNewUserDefaults(c)
	if balance != 0 || concurrency != 5 {
		t.Fatalf("defaults on settings error = balance %v concurrency %v", balance, concurrency)
	}
	if _, err := handler.buildMailer(c); err == nil {
		t.Fatal("buildMailer should return settings error")
	}

	mailerSettings := appsettings.NewService(handlerSettingsRepoStub{items: []appsettings.Setting{
		{Key: "smtp_host", Value: "localhost", Group: "smtp"},
		{Key: "smtp_from_email", Value: "noreply@example.com", Group: "smtp"},
	}})
	handler = NewAuthHandler(nil, mailerSettings, nil, mailer.NewVerifyCodeStore(), nil, nil)
	if m, err := handler.buildMailer(c); err != nil || m == nil {
		t.Fatalf("buildMailer success = %v, %v", m, err)
	}

	handleUsageError("usage helper", errors.New("usage failed"))
	NewDashboardHandler(nil).handleError("dashboard helper", errors.New("dashboard failed"))
}

type handlerSettingsRepoStub struct {
	items []appsettings.Setting
	err   error
}

func (s handlerSettingsRepoStub) List(_ context.Context, group string) ([]appsettings.Setting, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make([]appsettings.Setting, 0, len(s.items))
	for _, item := range s.items {
		if group == "" || item.Group == group {
			result = append(result, item)
		}
	}
	return result, nil
}

func (s handlerSettingsRepoStub) UpsertMany(context.Context, []appsettings.ItemInput) error {
	return s.err
}
