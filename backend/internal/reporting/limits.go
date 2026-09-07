// Package reporting bounds synchronous analytical work independently of the
// connection pool used by authentication and billing.
package reporting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalidRange               = errors.New("日期或时区无效，或查询范围过大")
	ErrTooManyGroups              = errors.New("统计结果分组过多，请缩小查询范围")
	ErrBusy                       = errors.New("统计查询繁忙，请稍后重试")
	ErrHistoryNotReady            = errors.New("历史统计尚未完成同步，请稍后重试")
	ErrHistoryNeedsBackfill error = historyNeedsBackfill{}
)

type historyNeedsBackfill struct{}

func (historyNeedsBackfill) Error() string { return "历史统计不完整，需要执行回填" }
func (historyNeedsBackfill) Unwrap() error { return ErrHistoryNotReady }

const MaxGroups = 8192

// ValidateDates does not silently turn malformed filters into unfiltered scans.
func ValidateDates(start, end, zone string) error {
	_, _, _, err := parseDates(start, end, zone)
	return err
}

func parseDates(start, end, zone string) (time.Time, time.Time, *time.Location, error) {
	loc := time.Local
	var err error
	if zone != "" {
		loc, err = time.LoadLocation(zone)
		if err != nil {
			return time.Time{}, time.Time{}, nil, ErrInvalidRange
		}
	}
	var from, until time.Time
	if start != "" {
		from, err = time.ParseInLocation("2006-01-02", start, loc)
		if err != nil {
			return from, until, loc, ErrInvalidRange
		}
	}
	if end != "" {
		until, err = time.ParseInLocation("2006-01-02", end, loc)
		if err != nil {
			return from, until, loc, ErrInvalidRange
		}
		until = until.AddDate(0, 0, 1)
	}
	if !from.IsZero() && !until.IsZero() && !from.Before(until) {
		return from, until, loc, ErrInvalidRange
	}
	return from, until, loc, nil
}

// Range returns a half-open interval. A single bound gets a finite counterpart.
func Range(start, end, zone string, now time.Time, recent time.Duration, maxDays int) (time.Time, time.Time, error) {
	from, until, loc, err := parseDates(start, end, zone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if recent <= 0 {
		recent = 24 * time.Hour
	}
	if until.IsZero() {
		until = now.In(loc)
	}
	if from.IsZero() {
		from = until.Add(-recent)
	}
	if !from.Before(until) || until.After(from.AddDate(0, 0, maxDays)) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w（最多 %d 天）", ErrInvalidRange, maxDays)
	}
	return from, until, nil
}

var gate = struct {
	sync.Mutex
	active int
	owners map[string]int
}{owners: make(map[string]int)}

// Acquire is fail-fast: neither SQL connections nor waiting goroutines are an
// unbounded analytics queue. Hold the permit for the actual loader lifetime.
func Acquire(ctx context.Context, owner string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	gate.Lock()
	if gate.active >= 4 || gate.owners[owner] >= 2 {
		gate.Unlock()
		return nil, nil, ErrBusy
	}
	gate.active++
	gate.owners[owner]++
	gate.Unlock()
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	var once sync.Once
	return queryCtx, func() {
		once.Do(func() {
			cancel()
			gate.Lock()
			gate.active--
			gate.owners[owner]--
			if gate.owners[owner] == 0 {
				delete(gate.owners, owner)
			}
			gate.Unlock()
		})
	}, nil
}
