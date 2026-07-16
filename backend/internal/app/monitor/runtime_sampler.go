package monitor

import (
	"context"
	stdsql "database/sql"
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/pkg/usagemodel"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
)

const (
	defaultRuntimeSampleInterval     = 5 * time.Second
	defaultLatencySampleInterval     = 30 * time.Second
	defaultRuntimeLatencyWindow      = 5 * time.Minute
	defaultRuntimeLatencyLongWindow  = time.Hour
	defaultDependencyPingTimeout     = 300 * time.Millisecond
	defaultRuntimeQueryTimeout       = time.Second
	defaultRuntimeCapacityTimeout    = 500 * time.Millisecond
	defaultRuntimeSafetyCacheTimeout = 500 * time.Millisecond
)

// RuntimeSampler periodically collects low-cost operational signals and keeps
// the latest snapshot in memory for the admin monitor API.
type RuntimeSampler struct {
	sqlDB       *stdsql.DB
	rdb         *redis.Client
	scheduler   *scheduler.Scheduler
	concurrency *scheduler.ConcurrencyManager
	recorder    *billing.Recorder
	monitor     *Service
	safetyCache SafetyCacheStatsReader

	sampleInterval  time.Duration
	latencyInterval time.Duration
	latencyWindow   time.Duration

	cpuSampler *processCPUSampler
	snapshot   atomic.Value

	lastPostgresWaitCount      int64
	lastPostgresWaitDurationNS int64
	lastRedisTimeouts          int64
	lastConcurrencyRejectTotal int64
	lastWaiterRejectTotal      int64
	postgresWaitCountReady     bool
	postgresWaitDurationReady  bool
	redisDeltaReady            bool
	concurrencyDeltaReady      bool
	waiterDeltaReady           bool
}

// SafetyCacheStatsReader provides runtime cache usage without coupling the
// monitor package to the plugin manager implementation.
type SafetyCacheStatsReader interface {
	SafetyCacheStats(ctx context.Context) (textSize, textCapacity, imageSize, imageCapacity int, err error)
}

// RuntimeSnapshot is the complete payload returned by /admin/monitor/runtime.
type RuntimeSnapshot struct {
	SampledAt     time.Time              `json:"sampled_at"`
	WindowSeconds int                    `json:"window_seconds"`
	Latency       RuntimeLatencyStats    `json:"latency"`
	Latency1H     RuntimeLatencyStats    `json:"latency_1h"`
	Capacity      RuntimeCapacityStats   `json:"capacity"`
	Dependencies  RuntimeDependencyStats `json:"dependencies"`
	Runtime       RuntimeProcessStats    `json:"runtime"`
}

type RuntimeLatencyStats struct {
	SampleCount        int64   `json:"sample_count"`
	TextSampleCount    int64   `json:"text_sample_count"`
	ImageSampleCount   int64   `json:"image_sample_count"`
	FRTAvgMS           int64   `json:"frt_avg_ms"`
	FRTP50MS           int64   `json:"frt_p50_ms"`
	FRTP95MS           int64   `json:"frt_p95_ms"`
	FRTP99MS           int64   `json:"frt_p99_ms"`
	ImageDurationP50MS int64   `json:"image_duration_p50_ms"`
	ImageDurationP95MS int64   `json:"image_duration_p95_ms"`
	ImageDurationP99MS int64   `json:"image_duration_p99_ms"`
	ErrorRate          float64 `json:"error_rate"`
	ErrorCount         int64   `json:"error_count"`
	TextErrorRate      float64 `json:"text_error_rate"`
	TextErrorCount     int64   `json:"text_error_count"`
	ImageErrorRate     float64 `json:"image_error_rate"`
	ImageErrorCount    int64   `json:"image_error_count"`
	Stale              bool    `json:"stale"`
	LastError          string  `json:"last_error,omitempty"`
}

type RuntimeCapacityStats struct {
	AccountInUse           int   `json:"account_in_use"`
	AccountCapacity        int   `json:"account_capacity"`
	WorkingAccounts        int   `json:"working_accounts"`
	MessageWaiters         int   `json:"message_waiters"`
	MaxAccountWaiters      int   `json:"max_account_waiters"`
	WaitingAccounts        int   `json:"waiting_accounts"`
	ConcurrencyRejectDelta int64 `json:"concurrency_reject_delta"`
	QueueFullDelta         int64 `json:"queue_full_delta"`
}

