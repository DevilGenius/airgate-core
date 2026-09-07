package plugin

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redismock/v9"
)

func TestAccountAdmissionStopsWhenRedisClockFails(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	f := &Forwarder{scheduler: scheduler.NewScheduler(nil, rdb), concurrency: scheduler.NewConcurrencyManager(rdb)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	state := &forwardState{account: &ent.Account{ID: 1, MaxConcurrency: 2}}
	mock.ExpectTime().SetErr(errors.New("redis unavailable"))
	release, failure := f.acquireAccountSlot(c, state)
	if release != nil || failure != accountSlotAcquireUnavailable {
		t.Fatalf("unknown capacity admitted: release=%v failure=%v", release != nil, failure)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
