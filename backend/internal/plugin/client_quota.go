package plugin

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/DevilGenius/airgate-core/internal/scheduler"
)

func (f *Forwarder) acquireDistributedClientQuota(c *gin.Context, state *forwardState) func() {
	info := state.keyInfo
	requestID := uuid.NewString()
	ttl := scheduler.SlotTTL(0)
	parent := c.Request.Context()
	ctx := parent
	var releases []func()
	var once sync.Once
	release := func() {
		once.Do(func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
			c.Request = c.Request.WithContext(parent)
		})
	}
	type gate struct {
		max           int
		code, message string
		acquire       func(context.Context) error
		renew         func(context.Context) (bool, error)
		release       func(context.Context)
	}
	gates := []gate{
		{info.UserMaxConcurrency, "user_concurrency_limit", "用户并发已达上限，请稍后重试",
			func(ctx context.Context) error {
				return f.concurrency.AcquireUserSlot(ctx, info.UserID, requestID, info.UserMaxConcurrency, ttl)
			},
			func(ctx context.Context) (bool, error) {
				return f.concurrency.RenewUserSlot(ctx, info.UserID, requestID, ttl)
			},
			func(ctx context.Context) { f.concurrency.ReleaseUserSlot(ctx, info.UserID, requestID) }},
		{info.KeyMaxConcurrency, "apikey_concurrency_limit", "API Key 并发已达上限，请稍后重试",
			func(ctx context.Context) error {
				return f.concurrency.AcquireAPIKeySlot(ctx, info.KeyID, requestID, info.KeyMaxConcurrency, ttl)
			},
			func(ctx context.Context) (bool, error) {
				return f.concurrency.RenewAPIKeySlot(ctx, info.KeyID, requestID, ttl)
			},
			func(ctx context.Context) { f.concurrency.ReleaseAPIKeySlot(ctx, info.KeyID, requestID) }},
	}
	for _, gate := range gates {
		if gate.max <= 0 {
			continue
		}
		if err := gate.acquire(ctx); err != nil {
			release()
			if errors.Is(err, scheduler.ErrConcurrencyLimit) {
				openAIRateLimitError(c, http.StatusTooManyRequests, gate.code, gate.message, time.Second)
			} else {
				openAIRateLimitError(c, http.StatusServiceUnavailable, "scheduler_unavailable", "调度服务暂不可用，请稍后重试", time.Second)
			}
			return nil
		}
		next, stop := scheduler.MaintainLease(ctx, ttl, gate.renew)
		ctx = next
		releases = append(releases, func() { stop(); gate.release(context.Background()) })
	}
	c.Request = c.Request.WithContext(ctx)
	return release
}