type RuntimeDependencyStats struct {
	Postgres RuntimePostgresStats `json:"postgres"`
	Redis    RuntimeRedisStats    `json:"redis"`
}

type RuntimePostgresStats struct {
	Healthy             bool   `json:"healthy"`
	PingMS              int64  `json:"ping_ms"`
	Open                int    `json:"open"`
	Active              int    `json:"active"`
	Idle                int    `json:"idle"`
	MaxOpen             int    `json:"max_open"`
	WaitCountDelta      int64  `json:"wait_count_delta"`
	WaitDurationMSDelta int64  `json:"wait_duration_ms_delta"`
	LastError           string `json:"last_error,omitempty"`
}

type RuntimeRedisStats struct {
	Healthy      bool   `json:"healthy"`
	PingMS       int64  `json:"ping_ms"`
	Total        int    `json:"total"`
	Active       int    `json:"active"`
	Idle         int    `json:"idle"`
	TimeoutDelta int64  `json:"timeout_delta"`
	LastError    string `json:"last_error,omitempty"`
}

type RuntimeProcessStats struct {
	CPUPercent       *float64 `json:"cpu_percent,omitempty"`
	HeapAllocBytes   uint64   `json:"heap_alloc_bytes"`
	SysBytes         uint64   `json:"sys_bytes"`
	Goroutines       int      `json:"goroutines"`
	BillingQueueLen  int      `json:"billing_queue_len"`
	BillingQueueCap  int      `json:"billing_queue_cap"`
	BillingRetryLen  int      `json:"billing_retry_len"`
	BillingRetryCap  int      `json:"billing_retry_cap"`
	BillingDeadTotal int64    `json:"billing_dead_letter_total"`
	MonitorQueueLen  int      `json:"monitor_queue_len"`
	MonitorQueueCap  int      `json:"monitor_queue_cap"`
	MonitorDropped   int64    `json:"monitor_dropped_total"`
	MonitorQueued    int64    `json:"monitor_queued_total"`
	MonitorFlushed   int64    `json:"monitor_flushed_total"`
	TextSafetyLen    int      `json:"text_safety_cache_len"`
	TextSafetyCap    int      `json:"text_safety_cache_cap"`
	ImageSafetyLen   int      `json:"image_safety_cache_len"`
	ImageSafetyCap   int      `json:"image_safety_cache_cap"`
}

// NewRuntimeSampler creates a runtime sampler. A nil dependency simply yields
// unknown/zero values for that section.
func NewRuntimeSampler(sqlDB *stdsql.DB, rdb *redis.Client, sched *scheduler.Scheduler, concurrency *scheduler.ConcurrencyManager, recorder *billing.Recorder, monitor *Service) *RuntimeSampler {
	s := &RuntimeSampler{
		sqlDB:           sqlDB,
		rdb:             rdb,
		scheduler:       sched,
		concurrency:     concurrency,
		recorder:        recorder,
		monitor:         monitor,
		sampleInterval:  defaultRuntimeSampleInterval,
		latencyInterval: defaultLatencySampleInterval,
		latencyWindow:   defaultRuntimeLatencyWindow,
		cpuSampler:      newProcessCPUSampler(),
	}
	s.snapshot.Store(RuntimeSnapshot{
		WindowSeconds: int(s.latencyWindow / time.Second),
		Latency: RuntimeLatencyStats{
			Stale: true,
		},
		Latency1H: RuntimeLatencyStats{
			Stale: true,
		},
	})
	return s
}

// SetSafetyCacheStatsReader injects the plugin runtime metrics reader.
func (s *RuntimeSampler) SetSafetyCacheStatsReader(reader SafetyCacheStatsReader) {
	if s != nil {
		s.safetyCache = reader
	}
}

