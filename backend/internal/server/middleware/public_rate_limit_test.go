package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestPublicRateLimitLimitsByClientAndRoute(t *testing.T) {
	resetPublicRateLimiterForTesting()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(PublicRateLimit(2, time.Minute))
	router.POST("/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/register", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "203.0.113.1:1234"
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("request %d status = %d", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("limited status=%d retry=%q body=%s", w.Code, w.Header().Get("Retry-After"), w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/register", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("different route should have separate bucket, status=%d", w.Code)
	}
}

func TestPublicRateLimiterWindowResets(t *testing.T) {
	limiter := &publicRateLimiter{}
	now := time.Unix(100, 0)
	if ok, _ := limiter.allow("key", 1, time.Minute, now); !ok {
		t.Fatal("first request should pass")
	}
	if ok, retry := limiter.allow("key", 1, time.Minute, now.Add(time.Second)); ok || retry <= 0 {
		t.Fatalf("second request = %v retry %s, want limited", ok, retry)
	}
	if ok, _ := limiter.allow("key", 1, time.Minute, now.Add(2*time.Minute)); !ok {
		t.Fatal("request after window should pass")
	}
}
