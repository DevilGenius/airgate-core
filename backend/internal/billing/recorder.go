package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/usagelog"
	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	"github.com/DevilGenius/airgate-core/internal/pkg/ratevalue"
	"github.com/DevilGenius/airgate-core/internal/pkg/usagemodel"
	"github.com/DevilGenius/airgate-core/internal/safego"
)

const (
	defaultBufferSize    = 1000            // 内存 channel 缓冲大小
	batchSize            = 100             // 批量写入阈值
	flushInterval        = 5 * time.Second // 定时刷新间隔
	maxFlushAttempts     = 6               // 包含首写在内的最大写入尝试次数
	retryQueueSize       = 64              // 失败批次补偿队列
	syncFallbackTimeout  = 10 * time.Second
	writeAttemptTimeout  = 15 * time.Second
	shutdownFlushTimeout = 15 * time.Second
	maxRetryBackoff      = 30 * time.Second
)

var errRecorderStopping = errors.New("billing recorder stopping")

// UsageRecord 使用记录
type UsageRecord struct {
	BillingEventID        string
	UserID                int
	UserEmail             string
	APIKeyID              int
	AccountID             int
	GroupID               int
	Platform              string
	Model                 string
	InputTokens           int
	OutputTokens          int
	CachedInputTokens     int
	CacheCreationTokens   int
	ReasoningOutputTokens int
	InputPrice            float64
	OutputPrice           float64
	CachedInputPrice      float64
	CacheCreationPrice    float64
	InputCost             float64
	OutputCost            float64
	CachedInputCost       float64
	CacheCreationCost     float64
	TotalCost             float64
	ActualCost            float64 // 平台真实成本（扣 reseller 余额）
	BilledCost            float64 // 客户账面消耗（累加到 APIKey.used_quota）
	AccountCost           float64 // 账号实际成本（仅服务"账号计费"统计）
	RateMultiplier        float64 // 快照：本次生效的平台计费倍率
	SellRate              float64 // 快照：本次生效的销售倍率（0 表示客户侧免费，1 表示不加价）
	AccountRateMultiplier float64 // 快照：本次生效的 account_rate
	ServiceTier           string
	Stream                bool
	DurationMs            int64
	FirstTokenMs          int64
	UserAgent             string
	IPAddress             string
	Endpoint              string
	ReasoningEffort       string
	UsageMetadata         map[string]string
}

// Recorder 异步记录器
// 使用 channel 缓冲，goroutine 批量写入
// 每 100 条或每 5 秒 flush 一次
type Recorder struct {
	db           *ent.Client
	rdb          *redis.Client
	ch           chan UsageRecord
	retryCh      chan []UsageRecord
	stopCh       chan struct{}
	stopped      chan struct{}
	retryStopped chan struct{}
	once         sync.Once
}

// NewRecorder 创建使用量记录器
func NewRecorder(db *ent.Client, bufferSize int, rdb ...*redis.Client) *Recorder {
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	var cache *redis.Client
	if len(rdb) > 0 {
		cache = rdb[0]
	}
	return &Recorder{
		db:           db,
		rdb:          cache,
		ch:           make(chan UsageRecord, bufferSize),
		retryCh:      make(chan []UsageRecord, retryQueueSize),
		stopCh:       make(chan struct{}),
		stopped:      make(chan struct{}),
		retryStopped: make(chan struct{}),
	}
}

// Record 提交使用记录（非阻塞）
func (r *Recorder) Record(record UsageRecord) {
	record = ensureBillingEventID(record)
	select {
	case r.ch <- record:
	default:
		slog.Warn("billing_record_buffer_full",
			"user_id", record.UserID,
			"model", record.Model,
		)
		ctx, cancel := context.WithTimeout(context.Background(), syncFallbackTimeout)
		defer cancel()
		if _, err := r.RecordSync(ctx, record); err != nil {
			slog.Error("billing_record_sync_fallback_failed",
				"user_id", record.UserID,
				"model", record.Model,
				"error", err,
			)
		}
	}
}

