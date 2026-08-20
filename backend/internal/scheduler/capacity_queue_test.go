package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCapacityQueueWakeIsPoolScopedAndBounded(t *testing.T) {
	queue := NewCapacityQueue(1, 2)
	pool := NewCapacityPoolKey(11, "openai", "gpt-5.6-sol")
	otherPool := NewCapacityPoolKey(12, "openai", "gpt-5.6-sol")
	result := make(chan error, 1)
	go func() {
		result <- queue.Wait(context.Background(), pool, time.Second)
	}()
	waitForCapacityQueueWaiters(t, queue, 1)

	if err := queue.Wait(context.Background(), pool, time.Second); !errors.Is(err, ErrCapacityQueueFull) {
		t.Fatalf("second waiter error = %v, want ErrCapacityQueueFull", err)
	}
	if queue.Notify(otherPool) {
		t.Fatal("notification for another pool woke a waiter")
	}
	if !queue.Notify(pool) {
		t.Fatal("pool notification did not wake the waiter")
	}
	if err := <-result; err != nil {
		t.Fatalf("woken waiter error = %v", err)
	}

	stats := queue.Stats()
	if stats.Waiters != 0 || stats.WaitingPools != 0 || stats.EnqueuedTotal != 1 || stats.WokenTotal != 1 || stats.RejectedTotal != 1 {
		t.Fatalf("unexpected queue stats: %+v", stats)
	}
}

func TestCapacityQueueTimeoutAndCancellation(t *testing.T) {
	queue := NewCapacityQueue(2, 2)
	pool := NewCapacityPoolKey(11, "openai", "gpt-5.6-sol")
	if err := queue.Wait(context.Background(), pool, 10*time.Millisecond); !errors.Is(err, ErrCapacityQueueTimeout) {
		t.Fatalf("timeout error = %v, want ErrCapacityQueueTimeout", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.Wait(ctx, pool, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v, want context.Canceled", err)
	}

	stats := queue.Stats()
	if stats.Waiters != 0 || stats.TimedOutTotal != 1 || stats.CanceledTotal != 1 || stats.WaitCompletedTotal != 2 {
		t.Fatalf("unexpected queue stats: %+v", stats)
	}
}

func TestCapacityQueueReleaseGenerationAvoidsLostWake(t *testing.T) {
	queue := NewCapacityQueue(2, 2)
	pool := NewCapacityPoolKey(11, "openai", "gpt-5.6-sol")
	generation := queue.Generation(pool)
	if queue.Notify(pool) {
		t.Fatal("notify without a waiter should not report a direct wake")
	}
	if err := queue.Wait(context.Background(), pool, time.Second, generation); err != nil {
		t.Fatalf("release generation was not consumed: %v", err)
	}
	if stats := queue.Stats(); stats.Waiters != 0 || stats.WaitingPools != 0 {
		t.Fatalf("pending signal left queue state: %+v", stats)
	}
}

func TestCapacityQueueReleaseFallsBackAcrossFamiliesInSamePool(t *testing.T) {
	queue := NewCapacityQueue(2, 2)
	waitingPool := NewCapacityPoolKey(11, "openai", "gpt-5.6-terra")
	releasingPool := NewCapacityPoolKey(11, "openai", "gpt-5.6-sol")
	result := make(chan error, 1)
	go func() {
		result <- queue.Wait(context.Background(), waitingPool, time.Second)
	}()
	waitForCapacityQueueWaiters(t, queue, 1)

	if !queue.Notify(releasingPool) {
		t.Fatal("release in the same group/platform did not wake another family")
	}
	if err := <-result; err != nil {
		t.Fatalf("cross-family waiter error = %v", err)
	}
}

func waitForCapacityQueueWaiters(t *testing.T, queue *CapacityQueue, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if queue.Stats().Waiters == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("capacity queue waiters = %d, want %d", queue.Stats().Waiters, want)
}
