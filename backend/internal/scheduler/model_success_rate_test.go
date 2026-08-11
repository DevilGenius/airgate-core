package scheduler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

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
	bucketID := now.Unix() / int64(modelSuccessRateBucketWidth/time.Second)
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

	stats, ok := tracker.Get(7, model)
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

	stats, ok := tracker.Get(7, "gpt-5.4")
	if !ok {
		t.Fatal("Get returned no model statistics")
	}
	if stats.Successes != 1 || stats.Failures != 1 || stats.InvalidRequests != 1 || stats.ValidRequests != 2 || stats.Requests != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.SuccessRate != 0.5 {
		t.Fatalf("success rate = %v, want 0.5", stats.SuccessRate)
	}
	apiStats := tracker.List(7)
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
	window := tracker.Window()
	if window.BucketCount != 48 || window.BucketSeconds != int64(30*time.Minute/time.Second) {
		t.Fatalf("window = %+v", window)
	}

	now = now.Add(25 * time.Hour)
	stats, ok = tracker.Get(7, "gpt-5.4")
	if !ok {
		t.Fatal("Get after window advance returned no model statistics")
	}
	if stats.Successes != 0 || stats.Failures != 0 || stats.ValidRequests != 0 || stats.Requests != 0 || stats.SuccessRate != 0 {
		t.Fatalf("expired stats = %+v", stats)
	}
}

func TestModelSuccessRateTrackerUsesThirtyMinuteBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 1, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }

	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	now = time.Date(2026, 8, 11, 12, 29, 59, 0, time.UTC)
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})
	now = time.Date(2026, 8, 11, 12, 30, 0, 0, time.UTC)
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

func TestModelSuccessRateTrackerPrunesExpiredSeries(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tracker := NewModelSuccessRateTracker(nil)
	tracker.now = func() time.Time { return now }
	tracker.Record(7, "gpt-5.4", sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess})

	now = now.Add(25 * time.Hour)
	tracker.pruneExpired(now)
	if _, ok := tracker.Get(7, "gpt-5.4"); ok {
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

	// Mark account 7 as restored too, so Forget covers every account-level map.
	tracker.applyPersistedSnapshot(7, []byte(`{"models":[]}`))
	tracker.Forget(7)

	if _, ok := tracker.Get(7, "gpt-5.4"); ok {
		t.Fatal("forgotten account still has model statistics")
	}
	if got := tracker.List(7); len(got) != 0 {
		t.Fatalf("forgotten account list = %+v, want empty", got)
	}
	_, shard := tracker.shardFor(7)
	shard.mu.RLock()
	_, hasSeries := shard.series[7]
	_, isDirty := shard.dirty[7]
	_, isRestored := shard.restored[7]
	shard.mu.RUnlock()
	if hasSeries || isDirty || isRestored {
		t.Fatalf("forgotten account remains in memory: series=%v dirty=%v restored=%v", hasSeries, isDirty, isRestored)
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
			_, _ = tracker.Get(accountID, "gpt-5.4")
			accountID++
			if accountID > 64 {
				accountID = 1
			}
		}
	})
}

func BenchmarkModelSuccessRateList(b *testing.B) {
	tracker := NewModelSuccessRateTracker(nil)
	outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}
	for modelID := 0; modelID < 32; modelID++ {
		tracker.Record(1, "model-"+strconv.Itoa(modelID), outcome)
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		_ = tracker.List(1)
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
