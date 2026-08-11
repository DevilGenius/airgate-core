package scheduler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"

	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func modelSuccessRateStatsForTest(tb testing.TB, tracker *ModelSuccessRateTracker, accountID int, model string) (ModelSuccessRateStats, bool) {
	tb.Helper()
	model = normalizeSuccessRateModel(model)
	stats, _ := tracker.Snapshot(accountID)
	for _, stat := range stats {
		if stat.Model == model {
			return stat, true
		}
	}
	return ModelSuccessRateStats{}, false
}

func TestClassifyModelSuccessRateOutcome(t *testing.T) {
	tests := []struct {
		name    string
		outcome sdk.ForwardOutcome
		want    modelSuccessRateDisposition
	}{
		{
			name:    "success",
			outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess},
			want:    modelSuccessRateSuccess,
		},
		{
			name: "forbidden invalid",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeAccountUnavailable,
				Reason:   "HTTP 403: 访问被拒绝，账号可能已被禁用或无权限 (HTTP 403)",
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusForbidden},
			},
			want: modelSuccessRateInvalid,
		},
		{
			name: "forbidden reason invalid without status",
			outcome: sdk.ForwardOutcome{
				Kind:   sdk.OutcomeAccountUnavailable,
				Reason: "HTTP 403: 访问被拒绝，账号可能已被禁用或无权限 (HTTP 403)",
			},
			want: modelSuccessRateInvalid,
		},
		{
			name: "overloaded 502 failure",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeUnknown,
				Reason:   "Our servers are currently overloaded. Please try again later.",
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
			},
			want: modelSuccessRateFailure,
		},
		{
			name: "overloaded 502 reason failure without status",
			outcome: sdk.ForwardOutcome{
				Kind:   sdk.OutcomeUnknown,
				Reason: "502 overloaded: Our servers are currently overloaded. Please try again later.",
			},
			want: modelSuccessRateFailure,
		},
		{
			name: "upstream transient failure",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeUpstreamTransient,
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
			},
			want: modelSuccessRateFailure,
		},
		{
			name:    "client error invalid",
			outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeClientError},
			want:    modelSuccessRateInvalid,
		},
		{
			name:    "rate limited invalid without status",
			outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited},
			want:    modelSuccessRateInvalid,
		},
		{
			name: "generic 4xx invalid",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeUnknown,
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusUnprocessableEntity},
			},
			want: modelSuccessRateInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyModelSuccessRateOutcome(tt.outcome); got != tt.want {
				t.Fatalf("classification = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestModelSuccessRateTrackerAppliesPersistedSnapshotAndLocalDelta(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 30, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }

	model := "gpt-5.4"
	bucketID := modelSuccessRateBucketID(7, now)
	tracker.Record(7, model, sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	payload, err := json.Marshal(persistedModelSuccessRate{
		Models: []persistedModelSuccessRateItem{{
			Model:       model,
			LastUpdated: now.UnixMilli(),
			Buckets: []persistedModelSuccessRateBucket{{
				Bucket:  bucketID,
				Success: 3,
				Failure: 2,
				Invalid: 4,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal persisted snapshot: %v", err)
	}
	tracker.applyPersistedSnapshot(7, payload)

	stats, ok := modelSuccessRateStatsForTest(t, tracker, 7, model)
	if !ok {
		t.Fatal("Get returned no model statistics")
	}
	if stats.Successes != 4 || stats.Failures != 2 || stats.InvalidRequests != 4 || stats.ValidRequests != 6 || stats.Requests != 10 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.SuccessRate != float64(4)/6 {
		t.Fatalf("success rate = %v, want %v", stats.SuccessRate, float64(4)/6)
	}
}

func TestModelSuccessRateTrackerRecordsFixedBuckets(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 30, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }

	tracker.Record(7, " GPT-5.4 ", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{
		Kind:     sdk.OutcomeAccountUnavailable,
		Upstream: sdk.UpstreamResponse{StatusCode: http.StatusForbidden},
	})

	stats, ok := modelSuccessRateStatsForTest(t, tracker, 7, "gpt-5.4")
	if !ok {
		t.Fatal("Get returned no model statistics")
	}
	if stats.Successes != 1 || stats.Failures != 1 || stats.InvalidRequests != 1 || stats.ValidRequests != 2 || stats.Requests != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.SuccessRate != 0.5 {
		t.Fatalf("success rate = %v, want 0.5", stats.SuccessRate)
	}
	apiStats, window := tracker.Snapshot(7)
	if len(apiStats) != 1 || len(apiStats[0].Buckets) != 1 {
		t.Fatalf("api stats = %+v", apiStats)
	}
	latest := apiStats[0].Buckets[0]
	if latest.Requests != 3 || latest.ValidRequests != 2 || latest.InvalidRequests != 1 {
		t.Fatalf("latest bucket = %+v", latest)
	}
	if latest.Index != 47 {
		t.Fatalf("latest bucket index = %d, want 47", latest.Index)
	}
	if window.BucketCount != 48 || window.BucketSeconds != int64(30*time.Minute/time.Second) {
		t.Fatalf("window = %+v", window)
	}

	now = now.Add(25 * time.Hour)
	stats, ok = modelSuccessRateStatsForTest(t, tracker, 7, "gpt-5.4")
	if !ok {
		t.Fatal("Get after window advance returned no model statistics")
	}
	if stats.Successes != 0 || stats.Failures != 0 || stats.ValidRequests != 0 || stats.Requests != 0 || stats.SuccessRate != 0 {
		t.Fatalf("expired stats = %+v", stats)
	}
}

func TestModelSuccessRateTrackerCurrentBucketHonorsThresholdAndMinimumSamples(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }

	for index := 0; index < modelSuccessRateMinBucketValidRequests-1; index++ {
		tracker.Record(7, "gpt-5", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
	}
	if tracker.isDemotedAt(7, "gpt-5", 0.9, tracker.now()) {
		t.Fatal("current bucket with insufficient samples was demoted")
	}

	tracker.Record(7, "gpt-5", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
	if !tracker.isDemotedAt(7, "gpt-5", 0.9, tracker.now()) {
		t.Fatal("current bucket below threshold was not demoted")
	}
	if tracker.isDemotedAt(7, "gpt-5", 0, tracker.now()) {
		t.Fatal("zero threshold should disable model downgrade")
	}
	if tracker.isDemotedAt(7, "gpt-5", 1.1, tracker.now()) {
		t.Fatal("invalid threshold should disable model downgrade")
	}
}

func TestModelSuccessRateTrackerDowngradeIsPerExactModel(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }

	for index := 0; index < modelSuccessRateMinBucketValidRequests-1; index++ {
		tracker.Record(7, "gpt-image-1", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
	}
	for index := 0; index < modelSuccessRateMinBucketValidRequests; index++ {
		tracker.Record(7, "gpt-image-2", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
	}
	if tracker.isDemotedAt(7, "gpt-image-1", 0.9, tracker.now()) {
		t.Fatal("samples from another model were incorrectly aggregated")
	}
	if !tracker.isDemotedAt(7, "gpt-image-2", 0.9, tracker.now()) {
		t.Fatal("exact model with enough failures was not demoted")
	}
}

func TestModelSuccessRateTrackerDowngradeExpiresAtBucketBoundary(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }
	for index := 0; index < modelSuccessRateMinBucketValidRequests; index++ {
		tracker.Record(7, "gpt-5", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
	}
	if !tracker.isDemotedAt(7, "gpt-5", 0.9, tracker.now()) {
		t.Fatal("model should be demoted in the bucket containing its failures")
	}

	now = now.Add(modelSuccessRateBucketWidth)
	if tracker.isDemotedAt(7, "gpt-5", 0.9, tracker.now()) {
		t.Fatal("model downgrade continued into an empty next bucket")
	}
}

func TestModelSuccessRateTrackerThresholdChangeReevaluatesCurrentBucket(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }
	for index := 0; index < modelSuccessRateMinBucketValidRequests-1; index++ {
		tracker.Record(7, "gpt-5", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	}
	tracker.Record(7, "gpt-5", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})

	if !tracker.isDemotedAt(7, "gpt-5", 0.95, tracker.now()) {
		t.Fatal("higher threshold should demote the current bucket")
	}
	if tracker.isDemotedAt(7, "gpt-5", 0.8, tracker.now()) {
		t.Fatal("lower threshold should immediately recover the current bucket")
	}
}

func TestModelSuccessRateSnapshotUsesOneClockSample(t *testing.T) {
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	currentBucket := modelSuccessRateBucketID(7, base)
	beforeBoundary := modelSuccessRateBucketTime(7, currentBucket+1).Add(-time.Second)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return beforeBoundary }
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})

	calls := 0
	tracker.now = func() time.Time {
		calls++
		if calls == 1 {
			return beforeBoundary
		}
		return beforeBoundary.Add(time.Second)
	}
	rates, window := tracker.Snapshot(7)
	if calls != 1 {
		t.Fatalf("now calls = %d, want 1", calls)
	}
	if len(rates) != 1 {
		t.Fatalf("rates = %+v, want one model", rates)
	}
	if !rates[0].WindowStart.Equal(window.WindowStart) || !rates[0].WindowEnd.Equal(window.WindowEnd) {
		t.Fatalf("rate window = [%s, %s), timeline = [%s, %s)",
			rates[0].WindowStart, rates[0].WindowEnd, window.WindowStart, window.WindowEnd)
	}
}

func TestModelSuccessRateTrackerUsesThirtyMinuteBoundaries(t *testing.T) {
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	bucketStart := modelSuccessRateBucketTime(7, modelSuccessRateBucketID(7, base))
	now := bucketStart.Add(time.Second)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }

	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	now = bucketStart.Add(modelSuccessRateBucketWidth - time.Second)
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	now = bucketStart.Add(modelSuccessRateBucketWidth)
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})

	_, shard := tracker.shardFor(7)
	shard.mu.RLock()
	series := shard.series[7]["gpt-5.4"]
	nonEmpty := 0
	for _, bucket := range series.buckets {
		if bucket.success+bucket.failure+bucket.invalid > 0 {
			nonEmpty++
		}
	}
	shard.mu.RUnlock()
	if nonEmpty != 2 {
		t.Fatalf("non-empty buckets = %d, want 2", nonEmpty)
	}
}

func TestModelSuccessRateBucketOffsetsSpreadAccounts(t *testing.T) {
	if first, second := modelSuccessRateBucketOffset(1), modelSuccessRateBucketOffset(2); first == second {
		t.Fatalf("account offsets should differ: account1=%d account2=%d", first, second)
	}
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	firstWindow := modelSuccessRateWindowInfo(1, now)
	secondWindow := modelSuccessRateWindowInfo(2, now)
	if firstWindow.WindowEnd.Equal(secondWindow.WindowEnd) {
		t.Fatalf("account bucket boundaries were not spread: %s", firstWindow.WindowEnd)
	}
	if firstWindow.WindowEnd.Sub(firstWindow.WindowStart) != modelSuccessRateWindow ||
		secondWindow.WindowEnd.Sub(secondWindow.WindowStart) != modelSuccessRateWindow {
		t.Fatalf("spread windows must remain 24h: first=%s second=%s",
			firstWindow.WindowEnd.Sub(firstWindow.WindowStart),
			secondWindow.WindowEnd.Sub(secondWindow.WindowStart))
	}
}

func TestModelSuccessRateTrackerPrunesExpiredSeries(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})

	now = now.Add(25 * time.Hour)
	tracker.pruneExpired(now)
	if _, ok := modelSuccessRateStatsForTest(t, tracker, 7, "gpt-5.4"); ok {
		t.Fatal("expired model series still present")
	}
	_, shard := tracker.shardFor(7)
	shard.mu.RLock()
	seriesCount := len(shard.series[7])
	shard.mu.RUnlock()
	if seriesCount != 0 {
		t.Fatalf("expired series count = %d", seriesCount)
	}
}