// RecordSync 同步写入一条使用记录并返回 usage_log.id。
// 需要立即把 usage_id 关联到任务时使用；普通转发仍走异步 Record。
func (r *Recorder) RecordSync(ctx context.Context, record UsageRecord) (int, error) {
	record = ensureBillingEventID(record)
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	inserted, err := r.insertUsageLogs(ctx, tx, []UsageRecord{record})
	if err != nil {
		return 0, fmt.Errorf("插入 UsageLog 失败: %w", err)
	}
	if len(inserted) > 0 {
		insertedBatch := insertedUsageRecords(inserted)
		if err := applyUsageCharges(ctx, tx, insertedBatch); err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("提交事务失败: %w", err)
		}
		r.updateAccountStatsCache(ctx, insertedBatch)
		return inserted[0].ID, nil
	}

	usageID, err := tx.UsageLog.Query().
		Where(usagelog.BillingEventIDEQ(record.BillingEventID)).
		OnlyID(ctx)
	if err != nil {
		return 0, fmt.Errorf("查询已有 UsageLog 失败 billing_event_id=%s: %w", record.BillingEventID, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return usageID, nil
}

// Start 启动后台写入 goroutine
func (r *Recorder) Start() {
	safego.Go("billing_recorder", r.run)
	safego.Go("billing_recorder_retry", r.runRetries)
}

// Stop 停止写入，等待缓冲区清空
func (r *Recorder) Stop() {
	r.once.Do(func() {
		close(r.stopCh)
		<-r.stopped
		close(r.retryCh)
		<-r.retryStopped
	})
}

// run 后台运行循环
func (r *Recorder) run() {
	defer close(r.stopped)

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]UsageRecord, 0, batchSize)
	ctx := context.Background()

	for {
		select {
		case rec := <-r.ch:
			batch = append(batch, rec)
			if len(batch) >= batchSize {
				r.flush(ctx, batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				r.flush(ctx, batch)
				batch = batch[:0]
			}

		case <-r.stopCh:
			// 停止前处理剩余数据
			close(r.ch)
			for rec := range r.ch {
				batch = append(batch, rec)
			}
			if len(batch) > 0 {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
				r.flush(shutdownCtx, batch)
				cancel()
			}
			return
		}
	}
}

// flush 批量写入数据库。首写失败后交给独立 retry worker，避免阻塞唯一消费者。
func (r *Recorder) flush(ctx context.Context, batch []UsageRecord) {
	if len(batch) == 0 {
		return
	}
	ensureBatchBillingEventIDs(batch)
	insertCtx, cancel := withWriteTimeout(ctx)
	defer cancel()
	if err := r.batchInsert(insertCtx, batch); err != nil {
		slog.Error("billing_batch_flush_failed",
			"attempt", 1,
			"count", len(batch),
			"error", err,
		)
		r.enqueueRetry(ctx, batch, err)
		return
	}
	slog.Debug("billing_batch_flush_succeeded", "count", len(batch))
}

func (r *Recorder) enqueueRetry(ctx context.Context, batch []UsageRecord, cause error) {
	retryBatch := append([]UsageRecord(nil), batch...)
	select {
	case r.retryCh <- retryBatch:
	case <-r.stopCh:
		r.deadLetter(retryBatch, "recorder_stopping", cause)
	case <-ctx.Done():
		r.deadLetter(retryBatch, "context_cancelled", ctx.Err())
	default:
		r.deadLetter(retryBatch, "retry_queue_full", cause)
	}
}

func (r *Recorder) runRetries() {
	defer close(r.retryStopped)
	ctx := context.Background()
	for batch := range r.retryCh {
		if err := r.flushRetryBatch(ctx, batch); err != nil {
			r.deadLetter(batch, "retry_exhausted", err)
		}
	}
}

func (r *Recorder) flushRetryBatch(ctx context.Context, batch []UsageRecord) error {
	ensureBatchBillingEventIDs(batch)
	var lastErr error
	for attempt := 2; attempt <= maxFlushAttempts; attempt++ {
		backoff := time.Duration(attempt-1) * time.Second
		if backoff > maxRetryBackoff {
			backoff = maxRetryBackoff
		}
		if err := r.waitRetry(ctx, backoff); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}
		insertCtx, cancel := withWriteTimeout(ctx)
		err := r.batchInsert(insertCtx, batch)
		cancel()
		if err != nil {
			lastErr = err
			slog.Error("billing_batch_flush_failed",
				"attempt", attempt,
				"count", len(batch),
				"error", err,
			)
			if attempt < maxFlushAttempts {
				slog.Warn("billing_batch_flush_retained", "count", len(batch), "next_retry_in", nextRetryBackoff(attempt+1))
			}
			continue
		}
		slog.Debug("billing_batch_flush_succeeded", "attempt", attempt, "count", len(batch))
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("billing batch retry exhausted")
}

