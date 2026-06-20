package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
)

func TestStickySessionAdditionalMemoryAndRedisBranches(t *testing.T) {
	ctx := context.Background()
	var nilSticky *StickySession
	if id, ok := nilSticky.Get(ctx, 1, "openai", "s"); ok || id != 0 {
		t.Fatalf("nil sticky get = %d %v", id, ok)
	}
	nilSticky.Set(ctx, 1, "openai", "s", 1)

	sticky := NewStickySession(nil)
	if id, ok := sticky.getMemory(""); ok || id != 0 {
		t.Fatalf("empty memory key = %d %v", id, ok)
	}
	key := stickyKey(1, "openai", "s1")
	sticky.items[key] = stickyBinding{accountID: 7, expiresAt: time.Now().Add(-time.Second)}
	if id, ok := sticky.getMemory(key); ok || id != 0 {
		t.Fatalf("expired memory binding = %d %v", id, ok)
	}
	if _, exists := sticky.items[key]; exists {
		t.Fatal("expired sticky binding should be deleted")
	}
	sticky.items[key] = stickyBinding{accountID: 0, expiresAt: time.Now().Add(time.Hour)}
	if id, ok := sticky.getMemory(key); ok || id != 0 {
		t.Fatalf("invalid account memory binding = %d %v", id, ok)
	}

	sticky.setMemoryWithRedisRefreshAfter("", 1, time.Time{})
	sticky.setMemoryWithRedisRefreshAfter("valid", 8, time.Time{})
	if id, ok := sticky.getMemory("valid"); !ok || id != 8 {
		t.Fatalf("setMemoryWithRedisRefreshAfter zero refresh = %d %v", id, ok)
	}
	if sticky.refreshMemory("", 1) || sticky.refreshMemory("valid", 0) {
		t.Fatal("invalid refreshMemory should return false")
	}
	if !sticky.refreshMemory("new", 9) {
		t.Fatal("new sticky memory should refresh redis")
	}
	if sticky.refreshMemory("new", 9) {
		t.Fatal("same sticky memory before refresh due should not refresh redis")
	}
	sticky.items["new"] = stickyBinding{accountID: 9, expiresAt: time.Now().Add(time.Hour), redisRefreshAfter: time.Now().Add(-time.Second)}
	if !sticky.refreshMemory("new", 9) {
		t.Fatal("past refresh due should refresh redis")
	}
	if !sticky.refreshMemory("new", 10) {
		t.Fatal("account change should refresh redis")
	}

	sticky.lastCleanupTime = time.Now().Add(-2 * stickyCleanupInterval)
	sticky.items["expired"] = stickyBinding{accountID: 1, expiresAt: time.Now().Add(-time.Second)}
	sticky.items["live"] = stickyBinding{accountID: 2, expiresAt: time.Now().Add(time.Hour)}
	sticky.cleanupMemory(time.Now())
	if _, ok := sticky.items["expired"]; ok {
		t.Fatal("cleanupMemory should delete expired sticky binding")
	}
	if _, ok := sticky.items["live"]; !ok {
		t.Fatal("cleanupMemory should keep live sticky binding")
	}
	sticky.cleanupMemory(time.Now())

	items := map[string]stickyBinding{
		"expired": {accountID: 1, expiresAt: time.Now().Add(-time.Second)},
		"live":    {accountID: 2, expiresAt: time.Now().Add(time.Hour)},
	}
	deleteOneExpiredOrArbitrarySticky(items, time.Now())
	if _, ok := items["expired"]; ok {
		t.Fatalf("expired arbitrary delete map = %+v", items)
	}
	deleteOneExpiredOrArbitrarySticky(items, time.Now())
	if len(items) != 0 {
		t.Fatalf("arbitrary sticky delete map = %+v", items)
	}

	rdb, mock := redismock.NewClientMock()
	sticky = NewStickySession(rdb)
	redisKey := stickyKey(2, "openai", "session")
	mock.ExpectGet(redisKey).SetVal("42")
	if id, ok := sticky.Get(ctx, 2, "openai", "session"); !ok || id != 42 {
		t.Fatalf("redis sticky get = %d %v", id, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sticky get expectations: %v", err)
	}

	rdb, mock = redismock.NewClientMock()
	sticky = NewStickySession(rdb)
	mock.ExpectGet(redisKey).SetVal("bad")
	if id, ok := sticky.Get(ctx, 2, "openai", "session"); ok || id != 0 {
		t.Fatalf("bad redis sticky get = %d %v", id, ok)
	}
	mock.ExpectGet(redisKey).SetErr(redis.Nil)
	if id, ok := sticky.Get(ctx, 2, "openai", "session"); ok || id != 0 {
		t.Fatalf("missing redis sticky get = %d %v", id, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sticky bad/missing expectations: %v", err)
	}
}

func TestResponseAffinityAdditionalMemoryAndRedisBranches(t *testing.T) {
	ctx := context.Background()
	var nilAffinity *ResponseAffinity
	nilAffinity.Bind(ctx, 1, "openai", "resp", 1)
	if id, ok := nilAffinity.Get(ctx, 1, "openai", "resp"); ok || id != 0 {
		t.Fatalf("nil affinity get = %d %v", id, ok)
	}
	nilAffinity.Refresh(ctx, 1, "openai", "resp", 1)

	affinity := NewResponseAffinity(nil)
	if key := responseAffinityKey(5, " openai ", " resp "); key != "ag:affinity:response:5:openai:resp" {
		t.Fatalf("response affinity key = %q", key)
	}
	affinity.setMemoryWithRedisRefreshAfter("", 1, time.Time{})
	affinity.setMemoryWithRedisRefreshAfter("valid", 4, time.Time{})
	if id, ok := affinity.getMemory("valid"); !ok || id != 4 {
		t.Fatalf("affinity memory = %d %v", id, ok)
	}
	affinity.items["expired"] = responseAffinityBinding{accountID: 4, expiresAt: time.Now().Add(-time.Second)}
	if id, ok := affinity.getMemory("expired"); ok || id != 0 {
		t.Fatalf("expired affinity = %d %v", id, ok)
	}
	affinity.items["invalid"] = responseAffinityBinding{accountID: 0, expiresAt: time.Now().Add(time.Hour)}
	if id, ok := affinity.getMemory("invalid"); ok || id != 0 {
		t.Fatalf("invalid affinity = %d %v", id, ok)
	}
	if affinity.refreshMemory("", 1) || affinity.refreshMemory("valid", 0) {
		t.Fatal("invalid affinity refresh should return false")
	}
	if !affinity.refreshMemory("new", 5) {
		t.Fatal("new affinity memory should refresh redis")
	}
	if affinity.refreshMemory("new", 5) {
		t.Fatal("same affinity before refresh due should not refresh redis")
	}
	affinity.items["new"] = responseAffinityBinding{accountID: 5, expiresAt: time.Now().Add(time.Hour), redisRefreshAfter: time.Now().Add(-time.Second)}
	if !affinity.refreshMemory("new", 5) {
		t.Fatal("past affinity refresh due should refresh redis")
	}
	affinity.lastCleanupTime = time.Now().Add(-2 * responseAffinityCleanupInterval)
	affinity.items["expired-cleanup"] = responseAffinityBinding{accountID: 1, expiresAt: time.Now().Add(-time.Second)}
	affinity.cleanupMemory(time.Now())
	if _, ok := affinity.items["expired-cleanup"]; ok {
		t.Fatal("cleanupMemory should delete expired response affinity")
	}
	items := map[string]responseAffinityBinding{
		"expired": {accountID: 1, expiresAt: time.Now().Add(-time.Second)},
		"live":    {accountID: 2, expiresAt: time.Now().Add(time.Hour)},
	}
	deleteOneExpiredOrArbitraryResponseAffinity(items, time.Now())
	if _, ok := items["expired"]; ok {
		t.Fatalf("expired affinity delete map = %+v", items)
	}
	deleteOneExpiredOrArbitraryResponseAffinity(items, time.Now())
	if len(items) != 0 {
		t.Fatalf("arbitrary affinity delete map = %+v", items)
	}

	rdb, mock := redismock.NewClientMock()
	affinity = NewResponseAffinity(rdb)
	redisKey := responseAffinityKey(6, "openai", "resp")
	mock.ExpectGet(redisKey).SetVal("44")
	if id, ok := affinity.Get(ctx, 6, "openai", "resp"); !ok || id != 44 {
		t.Fatalf("redis affinity get = %d %v", id, ok)
	}
	mock.ExpectGet(responseAffinityKey(6, "openai", "bad")).SetVal("bad")
	if id, ok := affinity.Get(ctx, 6, "openai", "bad"); ok || id != 0 {
		t.Fatalf("bad redis affinity get = %d %v", id, ok)
	}
	mock.ExpectGet(responseAffinityKey(6, "openai", "missing")).SetErr(redis.Nil)
	if id, ok := affinity.Get(ctx, 6, "openai", "missing"); ok || id != 0 {
		t.Fatalf("missing redis affinity get = %d %v", id, ok)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("affinity redis expectations: %v", err)
	}
}

func TestStateHelperAdditionalBranches(t *testing.T) {
	calledCritical := 0
	calledSnapshot := 0
	sm := &StateMachine{
		onCriticalTransition: func(int) { calledCritical++ },
		onStateSnapshotUpdated: func(int, entaccount.State, *time.Time, map[string]interface{}) {
			calledSnapshot++
		},
	}
	sm.notifyCritical(1)
	sm.notifyStateSnapshot(1, entaccount.StateActive, nil, nil)
	if calledCritical != 1 || calledSnapshot != 1 {
		t.Fatalf("callbacks critical=%d snapshot=%d", calledCritical, calledSnapshot)
	}
	(&StateMachine{}).notifyCritical(1)
	(&StateMachine{}).notifyStateSnapshot(1, entaccount.StateActive, nil, nil)

	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	if got := timePtrRFC3339(nil); got != "" {
		t.Fatalf("nil time = %q", got)
	}
	zero := time.Time{}
	if got := timePtrRFC3339(&zero); got != "" {
		t.Fatalf("zero time = %q", got)
	}
	if got := timePtrRFC3339(&now); got != now.Format(time.RFC3339) {
		t.Fatalf("time string = %q", got)
	}

	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	active := &ent.Account{State: entaccount.StateActive}
	disabled := &ent.Account{State: entaccount.StateDisabled}
	rateFuture := &ent.Account{State: entaccount.StateRateLimited, StateUntil: &future}
	ratePast := &ent.Account{State: entaccount.StateRateLimited, StateUntil: &past}
	degradedFuture := &ent.Account{State: entaccount.StateDegraded, StateUntil: &future}
	degradedPast := &ent.Account{State: entaccount.StateDegraded, StateUntil: &past}
	if isUnexpiredTemporaryState(nil, now) || isUnexpiredTemporaryState(active, now) || !isUnexpiredTemporaryState(rateFuture, now) {
		t.Fatal("isUnexpiredTemporaryState returned unexpected values")
	}
	if isExpiredTemporaryState(nil, now) || isExpiredTemporaryState(rateFuture, now) || !isExpiredTemporaryState(ratePast, now) {
		t.Fatal("isExpiredTemporaryState returned unexpected values")
	}
	if isTransientAvoidanceWindow(degradedFuture, now) {
		t.Fatal("degraded without marker should not be transient avoidance")
	}
	degradedFuture.Extra = map[string]interface{}{transientAvoidStepExtraKey: 1}
	if !isTransientAvoidanceWindow(degradedFuture, now) || isTransientAvoidanceWindow(degradedPast, now) {
		t.Fatal("transient avoidance window mismatch")
	}
	if schedulabilityWithTransientAvoidance(degradedFuture, now) != NotSchedulable ||
		hardAffinitySchedulabilityWithTransientAvoidance(degradedFuture, now) != NotSchedulable {
		t.Fatal("transient avoidance should block sticky schedulability")
	}
	if SchedulabilityOf(active, now) != Normal || SchedulabilityOf(disabled, now) != NotSchedulable ||
		SchedulabilityOf(rateFuture, now) != NotSchedulable || SchedulabilityOf(ratePast, now) != Normal ||
		SchedulabilityOf(degradedFuture, now) != StickyOnly || SchedulabilityOf(degradedPast, now) != Normal ||
		SchedulabilityOf(&ent.Account{State: entaccount.State("unknown")}, now) != NotSchedulable {
		t.Fatal("SchedulabilityOf returned unexpected values")
	}
	if !strings.HasPrefix(truncateReason(strings.Repeat("x", 600)), strings.Repeat("x", 500)) ||
		len(truncateReason(strings.Repeat("x", 600))) != 500 {
		t.Fatal("truncateReason did not clamp to 500 bytes")
	}
	for _, tt := range []struct {
		value any
		want  int
	}{
		{int32(3), 3},
		{int64(4), 4},
		{float64(5.9), 5},
		{float32(6.9), 6},
		{"bad", 0},
	} {
		if got := extraInt(map[string]interface{}{"v": tt.value}, "v"); got != tt.want {
			t.Fatalf("extraInt(%T) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestSchedulabilityExtraAdditionalBranches(t *testing.T) {
	extra := map[string]interface{}{
		"string": "value",
		"f64":    float64(1.25),
		"int":    2,
		"i64":    int64(3),
		"bool":   true,
		"true":   " true ",
		"bad":    "not-bool",
		"zero":   0,
		"one64":  int64(1),
		"fzero":  float64(0),
	}
	if ExtraString(extra, "string") != "value" || ExtraString(extra, "missing") != "" || ExtraString(extra, "int") != "" {
		t.Fatal("ExtraString mismatch")
	}
	if ExtraFloat64(extra, "f64") != 1.25 || ExtraFloat64(extra, "int") != 2 || ExtraFloat64(extra, "i64") != 3 || ExtraFloat64(extra, "string") != 0 {
		t.Fatal("ExtraFloat64 mismatch")
	}
	if ExtraInt(extra, "f64") != 1 || ExtraInt(extra, "int") != 2 || ExtraInt(extra, "i64") != 3 || ExtraInt(extra, "string") != 0 {
		t.Fatal("ExtraInt mismatch")
	}
	if !ExtraBool(extra, "bool") || !ExtraBool(extra, "true") || ExtraBool(extra, "bad") ||
		ExtraBool(extra, "zero") || !ExtraBool(extra, "one64") || ExtraBool(extra, "fzero") || ExtraBool(extra, "missing") {
		t.Fatal("ExtraBool mismatch")
	}
}
