package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// defaultOverloadDuration 默认限流冷却时间（当上游未返回 RetryAfter 时使用）
	defaultOverloadDuration = 60 * time.Second
	// maxOverloadDuration 最大限流冷却时间，防止异常值
	maxOverloadDuration = 10 * time.Minute
)

// OverloadManager 账户临时限流管理
// 基于 Redis KEY + TTL 实现，收到 429 时设置冷却期，冷却期内不调度该账户
type OverloadManager struct {
	rdb *redis.Client
}

// NewOverloadManager 创建限流管理器
func NewOverloadManager(rdb *redis.Client) *OverloadManager {
	return &OverloadManager{rdb: rdb}
}

// overloadKey 生成 Redis Key
func overloadKey(accountID int) string {
	return fmt.Sprintf("overload:%d", accountID)
}

// MarkOverloaded 标记账户为临时限流状态
// retryAfter 为上游返回的建议等待时间，<= 0 时使用默认值
func (m *OverloadManager) MarkOverloaded(ctx context.Context, accountID int, retryAfter time.Duration) {
	if m.rdb == nil {
		return
	}

	if retryAfter <= 0 {
		retryAfter = defaultOverloadDuration
	}
	if retryAfter > maxOverloadDuration {
		retryAfter = maxOverloadDuration
	}

	m.rdb.Set(ctx, overloadKey(accountID), "1", retryAfter)
}

// IsOverloaded 检查账户是否处于限流冷却期
func (m *OverloadManager) IsOverloaded(ctx context.Context, accountID int) bool {
	if m.rdb == nil {
		return false
	}

	exists, err := m.rdb.Exists(ctx, overloadKey(accountID)).Result()
	if err != nil {
		return false // fail-open
	}
	return exists > 0
}

// ClearOverload 清除账户限流状态（如需手动恢复）
func (m *OverloadManager) ClearOverload(ctx context.Context, accountID int) {
	if m.rdb == nil {
		return
	}
	m.rdb.Del(ctx, overloadKey(accountID))
}

// GetSchedulability 返回限流调度状态
func (m *OverloadManager) GetSchedulability(ctx context.Context, accountID int) Schedulability {
	if m.IsOverloaded(ctx, accountID) {
		return NotSchedulable
	}
	return Normal
}
