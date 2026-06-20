package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent"
	coreauth "github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func newAuthContext(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, w
}

func TestExtractBearerTokenAndHasAPIKey(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		apiKey        string
		wantToken     string
		wantHasKey    bool
	}{
		{"authorization_bearer", "Bearer sk-test", "", "sk-test", true},
		{"authorization_case_insensitive", "bearer sk-test", "", "sk-test", true},
		{"authorization_jwt_not_api_key", "Bearer eyJhbGciOiJIUzI1NiJ9.test.sig", "", "eyJhbGciOiJIUzI1NiJ9.test.sig", false},
		{"authorization_trim_space_non_api_key", "Bearer   token-123  ", "", "token-123", false},
		{"x_api_key_fallback", "", "sk-from-header", "sk-from-header", true},
		{"x_api_key_when_auth_not_bearer", "Basic abc", "sk-from-header", "sk-from-header", true},
		{"missing", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newAuthContext(http.MethodGet, "/v1/chat/completions")
			if tt.authorization != "" {
				c.Request.Header.Set("Authorization", tt.authorization)
			}
			if tt.apiKey != "" {
				c.Request.Header.Set("x-api-key", tt.apiKey)
			}

			if got := extractBearerToken(c); got != tt.wantToken {
				t.Fatalf("token = %q，期望 %q", got, tt.wantToken)
			}
			if got := HasAPIKey(c); got != tt.wantHasKey {
				t.Fatalf("HasAPIKey = %v，期望 %v", got, tt.wantHasKey)
			}
		})
	}
}

func TestAdminOnlyRejectsMissingOrNonAdminRole(t *testing.T) {
	tests := []struct {
		name string
		role string
	}{
		{"missing_role", ""},
		{"user_role", "user"},
		{"api_key_role", "api_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.role != "" {
					c.Set(CtxKeyRole, tt.role)
				}
			})
			router.Use(AdminOnly())
			router.GET("/admin", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))

			if w.Code != http.StatusForbidden {
				t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusForbidden)
			}
		})
	}
}

func TestRequireRoles(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		wantCode int
	}{
		{"missing_role", "", http.StatusForbidden},
		{"api_key_role", "api_key", http.StatusForbidden},
		{"user_role", "user", http.StatusOK},
		{"admin_role", "admin", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.role != "" {
					c.Set(CtxKeyRole, tt.role)
				}
			})
			router.Use(RequireRoles("admin", "user"))
			router.GET("/account", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/account", nil))

			if w.Code != tt.wantCode {
				t.Fatalf("状态码 = %d，期望 %d", w.Code, tt.wantCode)
			}
		})
	}
}

func TestAdminOnlyAllowsAdminRole(t *testing.T) {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(CtxKeyRole, "admin")
	})
	router.Use(AdminOnly())
	router.GET("/admin", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusOK)
	}
}

func TestAbortWithOpenAIError(t *testing.T) {
	c, w := newAuthContext(http.MethodGet, "/v1/models")

	abortWithOpenAIError(c, http.StatusPaymentRequired, "insufficient_quota", "额度不足")

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusPaymentRequired)
	}
	var got map[string]map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应 JSON 解析失败: %v", err)
	}
	errBody := got["error"]
	if errBody["message"] != "额度不足" || errBody["type"] != "authentication_error" || errBody["code"] != "insufficient_quota" {
		t.Fatalf("错误响应异常: %#v", errBody)
	}
	if !c.IsAborted() {
		t.Fatal("请求应该被终止")
	}
}

func TestJWTAuthRejectsMissingAndInvalidToken(t *testing.T) {
	mgr := coreauth.NewJWTManager("secret", 1)
	for _, tt := range []struct {
		name          string
		authorization string
		wantMessage   string
	}{
		{name: "missing", wantMessage: "缺少认证 Token"},
		{name: "invalid", authorization: "Bearer invalid", wantMessage: "Token 无效或已过期"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(JWTAuth(mgr))
			router.GET("/me", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			var got map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got["message"] != tt.wantMessage {
				t.Fatalf("message = %#v, want %q", got["message"], tt.wantMessage)
			}
		})
	}
}

