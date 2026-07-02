package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
	entapikey "github.com/DevilGenius/airgate-core/ent/apikey"
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

var (
	recorderGo            = safego.Go
	recorderFlushInterval = flushInterval
	recorderRetryBackoff  = nextRetryBackoff
	recorderMaxAttempts   = maxFlushAttempts
	recorderCommitTx      = func(tx *ent.Tx) error { return tx.Commit() }
	recorderInsertDialect = func(tx *ent.Tx) string { return tx.Driver().Dialect() }
	recorderFindUsageID   = func(ctx context.Context, tx *ent.Tx, eventID string) (int, error) {
		return tx.UsageLog.Query().
			Where(usagelog.BillingEventIDEQ(eventID)).
			OnlyID(ctx)
	}
	recorderQueryUsageInsert = func(ctx context.Context, tx *ent.Tx, query string, args []any) (usageLogRows, error) {
		var rows entsql.Rows
		if err := tx.Driver().Query(ctx, query, args, &rows); err != nil {
			return nil, err
		}
		return &rows, nil
	}
	marshalUsageMetadata = json.Marshal
)

type usageLogRows interface {
	Close() error
	Next() bool
	Scan(dest ...any) error
	Err() error
}

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
	OccurredAt            time.Time // 服务端记录到 usage 的时间，写入 usage_logs.created_at。
}

// APIKeyBalanceAlertInput 是 API Key 剩余额度提醒回调的输入。
type APIKeyBalanceAlertInput struct {
	KeyID      int
	KeyName    string
	UserID     int
	UserEmail  string
	AlertEmail string
	Remaining  float64
	Threshold  float64
	QuotaUSD   float64
	UsedQuota  float64
}

// APIKeyBalanceAlertFunc 发送 API Key 剩余额度提醒。
type APIKeyBalanceAlertFunc func(APIKeyBalanceAlertInput)

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

	apiKeyAlertMu      sync.RWMutex
	apiKeyBalanceAlert APIKeyBalanceAlertFunc
	deadLetterTotal    atomic.Int64
}

// RecorderStats exposes queue counters for runtime monitoring.
type RecorderStats struct {
	QueueLen        int   `json:"queue_len"`
	QueueCap        int   `json:"queue_cap"`
	RetryQueueLen   int   `json:"retry_queue_len"`
	RetryQueueCap   int   `json:"retry_queue_cap"`
	DeadLetterTotal int64 `json:"dead_letter_total"`
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

// SetAPIKeyBalanceAlertCallback 设置 API Key 剩余额度提醒回调。
func (r *Recorder) SetAPIKeyBalanceAlertCallback(fn APIKeyBalanceAlertFunc) {
	r.apiKeyAlertMu.Lock()
	defer r.apiKeyAlertMu.Unlock()
	r.apiKeyBalanceAlert = fn
}

func (r *Recorder) apiKeyBalanceAlertCallback() APIKeyBalanceAlertFunc {
	r.apiKeyAlertMu.RLock()
	defer r.apiKeyAlertMu.RUnlock()
	return r.apiKeyBalanceAlert
}

// Record 提交使用记录（非阻塞）
func (r *Recorder) Record(record UsageRecord) {
	record = ensureBillingEventID(record)
	record = ensureUsageOccurredAt(record, time.Now())
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
	record = ensureUsageOccurredAt(record, time.Now())
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
		if err := upsertUsageHourlyRollups(ctx, tx, inserted); err != nil {
			return 0, fmt.Errorf("更新 UsageLog 聚合失败: %w", err)
		}
		if err := applyUsageCharges(ctx, tx, insertedBatch); err != nil {
			return 0, err
		}
		if err := recorderCommitTx(tx); err != nil {
			return 0, fmt.Errorf("提交事务失败: %w", err)
		}
		r.updateAccountStatsCache(ctx, insertedBatch)
		r.scheduleAPIKeyBalanceAlertCheck(insertedBatch)
		return inserted[0].ID, nil
	}

	usageID, err := recorderFindUsageID(ctx, tx, record.BillingEventID)
	if err != nil {
		return 0, fmt.Errorf("查询已有 UsageLog 失败 billing_event_id=%s: %w", record.BillingEventID, err)
	}
	if err := recorderCommitTx(tx); err != nil {
		return 0, fmt.Errorf("提交事务失败: %w", err)
	}
	return usageID, nil
}

