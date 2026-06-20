package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type facadeRPMTracker struct {
	incrementCalls int
	decrementCalls int
	tryCalls       int
	tryAllowed     bool
	err            error
}

func (r *facadeRPMTracker) IncrementRPM(context.Context, int) (int, error) {
	r.incrementCalls++
	return r.incrementCalls, r.err
}

func (r *facadeRPMTracker) TryIncrementRPM(context.Context, int, int) (bool, error) {
	r.tryCalls++
	return r.tryAllowed, r.err
}

func (r *facadeRPMTracker) DecrementRPM(context.Context, int) {
	r.decrementCalls++
}

func (r *facadeRPMTracker) GetSchedulability(context.Context, int, int) Schedulability {
	return Normal
}

type facadeSessionTracker struct {
	refreshCalls  int
	registerCalls int
	lastIdle      time.Duration
	allowed       bool
	err           error
}

func (s *facadeSessionTracker) RefreshSession(_ context.Context, _ int, _ string, idleTimeout time.Duration) error {
	s.refreshCalls++
	s.lastIdle = idleTimeout
	return s.err
}

func (s *facadeSessionTracker) RegisterSession(_ context.Context, _ int, _ string, _ int, idleTimeout time.Duration) (bool, error) {
	s.registerCalls++
	s.lastIdle = idleTimeout
	return s.allowed, s.err
}

func (s *facadeSessionTracker) GetSchedulability(context.Context, int, map[string]interface{}) Schedulability {
	return Normal
}

type facadeWindowCostTracker struct {
	adds []float64
}

func (w *facadeWindowCostTracker) GetSchedulability(context.Context, int, map[string]interface{}) Schedulability {
	return Normal
}

func (w *facadeWindowCostTracker) AddCost(_ context.Context, _ int, cost float64) {
	w.adds = append(w.adds, cost)
}

type facadeFamilyCooldownTracker struct {
	entries []FamilyCooldownEntry
	batch   map[int][]FamilyCooldownEntry
	clear   int
}

func (f *facadeFamilyCooldownTracker) Until(context.Context, int, string) (time.Time, bool) {
	return time.Time{}, false
}

func (f *facadeFamilyCooldownTracker) List(context.Context, int) []FamilyCooldownEntry {
	return f.entries
}

func (f *facadeFamilyCooldownTracker) ListBatch(context.Context, []int) map[int][]FamilyCooldownEntry {
	return f.batch
}

func (f *facadeFamilyCooldownTracker) ClearAccount(context.Context, int) int {
	return f.clear
}

func TestNewSchedulerAndMonitorRecorderGuards(t *testing.T) {
	var nilScheduler *Scheduler
	nilScheduler.SetMonitorRecorder(&captureMonitorRecorder{})

	s := NewScheduler(nil, nil)
	if s == nil || s.sticky == nil || s.windowCost == nil || s.rpm == nil || s.session == nil ||
		s.msgQueue == nil || s.state == nil || s.familyCooldown == nil || s.responseAffinity == nil ||
		s.stateCache == nil {
		t.Fatalf("NewScheduler returned incomplete scheduler: %#v", s)
	}
	recorder := &captureMonitorRecorder{}
	s.SetMonitorRecorder(recorder)
	if s.state.monitor != recorder {
		t.Fatal("SetMonitorRecorder did not install recorder")
	}

	(&Scheduler{}).SetMonitorRecorder(recorder)
}

func TestSchedulerRPMAndApplyWrappers(t *testing.T) {
	ctx := context.Background()
	rpm := &facadeRPMTracker{tryAllowed: false}
	s := &Scheduler{
		rpm:   rpm,
		state: &StateMachine{},
	}

	s.IncrementRPM(ctx, 7)
	if rpm.incrementCalls != 1 {
		t.Fatalf("IncrementRPM calls = %d", rpm.incrementCalls)
	}
	if allowed := s.TryIncrementRPM(ctx, 7, 10); allowed {
		t.Fatal("TryIncrementRPM allowed = true, want false")
	}
	if rpm.tryCalls != 1 {
		t.Fatalf("TryIncrementRPM calls = %d", rpm.tryCalls)
	}
	s.DecrementRPM(ctx, 7)
	if rpm.decrementCalls != 1 {
		t.Fatalf("DecrementRPM calls = %d", rpm.decrementCalls)
	}

	rpm.err = errors.New("redis down")
	if allowed := s.TryIncrementRPM(ctx, 7, 10); !allowed {
		t.Fatal("TryIncrementRPM error should fail open")
	}
	s.IncrementRPM(ctx, 7)

	s.Apply(ctx, 7, Judgment{Kind: sdk.OutcomeClientError})
	if rpm.decrementCalls != 2 {
		t.Fatalf("Apply client error decrement calls = %d", rpm.decrementCalls)
	}
}

