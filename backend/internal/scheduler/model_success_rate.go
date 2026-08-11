package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

const (
	modelSuccessRateWindow                 = 24 * time.Hour
	modelSuccessRateBucketWidth            = 30 * time.Minute
	modelSuccessRateBucketCount            = int(modelSuccessRateWindow / modelSuccessRateBucketWidth)
	modelSuccessRatePersistInterval        = 10 * time.Second
	modelSuccessRatePruneInterval          = 30 * time.Minute
	modelSuccessRateMinBucketValidRequests = 10
	modelSuccessRateRestoreBatch           = 100
	modelSuccessRateShardCount             = 32
	modelSuccessRateBucketSpreadMultiplier = 997
)

// ModelSuccessRateStats 是单个账号、单个调度模型在最近 24 小时固定桶内的汇总统计。
type ModelSuccessRateStats struct {
	AccountID       int                           `json:"account_id"`
	Model           string                        `json:"model"`
	Requests        uint64                        `json:"requests"`
	ValidRequests   uint64                        `json:"valid_requests"`
	InvalidRequests uint64                        `json:"invalid_requests"`
	Successes       uint64                        `json:"successes"`
	Failures        uint64                        `json:"failures"`
	SuccessRate     float64                       `json:"success_rate"`
	WindowStart     time.Time                     `json:"window_start"`
	WindowEnd       time.Time                     `json:"window_end"`
	LastUpdated     time.Time                     `json:"last_updated"`
	Buckets         []ModelSuccessRateBucketStats `json:"buckets"`
}

// ModelSuccessRateBucketStats 是一个固定 30 分钟桶内的账号 × 模型统计。
type ModelSuccessRateBucketStats struct {
	Index           int     `json:"index"`
	Requests        uint64  `json:"requests"`
	ValidRequests   uint64  `json:"valid_requests"`
	InvalidRequests uint64  `json:"invalid_requests"`
	Successes       uint64  `json:"successes"`
	Failures        uint64  `json:"failures"`
	SuccessRate     float64 `json:"success_rate"`
}

type ModelSuccessRateWindow struct {
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	BucketSeconds int64     `json:"bucket_seconds"`
	BucketCount   int       `json:"bucket_count"`
}

type modelSuccessRateBucket struct {
	bucket  int64
	success uint64
	failure uint64
	invalid uint64
}

type modelSuccessRateSeries struct {
	buckets    [modelSuccessRateBucketCount]modelSuccessRateBucket
	lastUpdate time.Time
}

type persistedModelSuccessRate struct {
	Models []persistedModelSuccessRateItem `json:"models"`
}

type persistedModelSuccessRateItem struct {
	Model       string                            `json:"model"`
	LastUpdated int64                             `json:"last_updated"`
	Buckets     []persistedModelSuccessRateBucket `json:"buckets"`
}

type persistedModelSuccessRateBucket struct {
	Bucket  int64  `json:"bucket"`
	Success uint64 `json:"success"`
	Failure uint64 `json:"failure"`
	Invalid uint64 `json:"invalid"`
}

type modelSuccessRateDirtySnapshot struct {
	shardIndex int
	accountID  int
	version    uint64
	payload    []byte
}

type modelSuccessRateShard struct {
	mu     sync.RWMutex
	series map[int]map[string]*modelSuccessRateSeries
	dirty  map[int]uint64
}

// ModelSuccessRateTracker keeps the request-path view in memory. Redis only
// stores occasional account snapshots for restart recovery and is never read
// or written while handling a request.
type ModelSuccessRateTracker struct {
	rdb *redis.Client

	shards [modelSuccessRateShardCount]modelSuccessRateShard
	now    func() time.Time
}

func NewModelSuccessRateTracker(rdb *redis.Client) *ModelSuccessRateTracker {
	tracker := &ModelSuccessRateTracker{rdb: rdb, now: time.Now}
	for index := range tracker.shards {
		tracker.shards[index].series = make(map[int]map[string]*modelSuccessRateSeries)
		tracker.shards[index].dirty = make(map[int]uint64)
	}
	return tracker
}

func (t *ModelSuccessRateTracker) shardFor(accountID int) (int, *modelSuccessRateShard) {
	index := accountID % modelSuccessRateShardCount
	if index < 0 {
		index += modelSuccessRateShardCount
	}
	return index, &t.shards[index]
}