// batchInsert 在同一事务中批量写入使用记录并扣费
// 保证 UsageLog 插入与余额扣减的原子性，避免记录成功但扣费失败
func (r *Recorder) batchInsert(ctx context.Context, batch []UsageRecord) error {
	ensureBatchBillingEventIDs(batch)
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer func() {
		// 若事务未提交则回滚（Commit 后 Rollback 是 no-op）
		_ = tx.Rollback()
	}()

	inserted, err := r.insertUsageLogs(ctx, tx, batch)
	if err != nil {
		return fmt.Errorf("批量插入 UsageLog 失败: %w", err)
	}
	if len(inserted) == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交事务失败: %w", err)
		}
		return nil
	}
	insertedBatch := insertedUsageRecords(inserted)

	if err := applyUsageCharges(ctx, tx, insertedBatch); err != nil {
		return err
	}

	// 3. 提交事务
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	r.updateAccountStatsCache(ctx, insertedBatch)
	return nil
}

type insertedUsageLog struct {
	ID     int
	Record UsageRecord
}

func (r *Recorder) insertUsageLogs(ctx context.Context, tx *ent.Tx, batch []UsageRecord) ([]insertedUsageLog, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	dialectName := tx.Driver().Dialect()
	if dialectName != dialect.Postgres && dialectName != dialect.SQLite {
		return nil, fmt.Errorf("unsupported billing insert dialect: %s", dialectName)
	}

	now := time.Now()
	recordsByEvent := make(map[string]UsageRecord, len(batch))
	insert := entsql.Dialect(dialectName).Insert(usagelog.Table).
		Columns(usageLogInsertColumns()...).
		OnConflict(
			entsql.ConflictColumns(usagelog.FieldBillingEventID),
			entsql.DoNothing(),
		).
		Returning(usagelog.FieldID, usagelog.FieldBillingEventID)

	for _, rec := range batch {
		rec = ensureBillingEventID(rec)
		if err := validateUsageRecordForInsert(rec); err != nil {
			return nil, err
		}
		if _, ok := recordsByEvent[rec.BillingEventID]; !ok {
			recordsByEvent[rec.BillingEventID] = rec
		}
		values, err := usageLogInsertValues(rec, now)
		if err != nil {
			return nil, err
		}
		insert.Values(values...)
	}

	query, args := insert.Query()
	var rows entsql.Rows
	if err := tx.Driver().Query(ctx, query, args, &rows); err != nil {
		return nil, err
	}
	defer rows.Close()

	inserted := make([]insertedUsageLog, 0, len(batch))
	for rows.Next() {
		var id int
		var eventID string
		if err := rows.Scan(&id, &eventID); err != nil {
			return nil, err
		}
		rec, ok := recordsByEvent[eventID]
		if !ok {
			return nil, fmt.Errorf("插入 UsageLog 返回未知 billing_event_id=%s", eventID)
		}
		inserted = append(inserted, insertedUsageLog{ID: id, Record: rec})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return inserted, nil
}

func usageLogInsertColumns() []string {
	return []string{
		usagelog.FieldBillingEventID,
		usagelog.FieldPlatform,
		usagelog.FieldModel,
		usagelog.FieldInputTokens,
		usagelog.FieldOutputTokens,
		usagelog.FieldCachedInputTokens,
		usagelog.FieldCacheCreationTokens,
		usagelog.FieldReasoningOutputTokens,
		usagelog.FieldInputPrice,
		usagelog.FieldOutputPrice,
		usagelog.FieldCachedInputPrice,
		usagelog.FieldCacheCreationPrice,
		usagelog.FieldInputCost,
		usagelog.FieldOutputCost,
		usagelog.FieldCachedInputCost,
		usagelog.FieldCacheCreationCost,
		usagelog.FieldTotalCost,
		usagelog.FieldActualCost,
		usagelog.FieldBilledCost,
		usagelog.FieldAccountCost,
		usagelog.FieldRateMultiplier,
		usagelog.FieldSellRate,
		usagelog.FieldAccountRateMultiplier,
		usagelog.FieldServiceTier,
		usagelog.FieldStream,
		usagelog.FieldDurationMs,
		usagelog.FieldFirstTokenMs,
		usagelog.FieldUserAgent,
		usagelog.FieldIPAddress,
		usagelog.FieldEndpoint,
		usagelog.FieldReasoningEffort,
		usagelog.FieldUsageMetadata,
		usagelog.FieldUserIDSnapshot,
		usagelog.FieldUserEmailSnapshot,
		usagelog.FieldCreatedAt,
		usagelog.UserColumn,
		usagelog.APIKeyColumn,
		usagelog.AccountColumn,
		usagelog.GroupColumn,
	}
}

func usageLogInsertValues(rec UsageRecord, now time.Time) ([]any, error) {
	metadata, err := usageMetadataValue(rec.UsageMetadata)
	if err != nil {
		return nil, err
	}
	rateMultiplier := ratevalue.NormalizeMultiplier(rec.RateMultiplier, 1)
	sellRate := ratevalue.NormalizeSellMultiplier(rec.SellRate, 1)
	accountRateMultiplier := ratevalue.NormalizeMultiplier(rec.AccountRateMultiplier, 1)
	return []any{
		rec.BillingEventID,
		rec.Platform,
		rec.Model,
		rec.InputTokens,
		rec.OutputTokens,
		rec.CachedInputTokens,
		rec.CacheCreationTokens,
		rec.ReasoningOutputTokens,
		rec.InputPrice,
		rec.OutputPrice,
		rec.CachedInputPrice,
		rec.CacheCreationPrice,
		rec.InputCost,
		rec.OutputCost,
		rec.CachedInputCost,
		rec.CacheCreationCost,
		rec.TotalCost,
		rec.ActualCost,
		rec.BilledCost,
		rec.AccountCost,
		rateMultiplier,
		sellRate,
		accountRateMultiplier,
		rec.ServiceTier,
		rec.Stream,
		rec.DurationMs,
		rec.FirstTokenMs,
		rec.UserAgent,
		rec.IPAddress,
		rec.Endpoint,
		rec.ReasoningEffort,
		metadata,
		rec.UserID,
		rec.UserEmail,
		now,
		rec.UserID,
		nullablePositiveID(rec.APIKeyID),
		rec.AccountID,
		rec.GroupID,
	}, nil
}

func usageMetadataValue(metadata map[string]string) (any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("序列化 usage_metadata 失败: %w", err)
	}
	return string(raw), nil
}

