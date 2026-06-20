package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestRequestLoggerPropagatesRequestID(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/ping", func(c *gin.Context) {
		if got := RequestIDFromGinContext(c); got != "req-123" {
			t.Fatalf("上下文 request_id = %q，期望 req-123", got)
		}
		c.String(http.StatusCreated, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(sdk.HeaderRequestID, "req-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusCreated)
	}
	if got := w.Header().Get(sdk.HeaderRequestID); got != "req-123" {
		t.Fatalf("响应 request_id = %q，期望 req-123", got)
	}
}

func TestRequestLoggerCoversStatusLevelsAndAccessFields(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger())
	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/bad", func(c *gin.Context) {
		c.Set(CtxKeyUserID, int64(123))
		c.Set(CtxKeyAccessModel, "gpt-5")
		c.Set(CtxKeyAccessPlatform, "openai")
		c.Set(CtxKeyAccessAccountID, 45)
		c.Set(CtxKeyAccessAttempts, 2)
		c.String(http.StatusBadRequest, "bad")
	})
	router.GET("/err", func(c *gin.Context) {
		c.Set(CtxKeyUserID, 7)
		c.Set(CtxKeyAccessModel, "")
		c.Set(CtxKeyAccessPlatform, "")
		c.Set(CtxKeyAccessAccountID, 0)
		c.Set(CtxKeyAccessAttempts, 1)
		c.String(http.StatusInternalServerError, "err")
	})

	for _, path := range []string{"/healthz", "/bad", "/err"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code < 200 || w.Code >= 600 {
			t.Fatalf("%s status = %d", path, w.Code)
		}
		if got := w.Header().Get(sdk.HeaderRequestID); got == "" {
			t.Fatalf("%s missing response request id", path)
		}
	}
}

func TestRequestIDFromGinContextHandlesNil(t *testing.T) {
	if got := RequestIDFromGinContext(nil); got != "" {
		t.Fatalf("nil context request_id = %q，期望空字符串", got)
	}
}

func TestRecoveryReturnsJSONWithRequestID(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger(), Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("测试 panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set(sdk.HeaderRequestID, "panic-req")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusInternalServerError)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应 JSON 解析失败: %v", err)
	}
	if got["error"] != "internal_server_error" || got["request_id"] != "panic-req" {
		t.Fatalf("panic 响应异常: %#v", got)
	}
}
