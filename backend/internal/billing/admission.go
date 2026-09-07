package billing

import (
	"context"
	"errors"
	"sync"
)

var ErrBillingBusy = errors.New("计费服务繁忙，请稍后重试")

const maxBillingProducers = 1024

// Reserve runs before upstream consumption. High water rejects new work,
// rather than discarding already-produced financial events.
func (r *Recorder) Reserve() (func(), error) {
	if r == nil {
		return func() {}, nil
	}
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	if !r.accepting || r.producers >= maxBillingProducers {
		return nil, ErrBillingBusy
	}
	if r.journal != nil {
		count, bytes, err := r.journal.Stats()
		if err != nil || count+r.producers >= journalHighWaterRecords || bytes >= journalHighWaterBytes {
			return nil, ErrBillingBusy
		}
	} else if len(r.ch) == cap(r.ch) && len(r.retryCh) == cap(r.retryCh) {
		return nil, ErrBillingBusy
	}
	if r.producers == 0 {
		r.producerIdle = make(chan struct{})
	}
	r.producers++
	var once sync.Once
	return func() {
		once.Do(func() {
			r.admissionMu.Lock()
			r.producers--
			if r.producers == 0 {
				close(r.producerIdle)
			}
			r.admissionMu.Unlock()
		})
	}, nil
}

func (r *Recorder) acquireSyncWrite(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case r.syncWaiters <- struct{}{}:
	default:
		return nil, ErrBillingBusy
	}
	select {
	case r.syncWrites <- struct{}{}:
		return func() { <-r.syncWrites; <-r.syncWaiters }, nil
	case <-ctx.Done():
		<-r.syncWaiters
		return nil, ctx.Err()
	}
}
