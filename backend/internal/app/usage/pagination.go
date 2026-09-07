package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"github.com/DevilGenius/airgate-core/internal/pkg/timezone"
)

const (
	pageIndexFreshTTL        = 5 * time.Minute
	pageIndexTTL             = 30 * time.Minute
	pageIndexBuildTimeout    = 90 * time.Second
	pageIndexRefreshCooldown = 30 * time.Second
	pageIndexRetryDelay      = 15 * time.Second
	pageIndexMaxEntries      = 64
	pageIndexCacheBytes      = 128 << 20
	pageIndexRedisMaxBytes   = 16 << 20
)

type cachedPageIndex struct {
	index  *PageIndex
	usedAt time.Time
}
type pageIndexBuild struct {
	startedAt time.Time
	failed    bool
}

type paginationCache struct {
	mu      sync.Mutex
	entries map[string]cachedPageIndex
	latest  map[string]string
	builds  map[string]pageIndexBuild
	bytes   int64
	worker  chan struct{}
	decode  chan struct{}
	loads   singleflight.Group
	repo    PaginationRepository
	rdb     *redis.Client
}

func newPaginationCache(repo PaginationRepository, rdb *redis.Client) *paginationCache {
	return &paginationCache{entries: map[string]cachedPageIndex{}, latest: map[string]string{}, builds: map[string]pageIndexBuild{}, worker: make(chan struct{}, 1), decode: make(chan struct{}, 1), repo: repo, rdb: rdb}
}

func normalizedListFilter(filter ListFilter) (ListFilter, error) {
	filter.Page, filter.PageSize, filter.BeforeID = 0, 0, 0
	filter.Snapshot, filter.PageIDs = "", nil
	filter.AccountSearch = strings.TrimSpace(filter.AccountSearch)
	filter.Platform = strings.TrimSpace(filter.Platform)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.StartDate = strings.TrimSpace(filter.StartDate)
	filter.EndDate = strings.TrimSpace(filter.EndDate)
	loc := timezone.Resolve(filter.TZ)
	filter.TZ = loc.String()
	if filter.StartDate == "" && filter.EndDate == "" {
		filter.TZ = "UTC"
	}
	var start, end time.Time
	var err error
	if filter.StartDate != "" {
		start, err = timezone.ParseDate(filter.StartDate, loc)
		if err != nil {
			return filter, ErrInvalidListFilter
		}
	}
	if filter.EndDate != "" {
		end, err = timezone.ParseDate(filter.EndDate, loc)
		if err != nil {
			return filter, ErrInvalidListFilter
		}
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return filter, ErrInvalidListFilter
	}
	if err := ValidateModelFilter(filter.Model); err != nil {
		return filter, fmt.Errorf("%w: %v", ErrInvalidListFilter, err)
	}
	for _, id := range []*int64{filter.UserID, filter.APIKeyID, filter.AccountID, filter.GroupID} {
		if id != nil && *id <= 0 {
			return filter, ErrInvalidListFilter
		}
	}
	return filter, nil
}

func pageIndexFilterKey(filter ListFilter) string { return usageCacheKey("pages-v1", filter) }
func pageIndexDataKey(token string) string        { return "ag:usage:page-index:v1:" + token }

func pageIndexInfo(index *PageIndex, refreshing bool) PaginationInfo {
	return PaginationInfo{Status: "ready", Snapshot: index.Token, Total: index.Total, CreatedAt: index.CreatedAt, ExpiresAt: index.ExpiresAt, Refreshing: refreshing}
}

// Pagination prepares exact page metadata separately from the live first page.
// userID>0 is always imposed by the authenticated user handler.
func (s *Service) Pagination(ctx context.Context, userID int64, filter ListFilter, refresh bool) (PaginationInfo, error) {
	if s.pagination == nil {
		return PaginationInfo{}, errors.New("usage pagination unavailable")
	}
	if userID > 0 {
		filter.UserID = &userID
	}
	filter, err := normalizedListFilter(filter)
	if err != nil {
		return PaginationInfo{}, err
	}
	return s.pagination.info(ctx, filter, refresh), nil
}

func (p *paginationCache) local(token string) *PageIndex {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[token]
	if !ok {
		return nil
	}
	if !time.Now().Before(entry.index.ExpiresAt) {
		p.removeLocked(token)
		return nil
	}
	entry.usedAt = time.Now()
	p.entries[token] = entry
	return entry.index
}

