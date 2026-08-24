package account

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

const (
	usageCacheMaxTTL            = 5 * time.Hour
	usageCacheMinimumTTL        = time.Second
	usageAccountsProbeBatchSize = 10
	usageCacheWriteTimeout      = 5 * time.Second
)

type accountUsageRepository interface {
	ListAll(context.Context, ListFilter) ([]Account, error)
	FindByID(context.Context, int, LoadOptions) (Account, error)
	BatchWindowStats(context.Context, []int, time.Time) (map[int]AccountWindowStats, error)
	BatchImageStats(context.Context, []int, time.Time) (map[int]AccountImageStats, error)
	ObserveUsageGrowth(context.Context, int, UsageGrowthObservation) error
}

type accountUsagePluginCatalog interface {
	GetPluginByPlatform(string) *plugin.PluginInstance
}

type accountUsageStateWriter interface {
	MarkRateLimited(context.Context, int, time.Time, string)
	ClearRateLimited(context.Context, int)
	MarkDisabled(context.Context, int, string)
	MarkDegraded(context.Context, int, string)
}

type usageCacheEntry struct {
	platform  string
	info      AccountUsageInfo
	fetchedAt time.Time
	expiresAt time.Time
}

type accountProfileCachePayload struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	Type           string  `json:"type"`
	State          string  `json:"state"`
	StateUntil     string  `json:"state_until,omitempty"`
	Priority       int     `json:"priority"`
	MaxConcurrency int     `json:"max_concurrency"`
	RateMultiplier float64 `json:"rate_multiplier"`
	ErrorMsg       string  `json:"error_msg,omitempty"`
	UpstreamIsPool bool    `json:"upstream_is_pool"`
	LastUsedAt     string  `json:"last_used_at,omitempty"`
	LastProbeAt    string  `json:"last_probe_at,omitempty"`
	GroupIDs       []int64 `json:"group_ids,omitempty"`
	ProxyID        *int    `json:"proxy_id,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// accountUsageCache owns the Redis client and the process-local fallback
// cache used by account usage views.
type accountUsageCache struct {
	mu      sync.RWMutex
	entries map[int]*usageCacheEntry
	rdb     *redis.Client
	now     func() time.Time
}

// accountUsageService owns account usage probing, refresh coordination and
// cache orchestration. Service composes it behind a small set of forwarding
// methods so usage runtime state stays out of the account CRUD service.
type accountUsageService struct {
	repo        accountUsageRepository
	plugins     accountUsagePluginCatalog
	stateWriter accountUsageStateWriter
	now         func() time.Time

	*accountUsageCache

	refreshMu   sync.Mutex
	refreshing  map[string]struct{}
	probeFlight singleflight.Group
}

func newAccountUsageService(
	repo accountUsageRepository,
	plugins accountUsagePluginCatalog,
	stateWriter accountUsageStateWriter,
	now func() time.Time,
) *accountUsageService {
	return &accountUsageService{
		repo:        repo,
		plugins:     plugins,
		stateWriter: stateWriter,
		now:         now,
		accountUsageCache: &accountUsageCache{
			entries: make(map[int]*usageCacheEntry),
			now:     now,
		},
		refreshing: make(map[string]struct{}),
	}
}

func (s *accountUsageService) setRedis(rdb *redis.Client) {
	if s == nil || s.accountUsageCache == nil {
		return
	}
	s.rdb = rdb
}