func TestSchedulerSessionMessageAndCostWrappers(t *testing.T) {
	ctx := context.Background()
	session := &facadeSessionTracker{allowed: false}
	windowCost := &facadeWindowCostTracker{}
	s := &Scheduler{
		session:    session,
		msgQueue:   NewMessageQueue(nil, NewRPMCounter(nil)),
		windowCost: windowCost,
	}

	s.RefreshSession(ctx, 7, "", nil)
	if session.refreshCalls != 0 {
		t.Fatalf("RefreshSession empty session calls = %d", session.refreshCalls)
	}
	s.RefreshSession(ctx, 7, "sess", nil)
	if session.refreshCalls != 1 || session.lastIdle != defaultSessionIdleTimeout {
		t.Fatalf("RefreshSession default idle calls=%d idle=%v", session.refreshCalls, session.lastIdle)
	}
	session.err = errors.New("refresh failed")
	s.RefreshSession(ctx, 7, "sess", map[string]interface{}{"session_idle_timeout": 30})
	if session.refreshCalls != 2 || session.lastIdle != 30*time.Second {
		t.Fatalf("RefreshSession custom idle calls=%d idle=%v", session.refreshCalls, session.lastIdle)
	}

	if ok := s.RegisterSession(ctx, 7, "", nil); !ok {
		t.Fatal("RegisterSession empty session = false")
	}
	if ok := s.RegisterSession(ctx, 7, "sess", nil); !ok {
		t.Fatal("RegisterSession without limit = false")
	}
	if ok := s.RegisterSession(ctx, 7, "sess", map[string]interface{}{"max_sessions": 2, "session_idle_timeout": 45}); ok {
		t.Fatal("RegisterSession tracker denied, got true")
	}
	if session.registerCalls != 1 || session.lastIdle != 45*time.Second {
		t.Fatalf("RegisterSession calls=%d idle=%v", session.registerCalls, session.lastIdle)
	}

	if MessageLockEnabled(map[string]interface{}{"msg_lock_enabled": true}) != true {
		t.Fatal("MessageLockEnabled true = false")
	}
	if got := MessageLockMaxWaiters(nil); got != defaultMaxWaiters {
		t.Fatalf("MessageLockMaxWaiters default = %d", got)
	}
	if got := MessageLockMaxWaiters(map[string]interface{}{"msg_lock_max_waiters": 3}); got != 3 {
		t.Fatalf("MessageLockMaxWaiters custom = %d", got)
	}
	if ok, err := s.AcquireMessageLock(ctx, 7, "req", map[string]interface{}{
		"msg_lock_ttl_seconds":  1,
		"msg_lock_wait_seconds": 1,
		"msg_lock_max_waiters":  2,
	}); err != nil || !ok {
		t.Fatalf("AcquireMessageLock nil redis = %v, %v", ok, err)
	}
	s.ReleaseMessageLock(ctx, 7, "req")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	s.EnforceMessageDelay(canceled, 7, map[string]interface{}{"base_rpm": 60})
	s.EnforceMessageDelay(ctx, 7, nil)

	s.AddWindowCost(ctx, 7, 1.25)
	if len(windowCost.adds) != 1 || windowCost.adds[0] != 1.25 {
		t.Fatalf("windowCost adds = %#v", windowCost.adds)
	}
}

func TestSchedulerFamilyCooldownAndRouteGraphGuards(t *testing.T) {
	ctx := context.Background()
	var nilScheduler *Scheduler
	nilScheduler.RefreshRouteGraphGroup(ctx, 1)
	nilScheduler.RemoveRouteGraphGroup(1)
	nilScheduler.RefreshRouteGraphAccount(ctx, 1)
	nilScheduler.RemoveRouteGraphAccount(1)
	nilScheduler.RefreshRouteGraphUser(ctx, 1)
	nilScheduler.RemoveRouteGraphUser(1)
	nilScheduler.RefreshRouteGraphAPIKey(ctx, 1)
	nilScheduler.RemoveRouteGraphAPIKey(1)

	s := &Scheduler{stateCache: newAccountStateCache()}
	s.RefreshRouteGraphGroup(ctx, 0)
	s.RemoveRouteGraphGroup(0)
	s.RefreshRouteGraphAccount(ctx, 0)
	s.RemoveRouteGraphAccount(0)
	s.RefreshRouteGraphUser(ctx, 0)
	s.RemoveRouteGraphUser(0)
	s.RefreshRouteGraphAPIKey(ctx, 0)
	s.RemoveRouteGraphAPIKey(0)

	if got := s.ListFamilyCooldowns(ctx, 7); got != nil {
		t.Fatalf("ListFamilyCooldowns nil tracker = %#v", got)
	}
	if got := s.ListFamilyCooldownsBatch(ctx, []int{7}); got != nil {
		t.Fatalf("ListFamilyCooldownsBatch nil tracker = %#v", got)
	}
	if got := s.ClearFamilyCooldowns(ctx, 7); got != 0 {
		t.Fatalf("ClearFamilyCooldowns nil tracker = %d", got)
	}

	family := &facadeFamilyCooldownTracker{
		entries: []FamilyCooldownEntry{{Family: "gpt-4.1", Reason: "429"}},
		batch:   map[int][]FamilyCooldownEntry{7: {{Family: "gpt-4.1"}}},
		clear:   2,
	}
	s.familyCooldown = family
	if got := s.ListFamilyCooldowns(ctx, 7); len(got) != 1 || got[0].Family != "gpt-4.1" {
		t.Fatalf("ListFamilyCooldowns = %#v", got)
	}
	if got := s.ListFamilyCooldownsBatch(ctx, []int{7}); len(got[7]) != 1 {
		t.Fatalf("ListFamilyCooldownsBatch = %#v", got)
	}
	if got := s.ClearFamilyCooldowns(ctx, 7); got != 2 {
		t.Fatalf("ClearFamilyCooldowns = %d", got)
	}
}