func nullablePositiveID(id int) any {
	if id <= 0 {
		return nil
	}
	return id
}

func insertedUsageRecords(inserted []insertedUsageLog) []UsageRecord {
	records := make([]UsageRecord, 0, len(inserted))
	for _, item := range inserted {
		records = append(records, item.Record)
	}
	return records
}

func validateUsageRecordForInsert(rec UsageRecord) error {
	if rec.BillingEventID == "" {
		return fmt.Errorf("billing_event_id 不能为空")
	}
	if rec.Platform == "" {
		return fmt.Errorf("platform 不能为空 billing_event_id=%s", rec.BillingEventID)
	}
	if rec.Model == "" {
		return fmt.Errorf("model 不能为空 billing_event_id=%s", rec.BillingEventID)
	}
	return nil
}

func ensureBillingEventID(record UsageRecord) UsageRecord {
	if record.BillingEventID == "" {
		record.BillingEventID = "bill_" + uuid.NewString()
	}
	return record
}

func ensureBatchBillingEventIDs(batch []UsageRecord) {
	for i := range batch {
		batch[i] = ensureBillingEventID(batch[i])
	}
}

func withWriteTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, writeAttemptTimeout)
}

func (r *Recorder) waitRetry(ctx context.Context, backoff time.Duration) error {
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-r.stopCh:
		return errRecorderStopping
	}
}

func nextRetryBackoff(attempt int) time.Duration {
	backoff := time.Duration(attempt-1) * time.Second
	if backoff > maxRetryBackoff {
		return maxRetryBackoff
	}
	return backoff
}

func (r *Recorder) deadLetter(batch []UsageRecord, reason string, err error) {
	slog.Error("billing_batch_dead_letter",
		"reason", reason,
		"count", len(batch),
		"billing_event_ids", billingEventIDs(batch),
		"error", err,
	)
}

func billingEventIDs(batch []UsageRecord) []string {
	ids := make([]string, 0, len(batch))
	for _, rec := range batch {
		ids = append(ids, rec.BillingEventID)
	}
	return ids
}