func modelSuccessRateBucketOffset(accountID int) int64 {
	bucketSeconds := int64(modelSuccessRateBucketWidth / time.Second)
	if accountID <= 0 || bucketSeconds <= 0 {
		return 0
	}
	return ((int64(accountID) % bucketSeconds) * modelSuccessRateBucketSpreadMultiplier) % bucketSeconds
}

func modelSuccessRateBucketID(accountID int, now time.Time) int64 {
	bucketSeconds := int64(modelSuccessRateBucketWidth / time.Second)
	return (now.Unix() + modelSuccessRateBucketOffset(accountID)) / bucketSeconds
}

func modelSuccessRateBucketTime(accountID int, bucketID int64) time.Time {
	bucketSeconds := int64(modelSuccessRateBucketWidth / time.Second)
	return time.Unix(bucketID*bucketSeconds-modelSuccessRateBucketOffset(accountID), 0).UTC()
}

type modelSuccessRateDisposition uint8

const (
	modelSuccessRateInvalid modelSuccessRateDisposition = iota
	modelSuccessRateSuccess
	modelSuccessRateFailure
)

// Only successful requests and upstream failures participate in the success
// rate. Client/account 4xx results are retained as invalid requests so total
// traffic remains observable without penalizing the account-model pair.
func classifyModelSuccessRateOutcome(outcome sdk.ForwardOutcome) modelSuccessRateDisposition {
	statusCode := outcome.Upstream.StatusCode
	if statusCode == http.StatusForbidden {
		return modelSuccessRateInvalid
	}
	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return modelSuccessRateInvalid
	}
	if statusCode == http.StatusBadGateway && strings.Contains(strings.ToLower(outcome.Reason), "overloaded") {
		return modelSuccessRateFailure
	}
	if statusCode >= http.StatusInternalServerError {
		return modelSuccessRateFailure
	}

	switch outcome.Kind {
	case sdk.OutcomeSuccess:
		return modelSuccessRateSuccess
	case sdk.OutcomeUpstreamTransient,
		sdk.OutcomeStreamAborted,
		sdk.OutcomeFamilyTransient:
		return modelSuccessRateFailure
	case sdk.OutcomeClientError,
		sdk.OutcomeAccountRateLimited,
		sdk.OutcomeAccountDead,
		sdk.OutcomeAccountUnavailable,
		sdk.OutcomeAccountQuotaExhausted:
		return modelSuccessRateInvalid
	case sdk.OutcomeUnknown:
		// 502 overloaded should be identified from the plugin's reason. The
		// upstream response body is intentionally never inspected here.
		reason := strings.ToLower(outcome.Reason)
		if strings.Contains(reason, "502") && strings.Contains(reason, "overloaded") {
			return modelSuccessRateFailure
		}
		return modelSuccessRateInvalid
	default:
		return modelSuccessRateInvalid
	}
}

// Record observes one upstream attempt and only mutates process memory.
func (t *ModelSuccessRateTracker) Record(accountID int, model string, outcome sdk.ForwardOutcome) {
	if t == nil || accountID <= 0 {
		return
	}
	model = normalizeSuccessRateModel(model)
	if model == "" {
		return
	}

	now := t.now()
	bucketID := modelSuccessRateBucketID(accountID, now)
	disposition := classifyModelSuccessRateOutcome(outcome)

	_, shard := t.shardFor(accountID)
	shard.mu.Lock()
	models := shard.series[accountID]
	if models == nil {
		models = make(map[string]*modelSuccessRateSeries)
		shard.series[accountID] = models
	}
	series := models[model]
	if series == nil {
		series = &modelSuccessRateSeries{}
		models[model] = series
	}
	series.add(bucketID, disposition, now)
	shard.dirty[accountID]++
	shard.mu.Unlock()
}

// Forget removes all in-memory statistics and persistence bookkeeping for an
// account that has been deleted. It is intentionally independent of Redis so
// callers can use it even when the cache is not configured.
func (t *ModelSuccessRateTracker) Forget(accountID int) {
	if t == nil || accountID <= 0 {
		return
	}
	_, shard := t.shardFor(accountID)
	shard.mu.Lock()
	delete(shard.series, accountID)
	delete(shard.dirty, accountID)
	shard.mu.Unlock()
}

func normalizeSuccessRateModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func (s *modelSuccessRateSeries) add(bucketID int64, disposition modelSuccessRateDisposition, now time.Time) {
	index := int(bucketID % int64(modelSuccessRateBucketCount))
	bucket := &s.buckets[index]
	if bucket.bucket != bucketID {
		*bucket = modelSuccessRateBucket{bucket: bucketID}
	}
	switch disposition {
	case modelSuccessRateSuccess:
		bucket.success++
	case modelSuccessRateFailure:
		bucket.failure++
	case modelSuccessRateInvalid:
		bucket.invalid++
	}
	s.lastUpdate = now
}

