package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type publicRateLimitBucket struct {
	count int
	reset time.Time
}

type publicRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]publicRateLimitBucket
}

var defaultPublicRateLimiter = &publicRateLimiter{buckets: map[string]publicRateLimitBucket{}}

func PublicRateLimit(maxRequests int, window time.Duration) gin.HandlerFunc {
	if maxRequests <= 0 || window <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		allowed, retryAfter := defaultPublicRateLimiter.allow(publicRateLimitKey(c), maxRequests, window, time.Now())
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "请求过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}

func publicRateLimitKey(c *gin.Context) string {
	route := c.FullPath()
	if route == "" {
		route = c.Request.URL.Path
	}
	return c.ClientIP() + "\x00" + route
}

func (l *publicRateLimiter) allow(key string, maxRequests int, window time.Duration, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]publicRateLimitBucket{}
	}
	bucket := l.buckets[key]
	if bucket.reset.IsZero() || !now.Before(bucket.reset) {
		l.buckets[key] = publicRateLimitBucket{count: 1, reset: now.Add(window)}
		return true, 0
	}
	if bucket.count >= maxRequests {
		retryAfter := bucket.reset.Sub(now).Round(time.Second)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}
	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

func resetPublicRateLimiterForTesting() {
	defaultPublicRateLimiter.mu.Lock()
	defer defaultPublicRateLimiter.mu.Unlock()
	defaultPublicRateLimiter.buckets = map[string]publicRateLimitBucket{}
}
