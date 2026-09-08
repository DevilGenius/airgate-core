package billing

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/lib/pq"
)

var ErrInvalidUsageRecord = errors.New("invalid billing record")

func (r *Recorder) EnableJournal(dir string) error {
	journal, err := OpenJournal(dir)
	if err != nil {
		return err
	}
	r.journal = journal
	r.journalWake = make(chan struct{}, 1)
	r.journalDone = make(chan struct{})
	return nil
}
func (r *Recorder) notifyJournal() {
	select {
	case r.journalWake <- struct{}{}:
	default:
	}
}

func (r *Recorder) runJournal(ctx context.Context) {
	defer close(r.journalDone)
	refresh := time.NewTicker(10 * time.Second)
	defer refresh.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-refresh.C:
			if err := r.journal.Refresh(); err != nil {
				slog.Error("billing_journal_refresh_failed", "error", err)
			}
		default:
		}
		batch, err := r.journal.Batch(batchSize)
		if err != nil {
			slog.Error("billing_journal_read_failed", "error", err)
		}
		if len(batch) > 0 {
			writeCtx, cancel := context.WithTimeout(ctx, writeAttemptTimeout)
			err = r.flushJournalBatch(writeCtx, batch)
			cancel()
			if err == nil {
				if count, _, _ := r.journal.Stats(); count >= batchSize {
					continue
				}
			} else {
				slog.Warn("billing_journal_retry_pending", "records", len(batch), "error", err)
			}
		}
		wait := recorderFlushInterval
		if err != nil {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		wake := r.journalWake
		if err != nil {
			wake = nil
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-refresh.C:
			timer.Stop()
			if err := r.journal.Refresh(); err != nil {
				slog.Error("billing_journal_refresh_failed", "error", err)
			}
		case <-timer.C:
		}
	}
}

func permanentBillingError(err error) bool {
	if errors.Is(err, ErrInvalidUsageRecord) {
		return true
	}
	var postgres *pq.Error
	if errors.As(err, &postgres) {
		return postgres.Code.Class() == "22" || postgres.Code.Class() == "23" || postgres.Code.Class() == "21"
	}
	var sqlite interface{ Code() int }
	return (errors.As(err, &sqlite) && sqlite.Code()&255 == 19) || cgoSQLiteConstraintError(err)
}

func (r *Recorder) flushJournalBatch(ctx context.Context, batch []UsageRecord) error {
	err := r.batchInsert(ctx, batch)
	if err == nil {
		for _, record := range batch {
			if err := r.journal.Ack(record.BillingEventID); err != nil {
				return err
			}
		}
		return nil
	}
	if !permanentBillingError(err) {
		return err
	}
	if len(batch) == 1 {
		if moveErr := r.journal.Quarantine(batch[0].BillingEventID, err.Error()); moveErr != nil {
			return moveErr
		}
		r.deadLetterTotal.Add(1)
		slog.Error("billing_journal_quarantined", "billing_event_id", batch[0].BillingEventID, "error", err)
		return nil
	}
	middle := len(batch) / 2
	left := r.flushJournalBatch(ctx, batch[:middle])
	right := r.flushJournalBatch(ctx, batch[middle:])
	return errors.Join(left, right)
}

func finiteBillingRecord(record UsageRecord) bool {
	for _, value := range []float64{record.InputPrice, record.OutputPrice, record.CachedInputPrice, record.CacheCreationPrice, record.InputCost, record.OutputCost, record.CachedInputCost, record.CacheCreationCost, record.TotalCost, record.ActualCost, record.BilledCost, record.AccountCost, record.RateMultiplier, record.SellRate, record.AccountRateMultiplier} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}