// Start runs the sampler loop until ctx is cancelled.
func (s *RuntimeSampler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.sampleRuntime(ctx)
	s.sampleLatency(ctx)

	sampleTicker := time.NewTicker(s.sampleInterval)
	defer sampleTicker.Stop()
	latencyTicker := time.NewTicker(s.latencyInterval)
	defer latencyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sampleTicker.C:
			s.sampleRuntime(ctx)
		case <-latencyTicker.C:
			s.sampleLatency(ctx)
		}
	}
}

// Snapshot returns the latest in-memory runtime snapshot.
func (s *RuntimeSampler) Snapshot() RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{WindowSeconds: int(defaultRuntimeLatencyWindow / time.Second)}
	}
	value := s.snapshot.Load()
	if snap, ok := value.(RuntimeSnapshot); ok {
		return snap
	}
	return RuntimeSnapshot{WindowSeconds: int(s.latencyWindow / time.Second)}
}

func (s *RuntimeSampler) sampleRuntime(parent context.Context) {
	snap := s.Snapshot()
	snap.SampledAt = time.Now().UTC()
	snap.WindowSeconds = int(s.latencyWindow / time.Second)
	snap.Capacity = s.sampleCapacity(parent)
	snap.Dependencies = s.sampleDependencies(parent)
	snap.Runtime = s.sampleProcess(parent)
	s.snapshot.Store(snap)
}

func (s *RuntimeSampler) sampleLatency(parent context.Context) {
	snap := s.Snapshot()
	snap.SampledAt = time.Now().UTC()
	snap.WindowSeconds = int(s.latencyWindow / time.Second)

	latency, longLatency, err := s.queryLatencyWindows(parent)
	if err != nil {
		snap.Latency.Stale = true
		snap.Latency.LastError = truncateRuntimeError(err.Error())
		snap.Latency1H.Stale = true
		snap.Latency1H.LastError = truncateRuntimeError(err.Error())
		s.snapshot.Store(snap)
		return
	}
	snap.Latency = latency
	snap.Latency1H = longLatency
	s.snapshot.Store(snap)
}

type runtimeLatencyAggregate struct {
	sampleCount              int64
	textSampleCount          int64
	imageSampleCount         int64
	frtAvg                   float64
	frtPercentiles           pq.Float64Array
	imageDurationPercentiles pq.Float64Array
	errorCount               int64
	textErrorCount           int64
	imageErrorCount          int64
}

func (s *RuntimeSampler) queryLatencyWindows(parent context.Context) (RuntimeLatencyStats, RuntimeLatencyStats, error) {
	if s.sqlDB == nil {
		unavailable := RuntimeLatencyStats{Stale: true, LastError: "postgres unavailable"}
		return unavailable, unavailable, nil
	}

	now := time.Now()
	shortSince := now.Add(-s.latencyWindow)
	longSince := now.Add(-defaultRuntimeLatencyLongWindow)
	imageModelPattern := usagemodel.ImagePrefix + "%"
	var short, long runtimeLatencyAggregate

	if err := s.queryUsageLatencyWindows(parent, longSince, shortSince, imageModelPattern, &short, &long); err != nil {
		return RuntimeLatencyStats{}, RuntimeLatencyStats{}, err
	}
	if err := s.queryLatencyErrorWindows(parent, longSince, shortSince, imageModelPattern, &short, &long); err != nil {
		return RuntimeLatencyStats{}, RuntimeLatencyStats{}, err
	}

	return short.stats(), long.stats(), nil
}