func (s *modelSuccessRateSeries) stats(accountID int, model string, now time.Time) ModelSuccessRateStats {
	return s.statsWithBuckets(accountID, model, now, true)
}

func (s *modelSuccessRateSeries) statsWithBuckets(accountID int, model string, now time.Time, includeBuckets bool) ModelSuccessRateStats {
	currentBucket := modelSuccessRateBucketID(accountID, now)
	minimumBucket := currentBucket - int64(modelSuccessRateBucketCount) + 1
	window := modelSuccessRateWindowInfo(accountID, now)
	result := ModelSuccessRateStats{
		AccountID:   accountID,
		Model:       model,
		WindowStart: window.WindowStart,
		WindowEnd:   window.WindowEnd,
		LastUpdated: s.lastUpdate.UTC(),
	}
	if includeBuckets {
		result.Buckets = make([]ModelSuccessRateBucketStats, 0, modelSuccessRateBucketCount)
	}
	for bucketID := minimumBucket; bucketID <= currentBucket; bucketID++ {
		bucketStats := ModelSuccessRateBucketStats{Index: int(bucketID - minimumBucket)}
		bucket := s.buckets[int(bucketID%int64(modelSuccessRateBucketCount))]
		if bucket.bucket == bucketID {
			bucketStats.Successes = bucket.success
			bucketStats.Failures = bucket.failure
			bucketStats.InvalidRequests = bucket.invalid
		}
		bucketStats.ValidRequests = bucketStats.Successes + bucketStats.Failures
		bucketStats.Requests = bucketStats.ValidRequests + bucketStats.InvalidRequests
		if bucketStats.ValidRequests > 0 {
			bucketStats.SuccessRate = float64(bucketStats.Successes) / float64(bucketStats.ValidRequests)
		}
		if includeBuckets && bucketStats.Requests > 0 {
			result.Buckets = append(result.Buckets, bucketStats)
		}
		result.Successes += bucketStats.Successes
		result.Failures += bucketStats.Failures
		result.InvalidRequests += bucketStats.InvalidRequests
	}
	result.ValidRequests = result.Successes + result.Failures
	result.Requests = result.ValidRequests + result.InvalidRequests
	if result.ValidRequests > 0 {
		result.SuccessRate = float64(result.Successes) / float64(result.ValidRequests)
	}
	return result
}

func modelSuccessRateWindowInfo(accountID int, now time.Time) ModelSuccessRateWindow {
	currentBucket := modelSuccessRateBucketID(accountID, now)
	minimumBucket := currentBucket - int64(modelSuccessRateBucketCount) + 1
	bucketSeconds := int64(modelSuccessRateBucketWidth / time.Second)
	return ModelSuccessRateWindow{
		WindowStart:   modelSuccessRateBucketTime(accountID, minimumBucket),
		WindowEnd:     modelSuccessRateBucketTime(accountID, currentBucket+1),
		BucketSeconds: bucketSeconds,
		BucketCount:   modelSuccessRateBucketCount,
	}
}

func (t *ModelSuccessRateTracker) listAt(accountID int, now time.Time) []ModelSuccessRateStats {
	_, shard := t.shardFor(accountID)
	shard.mu.RLock()
	models := shard.series[accountID]
	result := make([]ModelSuccessRateStats, 0, len(models))
	for model, series := range models {
		result = append(result, series.stats(accountID, model, now))
	}
	shard.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Model < result[j].Model })
	return result
}

// Snapshot returns one account's model statistics and timeline using the same
// clock sample, so bucket labels cannot cross a boundary independently.
func (t *ModelSuccessRateTracker) Snapshot(accountID int) ([]ModelSuccessRateStats, ModelSuccessRateWindow) {
	if t == nil || accountID <= 0 {
		return nil, ModelSuccessRateWindow{}
	}
	now := t.now()
	return t.listAt(accountID, now), modelSuccessRateWindowInfo(accountID, now)
}

