// Package ratelimit 提供基于 Redis 滑动窗口的限流功能
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrRateLimited = errors.New("请求过于频繁，请稍后重试")

// Config 限流配置
type Config struct {
	RPM int // 每分钟请求数，默认 60
	TPM int // 每分钟 Token 数，默认 100000
}

// DefaultConfig 返回默认限流配置
func DefaultConfig() Config {
	return Config{
		RPM: 60,
		TPM: 100000,
	}
}

// Limiter Redis 滑动窗口限流器
type Limiter struct {
	rdb *redis.Client
	cfg Config
}

// NewLimiter 创建限流器
func NewLimiter(rdb *redis.Client, cfg Config) *Limiter {
	if cfg.RPM <= 0 {
		cfg.RPM = 60
	}
	if cfg.TPM <= 0 {
		cfg.TPM = 100000
	}
	return &Limiter{rdb: rdb, cfg: cfg}
}

// Check 检查用户在指定平台的 RPM 限流
// 使用 Redis 滑动窗口算法
func (l *Limiter) Check(ctx context.Context, userID int, platform string) error {
	key := fmt.Sprintf("ratelimit:rpm:%d:%s", userID, platform)
	now := time.Now().UnixMicro()
	windowStart := now - 60*1000000 // 1 分钟窗口（微秒）

	// 使用 Redis 事务执行滑动窗口限流
	pipe := l.rdb.Pipeline()

	// 移除窗口外的记录
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatInt(windowStart, 10))

	// 统计窗口内的请求数
	countCmd := pipe.ZCard(ctx, key)

	// 添加当前请求
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})

	// 设置过期时间（避免 key 残留）
	pipe.Expire(ctx, key, 2*time.Minute)

	if _, err := pipe.Exec(ctx); err != nil {
		// Redis 不可用时放行，避免影响正常请求
		return nil
	}

	if countCmd.Val() >= int64(l.cfg.RPM) {
		return ErrRateLimited
	}
	return nil
}