func (s *RuntimeSampler) queryUsageLatencyWindows(
	parent context.Context,
	longSince time.Time,
	shortSince time.Time,
	imageModelPattern string,
	short *runtimeLatencyAggregate,
	long *runtimeLatencyAggregate,
) error {
	ctx, cancel := context.WithTimeout(parent, defaultRuntimeQueryTimeout)
	defer cancel()

	return s.sqlDB.QueryRowContext(ctx, `
SELECT
	COUNT(*) FILTER (WHERE created_at >= $2)::bigint,
	COUNT(*) FILTER (WHERE created_at >= $2 AND model NOT LIKE $3)::bigint,
	COUNT(*) FILTER (WHERE created_at >= $2 AND model LIKE $3)::bigint,
	COALESCE(AVG(first_event_ms) FILTER (WHERE created_at >= $2 AND first_event_ms > 0 AND model NOT LIKE $3), 0)::double precision,
	COALESCE(
		percentile_cont(ARRAY[0.50, 0.95, 0.99]::double precision[])
			WITHIN GROUP (ORDER BY first_event_ms)
			FILTER (WHERE created_at >= $2 AND first_event_ms > 0 AND model NOT LIKE $3),
		ARRAY[0, 0, 0]::double precision[]
	),
	COALESCE(
		percentile_cont(ARRAY[0.50, 0.95, 0.99]::double precision[])
			WITHIN GROUP (ORDER BY duration_ms)
			FILTER (WHERE created_at >= $2 AND duration_ms > 0 AND model LIKE $3),
		ARRAY[0, 0, 0]::double precision[]
	),
	COUNT(*)::bigint,
	COUNT(*) FILTER (WHERE model NOT LIKE $3)::bigint,
	COUNT(*) FILTER (WHERE model LIKE $3)::bigint,
	COALESCE(AVG(first_event_ms) FILTER (WHERE first_event_ms > 0 AND model NOT LIKE $3), 0)::double precision,
	COALESCE(
		percentile_cont(ARRAY[0.50, 0.95, 0.99]::double precision[])
			WITHIN GROUP (ORDER BY first_event_ms)
			FILTER (WHERE first_event_ms > 0 AND model NOT LIKE $3),
		ARRAY[0, 0, 0]::double precision[]
	),
	COALESCE(
		percentile_cont(ARRAY[0.50, 0.95, 0.99]::double precision[])
			WITHIN GROUP (ORDER BY duration_ms)
			FILTER (WHERE duration_ms > 0 AND model LIKE $3),
		ARRAY[0, 0, 0]::double precision[]
	)
FROM usage_logs
WHERE created_at >= $1
`, longSince, shortSince, imageModelPattern).Scan(
		&short.sampleCount,
		&short.textSampleCount,
		&short.imageSampleCount,
		&short.frtAvg,
		&short.frtPercentiles,
		&short.imageDurationPercentiles,
		&long.sampleCount,
		&long.textSampleCount,
		&long.imageSampleCount,
		&long.frtAvg,
		&long.frtPercentiles,
		&long.imageDurationPercentiles,
	)
}

func (s *RuntimeSampler) queryLatencyErrorWindows(
	parent context.Context,
	longSince time.Time,
	shortSince time.Time,
	imageModelPattern string,
	short *runtimeLatencyAggregate,
	long *runtimeLatencyAggregate,
) error {
	ctx, cancel := context.WithTimeout(parent, defaultRuntimeQueryTimeout)
	defer cancel()

	return s.sqlDB.QueryRowContext(ctx, `
SELECT
	COUNT(*) FILTER (WHERE created_at >= $2)::bigint,
	COUNT(*) FILTER (WHERE created_at >= $2 AND model NOT LIKE $3)::bigint,
	COUNT(*) FILTER (WHERE created_at >= $2 AND model LIKE $3)::bigint,
	COUNT(*)::bigint,
	COUNT(*) FILTER (WHERE model NOT LIKE $3)::bigint,
	COUNT(*) FILTER (WHERE model LIKE $3)::bigint
FROM monitor_request_events
WHERE created_at >= $1 AND type NOT IN ('client_closed_request', 'plugin_forward_retry')
`, longSince, shortSince, imageModelPattern).Scan(
		&short.errorCount,
		&short.textErrorCount,
		&short.imageErrorCount,
		&long.errorCount,
		&long.textErrorCount,
		&long.imageErrorCount,
	)
}