func (t *ModelSuccessRateTracker) Run(ctx context.Context) {
	if t == nil {
		return
	}

	// Redis is only a restart snapshot. A transient startup read failure does
	// not affect request handling; the current process continues from memory.
	if t.rdb != nil {
		t.restore(ctx)
	}
	persistTicker := time.NewTicker(modelSuccessRatePersistInterval)
	defer persistTicker.Stop()
	pruneTicker := time.NewTicker(modelSuccessRatePruneInterval)
	defer pruneTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			t.persistDirty(flushCtx)
			cancel()
			return
		case <-persistTicker.C:
			t.persistDirty(ctx)
		case <-pruneTicker.C:
			t.pruneExpired(t.now())
		}
	}
}

// isDemotedAt performs an O(1) request-path lookup against the current fixed
// 30-minute bucket for one exact scheduling model. A new bucket automatically
// clears the decision because stale bucket IDs never match; no timer, Redis
// access, or background quality state is required.
func (t *ModelSuccessRateTracker) isDemotedAt(accountID int, normalizedModel string, threshold float64, now time.Time) bool {
	if t == nil || accountID <= 0 || normalizedModel == "" || threshold <= 0 || threshold > 1 {
		return false
	}
	bucketID := modelSuccessRateBucketID(accountID, now)
	_, shard := t.shardFor(accountID)
	shard.mu.RLock()
	series := shard.series[accountID][normalizedModel]
	if series == nil {
		shard.mu.RUnlock()
		return false
	}
	bucket := series.buckets[int(bucketID%int64(modelSuccessRateBucketCount))]
	shard.mu.RUnlock()
	if bucket.bucket != bucketID {
		return false
	}
	valid := bucket.success + bucket.failure
	if valid < modelSuccessRateMinBucketValidRequests {
		return false
	}
	return float64(bucket.success)/float64(valid) < threshold
}