func (r *Recorder) updateAccountStatsCache(ctx context.Context, batch []UsageRecord) {
	if r.rdb == nil || len(batch) == 0 {
		return
	}
	now := time.Now()
	day := accountcache.Day(now)
	pipe := r.rdb.Pipeline()
	for _, rec := range batch {
		if rec.AccountID <= 0 {
			continue
		}
		tokens := rec.InputTokens + rec.OutputTokens + rec.CachedInputTokens + rec.CacheCreationTokens
		todayKey := accountcache.TodayStatsKey(day)
		pipe.HIncrBy(ctx, todayKey, accountcache.TodayStatsField(rec.AccountID, "requests"), 1)
		if tokens != 0 {
			pipe.HIncrBy(ctx, todayKey, accountcache.TodayStatsField(rec.AccountID, "tokens"), int64(tokens))
		}
		if rec.AccountCost != 0 {
			pipe.HIncrByFloat(ctx, todayKey, accountcache.TodayStatsField(rec.AccountID, "account_cost"), rec.AccountCost)
		}
		if rec.ActualCost != 0 {
			pipe.HIncrByFloat(ctx, todayKey, accountcache.TodayStatsField(rec.AccountID, "user_cost"), rec.ActualCost)
		}
		pipe.HSet(ctx, todayKey, accountcache.TodayStatsField(rec.AccountID, "updated_at"), now.UTC().Format(time.RFC3339))
		pipe.Expire(ctx, todayKey, accountcache.TodayStatsTTL)

		if usagemodel.IsImageGen(rec.Model) {
			totalKey := accountcache.ImageTotalKey(rec.AccountID)
			todayImageKey := accountcache.ImageTodayKey(day, rec.AccountID)
			pipe.Incr(ctx, totalKey)
			pipe.Expire(ctx, totalKey, accountcache.ImageTotalTTL)
			pipe.Incr(ctx, todayImageKey)
			pipe.Expire(ctx, todayImageKey, accountcache.TodayStatsTTL)
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Debug("account_stats_cache_update_failed", "count", len(batch), "error", err)
	}
}

func applyUsageCharges(ctx context.Context, tx *ent.Tx, batch []UsageRecord) error {
	// 在同一事务中扣费 —— 三个独立累加器：
	// - User.balance：按 actual_cost 扣减。
	// - APIKey.used_quota：按 billed_cost 累加。
	// - APIKey.used_quota_actual：按 actual_cost 累加。
	userActualCosts := make(map[int]float64)
	keyBilledCosts := make(map[int]float64)
	keyActualCosts := make(map[int]float64)

	for _, rec := range batch {
		if rec.ActualCost > 0 {
			userActualCosts[rec.UserID] += rec.ActualCost
			if rec.APIKeyID > 0 {
				keyActualCosts[rec.APIKeyID] += rec.ActualCost
			}
		}
		if rec.APIKeyID > 0 && rec.BilledCost > 0 {
			keyBilledCosts[rec.APIKeyID] += rec.BilledCost
		}
	}

	for userID, cost := range userActualCosts {
		if err := tx.User.UpdateOneID(userID).
			AddBalance(-cost).
			Exec(ctx); err != nil {
			return fmt.Errorf("扣减用户余额失败 user_id=%d cost=%.8f: %w", userID, cost, err)
		}
	}

	// APIKey 双累加器：billed 和 actual 都更新（key 集合相同，合并一次 update 调用）
	// APIKeyID == 0 表示插件经 Host 调用发起的请求（无 API Key），跳过 APIKey 累加。
	keyIDs := make(map[int]struct{}, len(keyBilledCosts))
	for k := range keyBilledCosts {
		keyIDs[k] = struct{}{}
	}
	for k := range keyActualCosts {
		keyIDs[k] = struct{}{}
	}
	for keyID := range keyIDs {
		if keyID == 0 {
			continue
		}
		update := tx.APIKey.UpdateOneID(keyID)
		if billed := keyBilledCosts[keyID]; billed > 0 {
			update = update.AddUsedQuota(billed)
		}
		if actual := keyActualCosts[keyID]; actual > 0 {
			update = update.AddUsedQuotaActual(actual)
		}
		if err := update.Exec(ctx); err != nil {
			return fmt.Errorf("更新 API Key 用量失败 key_id=%d: %w", keyID, err)
		}
	}
	return nil
}
