package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrCapacityQueueFull    = errors.New("容量等待队列已满")
	ErrCapacityQueueTimeout = errors.New("等待上游容量超时")
)

// CapacityPoolKey identifies one independently scheduled capacity pool.
// Requests in different groups, platforms, or model families never block one
// another in the local bounded queue.
type CapacityPoolKey struct {
	GroupID  int
	Platform string
	Family   string
}

type capacityPoolScope struct {
	GroupID  int
	Platform string
}

func (k CapacityPoolKey) scope() capacityPoolScope {
	return capacityPoolScope{GroupID: k.GroupID, Platform: k.Platform}
}

func NewCapacityPoolKey(groupID int, platform, family string) CapacityPoolKey {
	return CapacityPoolKey{
		GroupID:  groupID,
		Platform: strings.ToLower(strings.TrimSpace(platform)),
		Family:   strings.ToLower(strings.TrimSpace(family)),
	}
}

// CapacityQueueStats is a lock-consistent snapshot of the bounded capacity
// queues plus monotonic lifecycle counters used by runtime monitoring.
type CapacityQueueStats struct {
	Waiters           int
	WaitingPools      int
	MaxPoolWaiters    int
	MaxWaitersPerPool int
	MaxTotalWaiters   int

	EnqueuedTotal      int64
	WokenTotal         int64
	TimedOutTotal      int64
	RejectedTotal      int64
	CanceledTotal      int64
	WaitCompletedTotal int64
	WaitDurationMS     int64
}

type capacityWaiterState uint8

const (
	capacityWaiterQueued capacityWaiterState = iota
	capacityWaiterWoken
	capacityWaiterTimedOut
	capacityWaiterCanceled
)

type capacityWaiter struct {
	ready    chan struct{}
	queuedAt time.Time
	state    capacityWaiterState
}

type capacityPoolQueue struct {
	waiters []*capacityWaiter
}

// CapacityQueue is an in-process, pool-scoped, bounded FIFO wait queue. It
// replaces request-local polling: a released account slot wakes at most one
// waiter for the same scheduling pool.
type CapacityQueue struct {
	mu                sync.Mutex
	pools             map[CapacityPoolKey]*capacityPoolQueue
	generations       map[capacityPoolScope]uint64
	totalWaiters      int
	maxWaitersPerPool int
	maxTotalWaiters   int

	enqueuedTotal      atomic.Int64
	wokenTotal         atomic.Int64
	timedOutTotal      atomic.Int64
	rejectedTotal      atomic.Int64
	canceledTotal      atomic.Int64
	waitCompletedTotal atomic.Int64
	waitDurationNS     atomic.Int64
}

func NewCapacityQueue(maxWaitersPerPool, maxTotalWaiters int) *CapacityQueue {
	if maxWaitersPerPool <= 0 {
		maxWaitersPerPool = 1
	}
	if maxTotalWaiters < maxWaitersPerPool {
		maxTotalWaiters = maxWaitersPerPool
	}
	return &CapacityQueue{
		pools:             make(map[CapacityPoolKey]*capacityPoolQueue),
		generations:       make(map[capacityPoolScope]uint64),
		maxWaitersPerPool: maxWaitersPerPool,
		maxTotalWaiters:   maxTotalWaiters,
	}
}

// Wait enqueues one request and blocks until a slot release wakes it, the
// caller is canceled, or the bounded wait expires. Queue saturation rejects
// immediately and never creates another goroutine.
func (q *CapacityQueue) Wait(ctx context.Context, key CapacityPoolKey, timeout time.Duration, observedGeneration ...uint64) error {
	if q == nil || timeout <= 0 {
		return ErrCapacityQueueTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key = NewCapacityPoolKey(key.GroupID, key.Platform, key.Family)
	waiter := &capacityWaiter{
		ready:    make(chan struct{}),
		queuedAt: time.Now(),
		state:    capacityWaiterQueued,
	}

	q.mu.Lock()
	if len(observedGeneration) > 0 && q.generations[key.scope()] != observedGeneration[0] {
		// A slot was released during the caller's candidate probe. Do not enqueue
		// behind an event that has already happened; the caller will re-select.
		q.mu.Unlock()
		return nil
	}
	pool := q.pools[key]
	poolWaiters := 0
	if pool != nil {
		poolWaiters = len(pool.waiters)
	}
	if q.totalWaiters >= q.maxTotalWaiters || poolWaiters >= q.maxWaitersPerPool {
		q.mu.Unlock()
		q.rejectedTotal.Add(1)
		return ErrCapacityQueueFull
	}
	if pool == nil {
		pool = &capacityPoolQueue{}
		q.pools[key] = pool
	}
	pool.waiters = append(pool.waiters, waiter)
	q.totalWaiters++
	q.mu.Unlock()
	q.enqueuedTotal.Add(1)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-waiter.ready:
		return nil
	case <-ctx.Done():
		if q.removeWaiter(key, waiter, capacityWaiterCanceled) {
			q.canceledTotal.Add(1)
			q.recordWaitCompletion(waiter)
			return ctx.Err()
		}
		// A slot notification won the race with cancellation.
		<-waiter.ready
		return nil
	case <-timer.C:
		if q.removeWaiter(key, waiter, capacityWaiterTimedOut) {
			q.timedOutTotal.Add(1)
			q.recordWaitCompletion(waiter)
			return ErrCapacityQueueTimeout
		}
		// A slot notification won the race with the timer.
		<-waiter.ready
		return nil
	}
}