func TestJWTAuthAcceptsUserAndAPIKeySessionTokens(t *testing.T) {
	mgr := coreauth.NewJWTManager("secret", 1)
	userToken, err := mgr.GenerateToken(12, "user", "u@example.com")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	apiKeyToken, err := mgr.GenerateAPIKeyToken(13, "", "k@example.com", 99)
	if err != nil {
		t.Fatalf("GenerateAPIKeyToken: %v", err)
	}

	tests := []struct {
		name       string
		token      string
		wantUserID int
		wantRole   string
		wantEmail  string
		wantKeyID  int
	}{
		{name: "user", token: userToken, wantUserID: 12, wantRole: "user", wantEmail: "u@example.com"},
		{name: "api_key", token: apiKeyToken, wantUserID: 13, wantRole: coreauth.APIKeySessionRole, wantEmail: "k@example.com", wantKeyID: 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(JWTAuth(mgr))
			router.GET("/me", func(c *gin.Context) {
				if got := c.GetInt(CtxKeyUserID); got != tt.wantUserID {
					t.Fatalf("user_id = %d, want %d", got, tt.wantUserID)
				}
				if got := c.GetString(CtxKeyRole); got != tt.wantRole {
					t.Fatalf("role = %q, want %q", got, tt.wantRole)
				}
				if got := c.GetString(CtxKeyEmail); got != tt.wantEmail {
					t.Fatalf("email = %q, want %q", got, tt.wantEmail)
				}
				if tt.wantKeyID > 0 {
					if got := c.GetInt(CtxKeyAPIKeyID); got != tt.wantKeyID {
						t.Fatalf("api_key_id = %d, want %d", got, tt.wantKeyID)
					}
				}
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/me", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestJWTAuthAcceptsAndRejectsAdminAPIKeys(t *testing.T) {
	db := openMiddlewareAuthDB(t, "middleware_admin_key")
	defer closeMiddlewareAuthDB(t, db)
	ctx := context.Background()

	adminKey := "admin-middleware-valid"
	if _, err := db.Setting.Create().
		SetGroup("security").
		SetKey("admin_api_key_hash").
		SetValue(coreauth.HashAPIKey(adminKey)).
		Save(ctx); err != nil {
		t.Fatalf("create admin key setting: %v", err)
	}

	mgr := coreauth.NewJWTManager("secret", 1)
	router := gin.New()
	router.Use(JWTAuth(mgr, db))
	router.GET("/admin", func(c *gin.Context) {
		if got := c.GetInt(CtxKeyUserID); got != 0 {
			t.Fatalf("admin user id = %d, want 0", got)
		}
		if got := c.GetString(CtxKeyRole); got != "admin" {
			t.Fatalf("admin role = %q, want admin", got)
		}
		if got := c.GetString(CtxKeyEmail); got != "" {
			t.Fatalf("admin email = %q, want empty", got)
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid admin key status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer admin-middleware-wrong")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid admin key status = %d, want 401", w.Code)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode invalid admin response: %v", err)
	}
	if got["message"] != "管理员 API Key 无效" {
		t.Fatalf("invalid admin message = %#v", got["message"])
	}
}

func TestAPIKeyAuthRejectsMissingAndInvalidFormat(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		wantCode      string
	}{
		{name: "missing", wantCode: "missing_api_key"},
		{name: "invalid_format", authorization: "Bearer jwt-token", wantCode: "invalid_api_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(APIKeyAuth(nil))
			router.GET("/v1/models", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			var got map[string]map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got["error"]["code"] != tt.wantCode {
				t.Fatalf("error code = %q, want %q", got["error"]["code"], tt.wantCode)
			}
		})
	}
}

func TestAPIKeyAuthValidatesDatabaseBackedKeys(t *testing.T) {
	coreauth.SetAPIKeyCacheRedis(nil)
	coreauth.InvalidateAPIKeyCache("")
	t.Cleanup(func() {
		coreauth.SetAPIKeyCacheRedis(nil)
		coreauth.InvalidateAPIKeyCache("")
	})

	db := openMiddlewareAuthDB(t, "middleware_api_key_auth")
	defer closeMiddlewareAuthDB(t, db)
	ctx := context.Background()
	user := createMiddlewareAuthUser(t, ctx, db, "middleware-user@example.com")
	group := createMiddlewareAuthGroup(t, ctx, db, "Middleware", "openai")
	validKey := createMiddlewareAPIKey(t, ctx, db, user, group, "valid", nil)

	router := gin.New()
	router.Use(APIKeyAuth(db))
	router.GET("/v1/models", func(c *gin.Context) {
		if got := c.GetInt(CtxKeyUserID); got != user.ID {
			t.Fatalf("context user id = %d, want %d", got, user.ID)
		}
		value, ok := c.Get(CtxKeyKeyInfo)
		if !ok {
			t.Fatalf("missing api key info")
		}
		info, ok := value.(*coreauth.APIKeyInfo)
		if !ok {
			t.Fatalf("api key info type = %T", value)
		}
		if info.UserID != user.ID || info.GroupID != group.ID || info.GroupName != group.Name {
			t.Fatalf("api key info = %+v", info)
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("valid api key status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAPIKeyAuthMapsValidationErrors(t *testing.T) {
	coreauth.SetAPIKeyCacheRedis(nil)
	coreauth.InvalidateAPIKeyCache("")
	t.Cleanup(func() {
		coreauth.SetAPIKeyCacheRedis(nil)
		coreauth.InvalidateAPIKeyCache("")
	})

	db := openMiddlewareAuthDB(t, "middleware_api_key_errors")
	defer closeMiddlewareAuthDB(t, db)
	ctx := context.Background()
	user := createMiddlewareAuthUser(t, ctx, db, "middleware-errors@example.com")
	group := createMiddlewareAuthGroup(t, ctx, db, "Errors", "openai")

	expiredKey := createMiddlewareAPIKey(t, ctx, db, user, group, "expired", func(c *ent.APIKeyCreate) {
		c.SetExpiresAt(time.Now().Add(-time.Hour))
	})
	quotaKey := createMiddlewareAPIKey(t, ctx, db, user, group, "quota", func(c *ent.APIKeyCreate) {
		c.SetQuotaUsd(1).SetUsedQuota(1)
	})
	unboundKey := createMiddlewareAPIKey(t, ctx, db, user, nil, "unbound", nil)

	tests := []struct {
		name       string
		key        string
		wantStatus int
		wantCode   string
	}{
		{name: "invalid", key: "sk-middleware-missing", wantStatus: http.StatusUnauthorized, wantCode: "invalid_api_key"},
		{name: "expired", key: expiredKey, wantStatus: http.StatusUnauthorized, wantCode: "api_key_expired"},
		{name: "quota", key: quotaKey, wantStatus: http.StatusPaymentRequired, wantCode: "insufficient_quota"},
		{name: "group_unbound", key: unboundKey, wantStatus: http.StatusForbidden, wantCode: "api_key_misconfigured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreauth.InvalidateAPIKeyCache("")
			status, code := serveAPIKeyAuthError(t, db, tt.key, nil)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Fatalf("status/code = %d/%q, want %d/%q", status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}

	t.Run("database_error", func(t *testing.T) {
		coreauth.InvalidateAPIKeyCache("")
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		status, code := serveAPIKeyAuthError(t, db, "sk-middleware-canceled", canceled)
		if status != http.StatusServiceUnavailable || code != "service_unavailable" {
			t.Fatalf("status/code = %d/%q, want 503/service_unavailable", status, code)
		}
	})
}

func serveAPIKeyAuthError(t *testing.T, db *ent.Client, key string, ctx context.Context) (int, string) {
	t.Helper()
	router := gin.New()
	router.Use(APIKeyAuth(db))
	router.GET("/v1/models", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var got map[string]map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode api key error response: %v; body=%s", err, w.Body.String())
	}
	return w.Code, got["error"]["code"]
}

func openMiddlewareAuthDB(t *testing.T, name string) *ent.Client {
	t.Helper()
	return testdb.OpenMemoryEnt(t, name, schema.WithGlobalUniqueID(false))
}

func closeMiddlewareAuthDB(t *testing.T, db *ent.Client) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}

func createMiddlewareAuthUser(t *testing.T, ctx context.Context, db *ent.Client, email string) *ent.User {
	t.Helper()
	user, err := db.User.Create().
		SetEmail(email).
		SetPasswordHash("secret").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return user
}

func createMiddlewareAuthGroup(t *testing.T, ctx context.Context, db *ent.Client, name, platform string) *ent.Group {
	t.Helper()
	group, err := db.Group.Create().
		SetName(name).
		SetPlatform(platform).
		Save(ctx)
	if err != nil {
		t.Fatalf("create group %q: %v", name, err)
	}
	return group
}

func createMiddlewareAPIKey(t *testing.T, ctx context.Context, db *ent.Client, user *ent.User, group *ent.Group, name string, mutate func(*ent.APIKeyCreate)) string {
	t.Helper()
	key, hash, err := coreauth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey %s: %v", name, err)
	}
	create := db.APIKey.Create().
		SetName(name).
		SetKeyHash(hash).
		SetUser(user)
	if group != nil {
		create.SetGroup(group)
	}
	if mutate != nil {
		mutate(create)
	}
	if _, err := create.Save(ctx); err != nil {
		t.Fatalf("create api key %s: %v", name, err)
	}
	return key
}