func (a runtimeLatencyAggregate) stats() RuntimeLatencyStats {
	return RuntimeLatencyStats{
		SampleCount:        a.sampleCount,
		TextSampleCount:    a.textSampleCount,
		ImageSampleCount:   a.imageSampleCount,
		FRTAvgMS:           roundMS(a.frtAvg),
		FRTP50MS:           roundMS(runtimePercentile(a.frtPercentiles, 0)),
		FRTP95MS:           roundMS(runtimePercentile(a.frtPercentiles, 1)),
		FRTP99MS:           roundMS(runtimePercentile(a.frtPercentiles, 2)),
		ImageDurationP50MS: roundMS(runtimePercentile(a.imageDurationPercentiles, 0)),
		ImageDurationP95MS: roundMS(runtimePercentile(a.imageDurationPercentiles, 1)),
		ImageDurationP99MS: roundMS(runtimePercentile(a.imageDurationPercentiles, 2)),
		ErrorRate:          runtimeErrorRate(a.sampleCount, a.errorCount),
		ErrorCount:         a.errorCount,
		TextErrorRate:      runtimeErrorRate(a.textSampleCount, a.textErrorCount),
		TextErrorCount:     a.textErrorCount,
		ImageErrorRate:     runtimeErrorRate(a.imageSampleCount, a.imageErrorCount),
		ImageErrorCount:    a.imageErrorCount,
		Stale:              false,
	}
}