// Notify wakes exactly one FIFO waiter for the pool. The caller invokes this
// after releasing an account slot; the woken request then performs a fresh
// scheduler selection and atomic slot acquisition.
func (q *CapacityQueue) Notify(key CapacityPoolKey) bool {
	if q == nil {
		return false
	}
	key = NewCapacityPoolKey(key.GroupID, key.Platform, key.Family)
	q.mu.Lock()
	q.generations[key.scope()]++
	pool := q.pools[key]
	if pool == nil || len(pool.waiters) == 0 {
		key, pool = q.oldestPoolWaiterLocked(key.scope())
		if pool == nil {
			q.mu.Unlock()
			return false
		}
	}
	waiter := pool.waiters[0]
	pool.waiters[0] = nil
	pool.waiters = pool.waiters[1:]
	waiter.state = capacityWaiterWoken
	q.totalWaiters--
	if len(pool.waiters) == 0 {
		delete(q.pools, key)
	}
	close(waiter.ready)
	q.mu.Unlock()

	q.wokenTotal.Add(1)
	q.recordWaitCompletion(waiter)
	return true
}

func (q *CapacityQueue) oldestPoolWaiterLocked(scope capacityPoolScope) (CapacityPoolKey, *capacityPoolQueue) {
	var selectedKey CapacityPoolKey
	var selectedPool *capacityPoolQueue
	var selectedAt time.Time
	for key, pool := range q.pools {
		if key.scope() != scope || pool == nil || len(pool.waiters) == 0 || pool.waiters[0] == nil {
			continue
		}
		queuedAt := pool.waiters[0].queuedAt
		if selectedPool == nil || queuedAt.Before(selectedAt) {
			selectedKey = key
			selectedPool = pool
			selectedAt = queuedAt
		}
	}
	return selectedKey, selectedPool
}

// Generation returns the release edge observed before a caller probes the
// candidate accounts. Passing it to Wait closes the release-before-enqueue
// race without retaining stale wake signals for future requests.
func (q *CapacityQueue) Generation(key CapacityPoolKey) uint64 {
	if q == nil {
		return 0
	}
	key = NewCapacityPoolKey(key.GroupID, key.Platform, key.Family)
	q.mu.Lock()
	generation := q.generations[key.scope()]
	q.mu.Unlock()
	return generation
}

func (q *CapacityQueue) removeWaiter(key CapacityPoolKey, waiter *capacityWaiter, state capacityWaiterState) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if waiter.state != capacityWaiterQueued {
		return false
	}
	pool := q.pools[key]
	if pool == nil {
		return false
	}
	for index, candidate := range pool.waiters {
		if candidate != waiter {
			continue
		}
		copy(pool.waiters[index:], pool.waiters[index+1:])
		pool.waiters[len(pool.waiters)-1] = nil
		pool.waiters = pool.waiters[:len(pool.waiters)-1]
		waiter.state = state
		q.totalWaiters--
		if len(pool.waiters) == 0 {
			delete(q.pools, key)
		}
		return true
	}
	return false
}

func (q *CapacityQueue) recordWaitCompletion(waiter *capacityWaiter) {
	if q == nil || waiter == nil {
		return
	}
	duration := time.Since(waiter.queuedAt)
	if duration < 0 {
		duration = 0
	}
	q.waitCompletedTotal.Add(1)
	q.waitDurationNS.Add(duration.Nanoseconds())
}

func (q *CapacityQueue) Stats() CapacityQueueStats {
	if q == nil {
		return CapacityQueueStats{}
	}
	q.mu.Lock()
	stats := CapacityQueueStats{
		Waiters:           q.totalWaiters,
		WaitingPools:      len(q.pools),
		MaxWaitersPerPool: q.maxWaitersPerPool,
		MaxTotalWaiters:   q.maxTotalWaiters,
	}
	for _, pool := range q.pools {
		if pool != nil && len(pool.waiters) > stats.MaxPoolWaiters {
			stats.MaxPoolWaiters = len(pool.waiters)
		}
	}
	q.mu.Unlock()

	stats.EnqueuedTotal = q.enqueuedTotal.Load()
	stats.WokenTotal = q.wokenTotal.Load()
	stats.TimedOutTotal = q.timedOutTotal.Load()
	stats.RejectedTotal = q.rejectedTotal.Load()
	stats.CanceledTotal = q.canceledTotal.Load()
	stats.WaitCompletedTotal = q.waitCompletedTotal.Load()
	stats.WaitDurationMS = q.waitDurationNS.Load() / int64(time.Millisecond)
	return stats
}