func (p *paginationCache) removeLocked(token string) {
	entry, ok := p.entries[token]
	if !ok {
		return
	}
	delete(p.entries, token)
	p.bytes -= entry.index.memoryBytes()
	if p.latest[entry.index.FilterKey] == token {
		delete(p.latest, entry.index.FilterKey)
	}
}

func (p *paginationCache) put(index *PageIndex) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeLocked(index.Token)
	for len(p.entries) >= pageIndexMaxEntries || p.bytes+index.memoryBytes() > pageIndexCacheBytes {
		oldest := ""
		var at time.Time
		for token, entry := range p.entries {
			if oldest == "" || entry.usedAt.Before(at) {
				oldest, at = token, entry.usedAt
			}
		}
		if oldest == "" {
			return
		}
		p.removeLocked(oldest)
	}
	p.entries[index.Token] = cachedPageIndex{index: index, usedAt: time.Now()}
	p.bytes += index.memoryBytes()
	// Loading an old, pinned token must never replace a newer default view.
	current, exists := p.entries[p.latest[index.FilterKey]]
	if !exists || index.CreatedAt.After(current.index.CreatedAt) {
		p.latest[index.FilterKey] = index.Token
	}
}

func (p *paginationCache) load(ctx context.Context, token, key string) *PageIndex {
	if _, err := uuid.Parse(token); err != nil {
		return nil
	}
	if index := p.local(token); index != nil {
		if index.FilterKey == key {
			return index
		}
		return nil
	}
	if p.rdb == nil {
		return nil
	}
	result := p.loads.DoChan(token, func() (any, error) {
		if index := p.local(token); index != nil {
			return index, nil
		}
		loadCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		select {
		case p.decode <- struct{}{}:
			defer func() { <-p.decode }()
		case <-loadCtx.Done():
			return nil, loadCtx.Err()
		}
		raw, err := p.rdb.Get(loadCtx, pageIndexDataKey(token)).Bytes()
		if err != nil || len(raw) > pageIndexRedisMaxBytes {
			return nil, ErrPageIndexExpired
		}
		index, err := decodePageIndex(raw)
		if err != nil || index.Token != token || !time.Now().Before(index.ExpiresAt) {
			return nil, ErrPageIndexExpired
		}
		p.put(index)
		return index, nil
	})
	select {
	case <-ctx.Done():
		return nil
	case result := <-result:
		if index, ok := result.Val.(*PageIndex); result.Err == nil && ok && index.FilterKey == key {
			return index
		}
	}
	return nil
}

func (p *paginationCache) info(ctx context.Context, filter ListFilter, refresh bool) PaginationInfo {
	key := pageIndexFilterKey(filter)
	p.mu.Lock()
	token := p.latest[key]
	build, building := p.builds[key]
	p.mu.Unlock()
	var index *PageIndex
	if token != "" {
		index = p.local(token)
	}
	if index == nil && !building && p.rdb != nil {
		readCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		remote, err := p.rdb.Get(readCtx, key+":latest").Result()
		cancel()
		if err == nil {
			index = p.load(ctx, remote, key)
		}
	}
	if building {
		if !build.failed {
			if index != nil {
				return pageIndexInfo(index, true)
			}
			return PaginationInfo{Status: "preparing"}
		}
		if time.Since(build.startedAt) < pageIndexRetryDelay {
			if index != nil {
				return pageIndexInfo(index, false)
			}
			return PaginationInfo{Status: "failed"}
		}
	}
	if index != nil && ((!refresh && time.Since(index.CreatedAt) < pageIndexFreshTTL) || time.Since(index.CreatedAt) < pageIndexRefreshCooldown) {
		return pageIndexInfo(index, false)
	}
	started := p.start(filter, key)
	if index != nil {
		return pageIndexInfo(index, started)
	}
	return PaginationInfo{Status: "preparing"}
}

func (p *paginationCache) start(filter ListFilter, key string) bool {
	p.mu.Lock()
	if b, ok := p.builds[key]; ok && !b.failed {
		p.mu.Unlock()
		return true
	}
	select {
	case p.worker <- struct{}{}:
	default:
		p.mu.Unlock()
		return false
	}
	for k, b := range p.builds {
		if b.failed {
			delete(p.builds, k)
		}
	}
	p.builds[key] = pageIndexBuild{startedAt: time.Now()}
	p.mu.Unlock()
	go p.build(filter, key)
	return true
}