func (t *ModelSuccessRateTracker) persistDirty(ctx context.Context) {
	if t == nil || t.rdb == nil {
		return
	}
	snapshots := t.dirtySnapshots()
	if len(snapshots) == 0 {
		return
	}
	pipe := t.rdb.Pipeline()
	for _, snapshot := range snapshots {
		pipe.Set(ctx, accountcache.ModelStatsKey(snapshot.accountID), snapshot.payload, accountcache.ModelStatsTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return
	}

	for _, snapshot := range snapshots {
		shard := &t.shards[snapshot.shardIndex]
		shard.mu.Lock()
		if shard.dirty[snapshot.accountID] == snapshot.version {
			delete(shard.dirty, snapshot.accountID)
		}
		shard.mu.Unlock()
	}
}

func (t *ModelSuccessRateTracker) dirtySnapshots() []modelSuccessRateDirtySnapshot {
	now := t.now()

	snapshots := make([]modelSuccessRateDirtySnapshot, 0)
	for shardIndex := range t.shards {
		shard := &t.shards[shardIndex]
		shard.mu.RLock()
		accounts := make([]int, 0, len(shard.dirty))
		for accountID := range shard.dirty {
			accounts = append(accounts, accountID)
		}
		sort.Ints(accounts)
		for _, accountID := range accounts {
			currentBucket := modelSuccessRateBucketID(accountID, now)
			minimumBucket := currentBucket - int64(modelSuccessRateBucketCount) + 1
			persisted := persistedAccountSnapshot(
				shard.series[accountID],
				minimumBucket,
				currentBucket,
			)
			version := shard.dirty[accountID]
			shard.mu.RUnlock()

			payload, err := json.Marshal(persisted)
			if err == nil {
				snapshots = append(snapshots, modelSuccessRateDirtySnapshot{
					shardIndex: shardIndex,
					accountID:  accountID,
					version:    version,
					payload:    payload,
				})
			}

			shard.mu.RLock()
		}
		shard.mu.RUnlock()
	}
	return snapshots
}

func persistedAccountSnapshot(models map[string]*modelSuccessRateSeries, minimumBucket, currentBucket int64) persistedModelSuccessRate {
	persisted := persistedModelSuccessRate{}
	modelNames := make([]string, 0, len(models))
	for model := range models {
		modelNames = append(modelNames, model)
	}
	sort.Strings(modelNames)
	for _, model := range modelNames {
		series := models[model]
		item := persistedModelSuccessRateItem{Model: model, LastUpdated: series.lastUpdate.UnixMilli()}
		for _, bucket := range series.buckets {
			if bucket.bucket < minimumBucket || bucket.bucket > currentBucket {
				continue
			}
			item.Buckets = append(item.Buckets, persistedModelSuccessRateBucket{
				Bucket:  bucket.bucket,
				Success: bucket.success,
				Failure: bucket.failure,
				Invalid: bucket.invalid,
			})
		}
		if len(item.Buckets) > 0 {
			persisted.Models = append(persisted.Models, item)
		}
	}
	return persisted
}

func (t *ModelSuccessRateTracker) pruneExpired(now time.Time) {
	for shardIndex := range t.shards {
		shard := &t.shards[shardIndex]
		shard.mu.Lock()
		for accountID, models := range shard.series {
			currentBucket := modelSuccessRateBucketID(accountID, now)
			minimumBucket := currentBucket - int64(modelSuccessRateBucketCount) + 1
			for model, series := range models {
				if !series.hasRecentBuckets(minimumBucket, currentBucket) {
					delete(models, model)
				}
			}
			if len(models) == 0 {
				delete(shard.series, accountID)
			}
		}
		shard.mu.Unlock()
	}
}

func (s *modelSuccessRateSeries) hasRecentBuckets(minimumBucket, currentBucket int64) bool {
	for _, bucket := range s.buckets {
		if bucket.bucket >= minimumBucket && bucket.bucket <= currentBucket &&
			bucket.success+bucket.failure+bucket.invalid > 0 {
			return true
		}
	}
	return false
}

func (t *ModelSuccessRateTracker) restore(ctx context.Context) {
	if t == nil || t.rdb == nil {
		return
	}

	restored := make(map[int]struct{})
	var cursor uint64
	for {
		keys, next, err := t.rdb.Scan(ctx, cursor, accountcache.ModelStatsPattern(), modelSuccessRateRestoreBatch).Result()
		if err != nil {
			return
		}
		if !t.restoreKeys(ctx, keys, restored) {
			return
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

func (t *ModelSuccessRateTracker) restoreKeys(ctx context.Context, keys []string, restored map[int]struct{}) bool {
	if len(keys) == 0 {
		return true
	}

	pending := make(map[int]string, len(keys))
	for _, key := range keys {
		accountID, ok := modelStatsAccountID(key)
		if !ok {
			continue
		}
		if _, alreadyRestored := restored[accountID]; alreadyRestored {
			continue
		}
		restored[accountID] = struct{}{}
		pending[accountID] = key
	}
	if len(pending) == 0 {
		return true
	}

	commands := make(map[int]*redis.StringCmd, len(pending))
	_, err := t.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for accountID, key := range pending {
			commands[accountID] = pipe.Get(ctx, key)
		}
		return nil
	})
	if err != nil && err != redis.Nil {
		return false
	}
	for accountID, command := range commands {
		payload, commandErr := command.Bytes()
		if commandErr == nil {
			t.applyPersistedSnapshot(accountID, payload)
		} else if commandErr != redis.Nil {
			return false
		}
	}
	return true
}

func modelStatsAccountID(key string) (int, bool) {
	separator := strings.LastIndexByte(key, ':')
	if separator < 0 || separator == len(key)-1 {
		return 0, false
	}
	accountID, err := strconv.Atoi(key[separator+1:])
	return accountID, err == nil && accountID > 0
}

func (t *ModelSuccessRateTracker) applyPersistedSnapshot(accountID int, payload []byte) {
	var persisted persistedModelSuccessRate
	if accountID <= 0 || json.Unmarshal(payload, &persisted) != nil {
		return
	}

	now := t.now()
	currentBucket := modelSuccessRateBucketID(accountID, now)
	minimumBucket := currentBucket - int64(modelSuccessRateBucketCount) + 1

	_, shard := t.shardFor(accountID)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	models := shard.series[accountID]
	for _, item := range persisted.Models {
		model := normalizeSuccessRateModel(item.Model)
		if model == "" {
			continue
		}
		var series *modelSuccessRateSeries
		if models != nil {
			series = models[model]
		}
		for _, bucket := range item.Buckets {
			if bucket.Bucket < minimumBucket || bucket.Bucket > currentBucket {
				continue
			}
			if models == nil {
				models = make(map[string]*modelSuccessRateSeries)
				shard.series[accountID] = models
			}
			if series == nil {
				series = &modelSuccessRateSeries{}
				models[model] = series
			}
			index := int(bucket.Bucket % int64(modelSuccessRateBucketCount))
			local := &series.buckets[index]
			if local.bucket != bucket.Bucket {
				*local = modelSuccessRateBucket{bucket: bucket.Bucket}
			}
			local.success += bucket.Success
			local.failure += bucket.Failure
			local.invalid += bucket.Invalid
		}
		if series != nil {
			updated := time.UnixMilli(item.LastUpdated)
			if updated.After(series.lastUpdate) {
				series.lastUpdate = updated
			}
		}
	}
}