// Start 启动后台写入 goroutine
func (r *Recorder) Start() {
	recorderGo("billing_recorder", r.run)
	recorderGo("billing_recorder_retry", r.runRetries)
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

// Stats returns cheap in-memory queue counters for runtime monitoring.
func (r *Recorder) Stats() RecorderStats {
	if r == nil {
		return RecorderStats{}
	}
	return RecorderStats{
		QueueLen:        len(r.ch),
		QueueCap:        cap(r.ch),
		RetryQueueLen:   len(r.retryCh),
		RetryQueueCap:   cap(r.retryCh),
		DeadLetterTotal: r.deadLetterTotal.Load(),
	}
}

// run 后台运行循环
func (r *Recorder) run() {
	defer close(r.stopped)

	ticker := time.NewTicker(recorderFlushInterval)
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
	for attempt := 2; attempt <= recorderMaxAttempts; attempt++ {
		backoff := recorderRetryBackoff(attempt)
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
		if err := recorderCommitTx(tx); err != nil {
			return fmt.Errorf("提交事务失败: %w", err)
		}
		return nil
	}
	insertedBatch := insertedUsageRecords(inserted)

	if err := upsertUsageHourlyRollups(ctx, tx, inserted); err != nil {
		return fmt.Errorf("更新 UsageLog 聚合失败: %w", err)
	}

	if err := applyUsageCharges(ctx, tx, insertedBatch); err != nil {
		return err
	}

	// 3. 提交事务
	if err := recorderCommitTx(tx); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	r.updateAccountStatsCache(ctx, insertedBatch)
	r.scheduleAPIKeyBalanceAlertCheck(insertedBatch)
	return nil
}

type insertedUsageLog struct {
	ID        int
	Record    UsageRecord
	CreatedAt time.Time
}

func (r *Recorder) insertUsageLogs(ctx context.Context, tx *ent.Tx, batch []UsageRecord) ([]insertedUsageLog, error) {
	if len(batch) == 0 {
		return nil, nil
	}
	dialectName := recorderInsertDialect(tx)
	if dialectName != dialect.Postgres && dialectName != dialect.SQLite {
		return nil, fmt.Errorf("unsupported billing insert dialect: %s", dialectName)
	}

	now := time.Now()
	recordsByEvent := make(map[string]insertedUsageLog, len(batch))
	insert := entsql.Dialect(dialectName).Insert(usagelog.Table).
		Columns(usageLogInsertColumns()...).
		OnConflict(
			entsql.ConflictColumns(usagelog.FieldBillingEventID),
			entsql.DoNothing(),
		).
		Returning(usagelog.FieldID, usagelog.FieldBillingEventID)

	for _, rec := range batch {
		rec = ensureBillingEventID(rec)
		createdAt := usageRecordCreatedAt(rec, now)
		if err := validateUsageRecordForInsert(rec); err != nil {
			return nil, err
		}
		if _, ok := recordsByEvent[rec.BillingEventID]; !ok {
			recordsByEvent[rec.BillingEventID] = insertedUsageLog{Record: rec, CreatedAt: createdAt}
		}
		values, err := usageLogInsertValues(rec, createdAt)
		if err != nil {
			return nil, err
		}
		insert.Values(values...)
	}

	query, args := insert.Query()
	rows, err := recorderQueryUsageInsert(ctx, tx, query, args)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	inserted := make([]insertedUsageLog, 0, len(batch))
	for rows.Next() {
		var id int
		var eventID string
		if err := rows.Scan(&id, &eventID); err != nil {
			return nil, err
		}
		pending, ok := recordsByEvent[eventID]
		if !ok {
			return nil, fmt.Errorf("插入 UsageLog 返回未知 billing_event_id=%s", eventID)
		}
		inserted = append(inserted, insertedUsageLog{ID: id, Record: pending.Record, CreatedAt: pending.CreatedAt})
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

func usageLogInsertValues(rec UsageRecord, createdAt time.Time) ([]any, error) {
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
		createdAt,
		rec.UserID,
		nullablePositiveID(rec.APIKeyID),
		rec.AccountID,
		rec.GroupID,
	}, nil
}

func ensureUsageOccurredAt(record UsageRecord, fallback time.Time) UsageRecord {
	if record.OccurredAt.IsZero() {
		record.OccurredAt = fallback
	}
	return record
}

func usageRecordCreatedAt(record UsageRecord, fallback time.Time) time.Time {
	if record.OccurredAt.IsZero() {
		return fallback
	}
	return record.OccurredAt
}

func usageMetadataValue(metadata map[string]string) (any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	raw, err := marshalUsageMetadata(metadata)
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

type usageHourlyRollupBatchField struct {
	name  string
	cast  string
	value any
}

func upsertUsageHourlyRollups(ctx context.Context, tx *ent.Tx, inserted []insertedUsageLog) error {
	if len(inserted) == 0 || recorderInsertDialect(tx) != dialect.Postgres {
		return nil
	}

	var args []any
	valueRows := make([]string, 0, len(inserted))
	for _, item := range inserted {
		var valueRow string
		args, valueRow = appendUsageHourlyRollupBatchRow(args, item)
		valueRows = append(valueRows, valueRow)
	}

	query := `WITH batch (
` + usageHourlyRollupBatchColumnList() + `
) AS (VALUES ` + strings.Join(valueRows, ",") + `)
INSERT INTO public.usage_hourly_rollups (
	bucket_start,
	user_id,
	user_email,
	model,
	requests,
	input_tokens,
	output_tokens,
	cached_input_tokens,
	cache_creation_tokens,
	actual_cost,
	total_cost,
	image_requests,
	non_image_requests,
	non_image_duration_ms,
	first_token_requests,
	first_token_ms,
	image_duration_ms,
	updated_at
)
SELECT
	date_trunc('hour', created_at),
	user_id,
	COALESCE(MAX(NULLIF(user_email, '')), ''),
	model,
	COUNT(*)::bigint,
	COALESCE(SUM(input_tokens), 0)::bigint,
	COALESCE(SUM(output_tokens), 0)::bigint,
	COALESCE(SUM(cached_input_tokens), 0)::bigint,
	COALESCE(SUM(cache_creation_tokens), 0)::bigint,
	COALESCE(SUM(actual_cost), 0),
	COALESCE(SUM(total_cost), 0),
	COALESCE(SUM(CASE WHEN is_image THEN 1 ELSE 0 END), 0)::bigint,
	COALESCE(SUM(CASE WHEN NOT is_image THEN 1 ELSE 0 END), 0)::bigint,
	COALESCE(SUM(CASE WHEN NOT is_image THEN duration_ms ELSE 0 END), 0)::bigint,
	COALESCE(SUM(CASE WHEN NOT is_image AND first_token_ms > 0 THEN 1 ELSE 0 END), 0)::bigint,
	COALESCE(SUM(CASE WHEN NOT is_image AND first_token_ms > 0 THEN first_token_ms ELSE 0 END), 0)::bigint,
	COALESCE(SUM(CASE WHEN is_image THEN duration_ms ELSE 0 END), 0)::bigint,
	now()
FROM batch
GROUP BY 1, user_id, model
ON CONFLICT (bucket_start, user_id, model) DO UPDATE SET
	user_email = CASE
		WHEN EXCLUDED.user_email <> '' THEN EXCLUDED.user_email
		ELSE public.usage_hourly_rollups.user_email
	END,
	requests = public.usage_hourly_rollups.requests + EXCLUDED.requests,
	input_tokens = public.usage_hourly_rollups.input_tokens + EXCLUDED.input_tokens,
	output_tokens = public.usage_hourly_rollups.output_tokens + EXCLUDED.output_tokens,
	cached_input_tokens = public.usage_hourly_rollups.cached_input_tokens + EXCLUDED.cached_input_tokens,
	cache_creation_tokens = public.usage_hourly_rollups.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
	actual_cost = public.usage_hourly_rollups.actual_cost + EXCLUDED.actual_cost,
	total_cost = public.usage_hourly_rollups.total_cost + EXCLUDED.total_cost,
	image_requests = public.usage_hourly_rollups.image_requests + EXCLUDED.image_requests,
	non_image_requests = public.usage_hourly_rollups.non_image_requests + EXCLUDED.non_image_requests,
	non_image_duration_ms = public.usage_hourly_rollups.non_image_duration_ms + EXCLUDED.non_image_duration_ms,
	first_token_requests = public.usage_hourly_rollups.first_token_requests + EXCLUDED.first_token_requests,
	first_token_ms = public.usage_hourly_rollups.first_token_ms + EXCLUDED.first_token_ms,
	image_duration_ms = public.usage_hourly_rollups.image_duration_ms + EXCLUDED.image_duration_ms,
	updated_at = now()`

	var result entsql.Result
	if err := tx.Driver().Exec(ctx, query, args, &result); err != nil {
		if isUsageHourlyRollupMissing(err) {
			return nil
		}
		return err
	}
	return nil
}

func isUsageHourlyRollupMissing(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42P01"
}

func appendUsageHourlyRollupBatchRow(args []any, item insertedUsageLog) ([]any, string) {
	fields := usageHourlyRollupBatchFields(item)
	placeholders := make([]string, 0, len(fields))
	for _, field := range fields {
		args = append(args, field.value)
		placeholders = append(placeholders, fmt.Sprintf("$%d::%s", len(args), field.cast))
	}
	return args, "(" + strings.Join(placeholders, ",") + ")"
}

func usageHourlyRollupBatchColumnList() string {
	fields := usageHourlyRollupBatchFields(insertedUsageLog{})
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
		columns = append(columns, "\t"+field.name)
	}
	return strings.Join(columns, ",\n")
}

func usageHourlyRollupBatchFields(item insertedUsageLog) []usageHourlyRollupBatchField {
	rec := item.Record
	return []usageHourlyRollupBatchField{
		{name: "created_at", cast: "timestamptz", value: item.CreatedAt},
		{name: "user_id", cast: "integer", value: rec.UserID},
		{name: "user_email", cast: "text", value: rec.UserEmail},
		{name: "model", cast: "text", value: rec.Model},
		{name: "input_tokens", cast: "bigint", value: rec.InputTokens},
		{name: "output_tokens", cast: "bigint", value: rec.OutputTokens},
		{name: "cached_input_tokens", cast: "bigint", value: rec.CachedInputTokens},
		{name: "cache_creation_tokens", cast: "bigint", value: rec.CacheCreationTokens},
		{name: "actual_cost", cast: "numeric", value: rec.ActualCost},
		{name: "total_cost", cast: "numeric", value: rec.TotalCost},
		{name: "duration_ms", cast: "bigint", value: rec.DurationMs},
		{name: "first_token_ms", cast: "bigint", value: rec.FirstTokenMs},
		{name: "is_image", cast: "boolean", value: usagemodel.IsImageGen(rec.Model)},
	}
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
	r.deadLetterTotal.Add(int64(len(batch)))
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

func (r *Recorder) scheduleAPIKeyBalanceAlertCheck(batch []UsageRecord) {
	callback := r.apiKeyBalanceAlertCallback()
	if callback == nil {
		return
	}
	keyIDs := apiKeyBalanceAlertCandidateIDs(batch)
	if len(keyIDs) == 0 {
		return
	}
	recorderGo("api_key_balance_alert_check", func() {
		ctx, cancel := context.WithTimeout(context.Background(), writeAttemptTimeout)
		defer cancel()
		r.checkAPIKeyBalanceAlerts(ctx, keyIDs, callback)
	})
}

func apiKeyBalanceAlertCandidateIDs(batch []UsageRecord) []int {
	seen := make(map[int]struct{})
	ids := make([]int, 0, len(batch))
	for _, rec := range batch {
		if rec.APIKeyID <= 0 || rec.BilledCost <= 0 {
			continue
		}
		if _, ok := seen[rec.APIKeyID]; ok {
			continue
		}
		seen[rec.APIKeyID] = struct{}{}
		ids = append(ids, rec.APIKeyID)
	}
	return ids
}

func (r *Recorder) checkAPIKeyBalanceAlerts(ctx context.Context, keyIDs []int, callback APIKeyBalanceAlertFunc) {
	keys, err := r.db.APIKey.Query().
		Where(entapikey.IDIn(keyIDs...)).
		WithUser().
		All(ctx)
	if err != nil {
		slog.Error("api_key_balance_alert_load_failed", "key_count", len(keyIDs), "error", err)
		return
	}
	for _, key := range keys {
		alertEmail := strings.TrimSpace(key.BalanceAlertEmail)
		threshold := key.BalanceAlertThreshold
		remaining := key.QuotaUsd - key.UsedQuota
		alertConfigured := key.BalanceAlertEnabled && alertEmail != "" && threshold > 0 && key.QuotaUsd > 0
		if key.BalanceAlertNotified && (!alertConfigured || remaining > threshold) {
			if err := r.db.APIKey.UpdateOneID(key.ID).SetBalanceAlertNotified(false).Exec(ctx); err != nil {
				slog.Debug("api_key_balance_alert_reset_failed", "key_id", key.ID, "error", err)
			}
			continue
		}
		if !alertConfigured || remaining > threshold || key.BalanceAlertNotified {
			continue
		}
		affected, err := r.db.APIKey.Update().
			Where(
				entapikey.IDEQ(key.ID),
				entapikey.BalanceAlertEnabledEQ(true),
				entapikey.BalanceAlertEmailNEQ(""),
				entapikey.BalanceAlertThresholdGT(0),
				entapikey.QuotaUsdGT(0),
				entapikey.BalanceAlertNotifiedEQ(false),
			).
			SetBalanceAlertNotified(true).
			Save(ctx)
		if err != nil {
			slog.Error("api_key_balance_alert_mark_failed", "key_id", key.ID, "error", err)
			continue
		}
		if affected == 0 {
			continue
		}
		input := APIKeyBalanceAlertInput{
			KeyID:      key.ID,
			KeyName:    key.Name,
			AlertEmail: alertEmail,
			Remaining:  remaining,
			Threshold:  threshold,
			QuotaUSD:   key.QuotaUsd,
			UsedQuota:  key.UsedQuota,
		}
		if key.Edges.User != nil {
			input.UserID = key.Edges.User.ID
			input.UserEmail = key.Edges.User.Email
		}
		callback(input)
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
