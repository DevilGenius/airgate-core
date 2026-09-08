package billing

import (
	"context"
	"errors"
	"time"
)

func (r *Recorder) StopAdmission() {
	if r != nil {
		r.admissionMu.Lock()
		r.accepting = false
		r.admissionMu.Unlock()
	}
}

func waitRecorder(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Recorder) beginRecordCall() (func(), error) {
	r.admissionMu.Lock()
	defer r.admissionMu.Unlock()
	if r.recordsClosed {
		return nil, errRecorderStopping
	}
	if r.recordCalls == 0 {
		r.recordIdle = make(chan struct{})
	}
	r.recordCalls++
	return func() {
		r.admissionMu.Lock()
		r.recordCalls--
		if r.recordCalls == 0 {
			close(r.recordIdle)
		}
		r.admissionMu.Unlock()
	}, nil
}

func (r *Recorder) saveLateRecord(record UsageRecord) error {
	if r.journal != nil {
		if _, err := r.journal.Append(record); err != nil {
			return err
		}
	}
	return errRecorderStopping
}

func (r *Recorder) StopContext(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.StopAdmission()
	r.once.Do(func() { go func() { r.shutdownErr = r.shutdown(ctx); close(r.shutdownDone) }() })
	if err := waitRecorder(ctx, r.shutdownDone); err != nil {
		return err
	}
	return r.shutdownErr
}

func (r *Recorder) shutdown(ctx context.Context) error {
	defer r.cancelWrites()
	r.admissionMu.Lock()
	producerIdle := r.producerIdle
	r.admissionMu.Unlock()
	producerErr := waitRecorder(ctx, producerIdle)
	r.admissionMu.Lock()
	r.recordsClosed = true
	recordIdle := r.recordIdle
	started := r.started
	r.admissionMu.Unlock()
	if producerErr != nil {
		r.cancelWrites()
	}
	recordErr := waitRecorder(ctx, recordIdle)
	if !started {
		if r.journal == nil && len(r.ch) > 0 {
			r.queueMu.Lock()
			r.queueClosed = true
			batch := make([]UsageRecord, 0, len(r.ch))
			for len(r.ch) > 0 {
				batch = append(batch, <-r.ch)
			}
			r.queueMu.Unlock()
			if r.db == nil {
				return errRecorderStopping
			}
			return errors.Join(producerErr, recordErr, r.batchInsert(ctx, batch))
		}
		return errors.Join(producerErr, recordErr)
	}
	if r.journal != nil {
		defer r.journalCancel()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			count, _, err := r.journal.Stats()
			if err != nil {
				return errors.Join(producerErr, recordErr, err)
			}
			if count == 0 {
				break
			}
			select {
			case <-ctx.Done():
				return errors.Join(producerErr, recordErr, ctx.Err())
			case <-ticker.C:
				r.notifyJournal()
			}
		}
		r.journalCancel()
		return errors.Join(producerErr, recordErr, waitRecorder(ctx, r.journalDone))
	}
	r.shutdownCtx = ctx
	close(r.stopCh)
	done := make(chan struct{})
	go func() { <-r.stopped; close(r.retryCh); <-r.retryStopped; close(done) }()
	return errors.Join(producerErr, recordErr, waitRecorder(ctx, done))
}
