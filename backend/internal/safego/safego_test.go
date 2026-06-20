package safego

import (
	"testing"
	"time"
)

func TestGoRunsFunction(t *testing.T) {
	done := make(chan struct{})

	Go("normal", func() {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not run")
	}
}

func TestGoRecoversPanic(t *testing.T) {
	done := make(chan struct{})

	Go("panic", func() {
		close(done)
		panic("boom")
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not run")
	}

	time.Sleep(10 * time.Millisecond)
}