func (p *paginationCache) build(filter ListFilter, key string) {
	start := time.Now()
	var buildErr error
	defer func() {
		if recovered := recover(); recovered != nil {
			buildErr = fmt.Errorf("page index panic: %v", recovered)
		}
		p.mu.Lock()
		if buildErr != nil {
			p.builds[key] = pageIndexBuild{startedAt: time.Now(), failed: true}
		} else {
			delete(p.builds, key)
		}
		p.mu.Unlock()
		<-p.worker
		if buildErr != nil {
			slog.Warn("usage_page_index_failed", "duration_ms", time.Since(start).Milliseconds(), "error", buildErr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), pageIndexBuildTimeout)
	defer cancel()
	lockToken := uuid.NewString()
	if p.rdb != nil {
		lockCtx, lockCancel := context.WithTimeout(ctx, time.Second)
		locked, err := p.rdb.SetNX(lockCtx, key+":building", lockToken, pageIndexBuildTimeout+5*time.Second).Result()
		lockCancel()
		if err == nil && !locked {
			return
		}
		if err == nil {
			defer func() {
				releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				defer releaseCancel()
				_, _ = usageCacheLockReleaseScript.Run(releaseCtx, p.rdb, []string{key + ":building"}, lockToken).Result()
			}()
		}
	}
	index, err := p.repo.BuildPageIndex(ctx, filter)
	if err != nil {
		buildErr = err
		return
	}
	index.Token = uuid.NewString()
	index.FilterKey = key
	index.CreatedAt = start
	index.ExpiresAt = time.Now().Add(pageIndexTTL)
	if index.Total <= 100 {
		index.ExpiresAt = time.Now().Add(time.Minute)
	}
	if p.rdb != nil {
		raw, err := encodePageIndex(index)
		if err == nil && len(raw) <= pageIndexRedisMaxBytes {
			writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
			// Publish the token only after its complete immutable payload is stored.
			ttl := time.Until(index.ExpiresAt)
			if err = p.rdb.Set(writeCtx, pageIndexDataKey(index.Token), raw, ttl).Err(); err == nil {
				_ = p.rdb.Set(writeCtx, key+":latest", index.Token, min(ttl, pageIndexFreshTTL)).Err()
			}
			writeCancel()
		}
	}
	p.put(index)
	slog.Info("usage_page_index_ready", "total", index.Total, "words", len(index.Words), "memory_bytes", index.memoryBytes(), "duration_ms", time.Since(start).Milliseconds())
}

func (s *Service) snapshotPage(ctx context.Context, userID int64, filter ListFilter) (ListResult, error) {
	if s.pagination == nil {
		return ListResult{}, ErrPageIndexExpired
	}
	normalized := filter
	if userID > 0 {
		normalized.UserID = &userID
	}
	normalized, err := normalizedListFilter(normalized)
	if err != nil {
		return ListResult{}, err
	}
	index := s.pagination.load(ctx, filter.Snapshot, pageIndexFilterKey(normalized))
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	if index == nil {
		return ListResult{}, ErrPageIndexExpired
	}
	page, size := NormalizePage(filter.Page, filter.PageSize)
	ids, page := index.Page(page, size)
	result := ListResult{List: []LogRecord{}, Page: page, PageSize: size, Total: index.Total, TotalExact: true, HasMore: int64(page)*int64(size) < index.Total}
	if len(ids) == 0 {
		return result, nil
	}
	filter = normalized
	filter.PageIDs = ids
	filter.BeforeID = 0
	filter.Page, filter.PageSize = page, size
	if userID > 0 {
		result.List, _, _, err = s.repo.ListUser(ctx, userID, filter)
	} else {
		result.List, _, _, err = s.repo.ListAdmin(ctx, filter)
	}
	if err != nil {
		return ListResult{}, err
	}
	if len(result.List) != len(ids) {
		s.pagination.invalidate(index)
		return ListResult{}, ErrPageIndexExpired
	}
	return result, nil
}

func (p *paginationCache) invalidate(index *PageIndex) {
	p.mu.Lock()
	p.removeLocked(index.Token)
	p.mu.Unlock()
	if p.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, _ = usageCacheLockReleaseScript.Run(ctx, p.rdb, []string{index.FilterKey + ":latest"}, index.Token).Result()
		_ = p.rdb.Del(ctx, pageIndexDataKey(index.Token)).Err()
	}
}