func TestModelSuccessRateTrackerForgetClearsMemoryAndDirtyState(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	tracker.Record(8, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})

	tracker.Forget(7)

	if _, ok := modelSuccessRateStatsForTest(t, tracker, 7, "gpt-5.4"); ok {
		t.Fatal("forgotten account still has model statistics")
	}
	if got, _ := tracker.Snapshot(7); len(got) != 0 {
		t.Fatalf("forgotten account list = %+v, want empty", got)
	}
	_, shard := tracker.shardFor(7)
	shard.mu.RLock()
	_, hasSeries := shard.series[7]
	_, isDirty := shard.dirty[7]
	shard.mu.RUnlock()
	if hasSeries || isDirty {
		t.Fatalf("forgotten account remains in memory: series=%v dirty=%v", hasSeries, isDirty)
	}

	snapshots := tracker.dirtySnapshots()
	for _, snapshot := range snapshots {
		if snapshot.accountID == 7 {
			t.Fatal("forgotten account still has a dirty snapshot")
		}
	}
	if len(snapshots) != 1 || snapshots[0].accountID != 8 {
		t.Fatalf("dirty snapshots = %+v, want only account 8", snapshots)
	}
}

func TestModelSuccessRateRestoreUsesLocalDeduplicationSet(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	rdb, mock := redismock.NewClientMock()
	tracker := NewModelSuccessRateTracker(rdb)
	tracker.now = func() time.Time { return now }
	bucketID := modelSuccessRateBucketID(7, now)
	payload, err := json.Marshal(persistedModelSuccessRate{Models: []persistedModelSuccessRateItem{{
		Model:       "gpt-5.4",
		LastUpdated: now.UnixMilli(),
		Buckets: []persistedModelSuccessRateBucket{{
			Bucket:  bucketID,
			Success: 2,
		}},
	}}})
	if err != nil {
		t.Fatalf("marshal persisted snapshot: %v", err)
	}

	key := accountcache.ModelStatsKey(7)
	mock.ExpectGet(key).SetVal(string(payload))
	restored := make(map[int]struct{})
	if !tracker.restoreKeys(t.Context(), []string{key}, restored) {
		t.Fatal("first restoreKeys returned false")
	}
	if !tracker.restoreKeys(t.Context(), []string{key}, restored) {
		t.Fatal("duplicate restoreKeys returned false")
	}
	stats, ok := modelSuccessRateStatsForTest(t, tracker, 7, "gpt-5.4")
	if !ok || stats.Successes != 2 {
		t.Fatalf("restored stats = %+v ok=%v, want two successes once", stats, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func TestSchedulerOnAccountsDeletedOwnsModelStatsCleanup(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	tracker := NewModelSuccessRateTracker(rdb)
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	scheduler := &Scheduler{rdb: rdb, modelSuccessRate: tracker}

	mock.ExpectDel(accountcache.ModelStatsKey(7)).SetVal(1)
	scheduler.OnAccountsDeleted([]int{0, 7})

	if _, ok := modelSuccessRateStatsForTest(t, tracker, 7, "gpt-5.4"); ok {
		t.Fatal("deleted account still has in-memory model statistics")
	}
	if snapshots := tracker.dirtySnapshots(); len(snapshots) != 0 {
		t.Fatalf("dirty snapshots after account deletion = %+v, want empty", snapshots)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("redis expectations: %v", err)
	}
}

func BenchmarkModelSuccessRateRecord(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		accountID := 1
		for pb.Next() {
			tracker.Record(accountID, "gpt-5.4", outcome)
			accountID++
			if accountID > 64 {
				accountID = 1
			}
		}
	})
}

func BenchmarkModelSuccessRateRecordWithLargeBody(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{
		Kind:     sdk.OutcomeSuccess,
		Upstream: sdk.UpstreamResponse{Body: bytes.Repeat([]byte("model response "), 16*1024)},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		tracker.Record(1, "gpt-5.4", outcome)
	}
}

func BenchmarkModelSuccessRateGet(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}
	for accountID := 1; accountID <= 64; accountID++ {
		tracker.Record(accountID, "gpt-5.4", outcome)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		accountID := 1
		for pb.Next() {
			_, _ = modelSuccessRateStatsForTest(b, tracker, accountID, "gpt-5.4")
			accountID++
			if accountID > 64 {
				accountID = 1
			}
		}
	})
}

func BenchmarkModelSuccessRateIsDemotedCurrentBucket(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient}
	for accountID := 1; accountID <= 64; accountID++ {
		for index := 0; index < modelSuccessRateMinBucketValidRequests; index++ {
			tracker.Record(accountID, "gpt-5.4", outcome)
		}
	}
	normalizedModel := normalizeSuccessRateModel("gpt-5.4")
	now := tracker.now()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		accountID := 1
		for pb.Next() {
			_ = tracker.isDemotedAt(accountID, normalizedModel, 0.9, now)
			accountID++
			if accountID > 64 {
				accountID = 1
			}
		}
	})
}

func BenchmarkModelSuccessRateSnapshot(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}
	for modelID := 0; modelID < 32; modelID++ {
		tracker.Record(1, "model-"+strconv.Itoa(modelID), outcome)
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_, _ = tracker.Snapshot(1)
	}
}

func BenchmarkModelSuccessRateDirtySnapshots(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}
	for accountID := 1; accountID <= 64; accountID++ {
		for modelID := 0; modelID < 16; modelID++ {
			tracker.Record(accountID, "model-"+strconv.Itoa(modelID), outcome)
		}
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_ = tracker.dirtySnapshots()
	}
}