func runtimePercentile(values pq.Float64Array, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func runtimeErrorRate(sampleCount, errorCount int64) float64 {
	total := sampleCount + errorCount
	if total <= 0 {
		return 0
	}
	return float64(errorCount) / float64(total)
}

func (s *RuntimeSampler) sampleCapacity(parent context.Context) RuntimeCapacityStats {
	stats := RuntimeCapacityStats{}

	if s.concurrency != nil {
		ctx, cancel := context.WithTimeout(parent, defaultRuntimeCapacityTimeout)
		working := s.concurrency.GetWorkingCounts(ctx)
		cancel()
		stats.WorkingAccounts = len(working)
		for _, current := range working {
			stats.AccountInUse += current
		}
		currentRejectTotal := s.concurrency.RejectTotal()
		stats.ConcurrencyRejectDelta = s.deltaInt64(currentRejectTotal, &s.lastConcurrencyRejectTotal, &s.concurrencyDeltaReady)
	}

	stats.AccountCapacity = s.queryAccountCapacity(parent)
	if s.scheduler != nil {
		ctx, cancel := context.WithTimeout(parent, defaultRuntimeCapacityTimeout)
		queueStats := s.scheduler.MessageQueueStats(ctx)
		cancel()
		stats.MessageWaiters = queueStats.WaitersTotal
		stats.MaxAccountWaiters = queueStats.MaxAccountWaiters
		stats.WaitingAccounts = queueStats.WaitingAccounts
		stats.QueueFullDelta = s.deltaInt64(queueStats.WaiterRejectTotal, &s.lastWaiterRejectTotal, &s.waiterDeltaReady)
	}
	return stats
}

func (s *RuntimeSampler) queryAccountCapacity(parent context.Context) int {
	if s.sqlDB == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(parent, defaultRuntimeCapacityTimeout)
	defer cancel()

	var capacity stdsql.NullInt64
	if err := s.sqlDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(max_concurrency), 0)::bigint
FROM accounts
WHERE state = 'active' AND max_concurrency > 0
`).Scan(&capacity); err != nil || !capacity.Valid {
		return 0
	}
	return int(capacity.Int64)
}

func (s *RuntimeSampler) sampleDependencies(parent context.Context) RuntimeDependencyStats {
	return RuntimeDependencyStats{
		Postgres: s.samplePostgres(parent),
		Redis:    s.sampleRedis(parent),
	}
}

func (s *RuntimeSampler) samplePostgres(parent context.Context) RuntimePostgresStats {
	stats := RuntimePostgresStats{}
	if s.sqlDB == nil {
		stats.LastError = "postgres unavailable"
		return stats
	}

	dbStats := s.sqlDB.Stats()
	stats.Open = dbStats.OpenConnections
	stats.Active = dbStats.InUse
	stats.Idle = dbStats.Idle
	stats.MaxOpen = dbStats.MaxOpenConnections
	stats.WaitCountDelta = s.deltaInt64(dbStats.WaitCount, &s.lastPostgresWaitCount, &s.postgresWaitCountReady)
	waitDurationNS := dbStats.WaitDuration.Nanoseconds()
	waitDurationDeltaNS := s.deltaInt64(waitDurationNS, &s.lastPostgresWaitDurationNS, &s.postgresWaitDurationReady)
	stats.WaitDurationMSDelta = waitDurationDeltaNS / int64(time.Millisecond)

	ctx, cancel := context.WithTimeout(parent, defaultDependencyPingTimeout)
	start := time.Now()
	err := s.sqlDB.PingContext(ctx)
	stats.PingMS = elapsedMS(start)
	cancel()
	if err != nil {
		stats.LastError = truncateRuntimeError(err.Error())
		return stats
	}
	stats.Healthy = true
	return stats
}

func (s *RuntimeSampler) sampleRedis(parent context.Context) RuntimeRedisStats {
	stats := RuntimeRedisStats{}
	if s.rdb == nil {
		stats.LastError = "redis unavailable"
		return stats
	}

	poolStats := s.rdb.PoolStats()
	if poolStats != nil {
		stats.Total = int(poolStats.TotalConns)
		stats.Idle = int(poolStats.IdleConns)
		stats.Active = stats.Total - stats.Idle
		if stats.Active < 0 {
			stats.Active = 0
		}
		stats.TimeoutDelta = s.deltaInt64(int64(poolStats.Timeouts), &s.lastRedisTimeouts, &s.redisDeltaReady)
	}

	ctx, cancel := context.WithTimeout(parent, defaultDependencyPingTimeout)
	start := time.Now()
	err := s.rdb.Ping(ctx).Err()
	stats.PingMS = elapsedMS(start)
	cancel()
	if err != nil {
		stats.LastError = truncateRuntimeError(err.Error())
		return stats
	}
	stats.Healthy = true
	return stats
}

func (s *RuntimeSampler) sampleProcess(parent context.Context) RuntimeProcessStats {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	billingStats := billing.RecorderStats{}
	if s.recorder != nil {
		billingStats = s.recorder.Stats()
	}
	monitorStats := RuntimeStats{}
	if s.monitor != nil {
		monitorStats = s.monitor.RuntimeStats()
	}
	cpuPercent, _ := s.cpuSampler.Percent()
	textSafetyLen, textSafetyCap := 0, 0
	imageSafetyLen, imageSafetyCap := 0, 0
	if s.safetyCache != nil {
		ctx, cancel := context.WithTimeout(parent, defaultRuntimeSafetyCacheTimeout)
		textSize, textCapacity, imageSize, imageCapacity, err := s.safetyCache.SafetyCacheStats(ctx)
		cancel()
		if err == nil {
			textSafetyLen = textSize
			textSafetyCap = textCapacity
			imageSafetyLen = imageSize
			imageSafetyCap = imageCapacity
		}
	}

	return RuntimeProcessStats{
		CPUPercent:       cpuPercent,
		HeapAllocBytes:   mem.HeapAlloc,
		SysBytes:         mem.Sys,
		Goroutines:       runtime.NumGoroutine(),
		BillingQueueLen:  billingStats.QueueLen,
		BillingQueueCap:  billingStats.QueueCap,
		BillingRetryLen:  billingStats.RetryQueueLen,
		BillingRetryCap:  billingStats.RetryQueueCap,
		BillingDeadTotal: billingStats.DeadLetterTotal,
		MonitorQueueLen:  monitorStats.QueueLen,
		MonitorQueueCap:  monitorStats.QueueCap,
		MonitorDropped:   monitorStats.DroppedTotal,
		MonitorQueued:    monitorStats.QueuedTotal,
		MonitorFlushed:   monitorStats.FlushedTotal,
		TextSafetyLen:    textSafetyLen,
		TextSafetyCap:    textSafetyCap,
		ImageSafetyLen:   imageSafetyLen,
		ImageSafetyCap:   imageSafetyCap,
	}
}

func (s *RuntimeSampler) deltaInt64(current int64, previous *int64, ready *bool) int64 {
	if !*ready {
		*previous = current
		*ready = true
		return 0
	}
	delta := current - *previous
	*previous = current
	if delta < 0 {
		return 0
	}
	return delta
}

func roundMS(value float64) int64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return int64(math.Round(value))
}

func elapsedMS(start time.Time) int64 {
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func truncateRuntimeError(value string) string {
	const limit = 240
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
