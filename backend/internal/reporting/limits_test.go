package reporting

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRangeRejectsInvalidUnboundedAndReversedInputs(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	for _, values := range [][3]string{
		{"bad", "", "UTC"}, {"", "bad", "UTC"}, {"", "", "invalid/timezone"},
		{"2026-09-09", "2026-09-08", "UTC"}, {"2020-01-01", "", "UTC"},
	} {
		if _, _, err := Range(values[0], values[1], values[2], now, 24*time.Hour, 31); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("Range(%v) = %v", values, err)
		}
	}
	from, until, err := Range("", "2026-03-08", "America/New_York", now, 24*time.Hour, 31)
	if err != nil || until.Sub(from) != 24*time.Hour || until.In(from.Location()).Hour() != 0 {
		t.Fatalf("bounded DST end-only range = %v/%v/%v", from, until, err)
	}
}

func TestQueryBudgetRemainsHeldUntilWorkCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	_, release1, err := Acquire(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	defer release1()
	_, release2, err := Acquire(t.Context(), "one")
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	cancel()
	if _, _, err := Acquire(t.Context(), "one"); !errors.Is(err, ErrBusy) {
		t.Fatalf("permit released early: %v", err)
	}
	release1()
	release1()
	_, release3, err := Acquire(t.Context(), "one")
	if err != nil {
		t.Fatal(err)
	}
	release3()
	_, release4, err := Acquire(t.Context(), "two")
	if err != nil {
		t.Fatal(err)
	}
	defer release4()
	_, release5, err := Acquire(t.Context(), "three")
	if err != nil {
		t.Fatal(err)
	}
	defer release5()
	_, release6, err := Acquire(t.Context(), "four")
	if err != nil {
		t.Fatal(err)
	}
	defer release6()
	if _, _, err := Acquire(t.Context(), "five"); !errors.Is(err, ErrBusy) {
		t.Fatalf("global limit = %v", err)
	}
}
