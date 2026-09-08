package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/scheduler"
)

func TestLostClientLeaseIsUnavailableRatherThanClientDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(scheduler.ErrLeaseLost)
	status := canceledForwardStatus(ctx)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("lease loss status=%d", status)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	markCanceledRequest(c, status)
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" {
		t.Fatal("lease loss did not return retryable response")
	}
}
