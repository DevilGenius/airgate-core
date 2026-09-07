package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
)

func TestRPMRefundRetainsOriginalWindowAndRunsOnce(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	rpm := NewRPMCounter(rdb)
	ctx := context.Background()
	old := rpmMinuteKey(7, 2)
	current := rpmMinuteKey(7, 3)
	reservation := &RPMReservation{}
	mock.ExpectTime().SetVal(time.Unix(120, 0))
	mock.ExpectEvalSha(tryIncrementScript.Hash(), []string{old}, 10).SetVal(int64(1))
	if allowed, err := rpm.TryIncrementRPM(ctx, 7, 10, reservation); err != nil || !allowed {
		t.Fatalf("acquire = %v, %v", allowed, err)
	}
	mock.ExpectTime().SetVal(time.Unix(180, 0))
	mock.ExpectEvalSha(tryIncrementScript.Hash(), []string{current}, 10).SetVal(int64(1))
	if allowed, err := rpm.TryIncrementRPM(ctx, 7, 10); err != nil || !allowed {
		t.Fatalf("next window acquire = %v, %v", allowed, err)
	}
	// No TIME command is allowed here: cancellation refunds the admitted window.
	mock.ExpectEvalSha(decrementRPMScript.Hash(), []string{old}).SetVal(int64(0))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	rpm.DecrementRPM(canceled, 7, reservation)
	rpm.DecrementRPM(ctx, 7, reservation)
	rpm.DecrementRPM(ctx, 7) // No admission receipt, no decrement.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
