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
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
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

func TestPublicRateLimitIgnoresForwardedFor(t *testing.T) {
	resetPublicRateLimiterForTesting()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	router.Use(PublicRateLimit(2, time.Minute))
	router.POST("/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "203.0.113.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.1")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("request %d status = %d", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For bypassed limiter, status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPublicRateLimitIgnoresForwardedForFromTrustedProxy(t *testing.T) {
	resetPublicRateLimiterForTesting()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	if err := router.SetTrustedProxies([]string{"203.0.113.1"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	router.Use(PublicRateLimit(2, time.Minute))
	router.POST("/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "203.0.113.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.1")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("trusted proxy request %d status = %d", i, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.2")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("trusted forwarded client bypassed limiter, status=%d body=%s", w.Code, w.Body.String())
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

func TestPublicRateLimiterCleanupExpiredBuckets(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := &publicRateLimiter{buckets: map[string]publicRateLimitBucket{
		"expired": {count: 1, reset: now.Add(-time.Second)},
		"active":  {count: 1, reset: now.Add(time.Minute)},
		"zero":    {count: 1},
	}}

	limiter.cleanupExpired(now)

	if _, ok := limiter.buckets["expired"]; ok {
		t.Fatal("expired bucket was not removed")
	}
	if _, ok := limiter.buckets["active"]; !ok {
		t.Fatal("active bucket was removed")
	}
	if _, ok := limiter.buckets["zero"]; !ok {
		t.Fatal("zero reset bucket should be preserved")
	}
}
