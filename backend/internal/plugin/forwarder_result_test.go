package plugin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/ent"
	sdk "github.com/DouDOU-start/airgate-sdk"
)

// TestMain 在所有并行测试启动前调一次 gin.SetMode，避免 SetMode 内部变量
// 被多个 t.Parallel() goroutine 同时写导致 -race 告警。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// fakeState 构造测试用的最小 forwardState（只填 writeFailureResponse 读到的字段）。
func fakeState(stream bool) *forwardState {
	return &forwardState{
		stream:  stream,
		plugin:  &PluginInstance{Name: "test-plugin"},
		account: &ent.Account{ID: 1},
	}
}

func TestWriteFailureResponse_StreamBeforeResponseStarts(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writeFailureResponse(c, fakeState(true), forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient, Reason: "boom"},
		err:     errors.New("upstream eof"),
	})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "上游服务暂不可用") {
		t.Fatalf("body = %q, want contain '上游服务暂不可用'", body)
	}
}

func TestWriteFailureResponse_StreamAfterResponseStarts(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()

	writeFailureResponse(c, fakeState(true), forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeStreamAborted},
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestWriteFailureResponse_NonStreamAlwaysWrites(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writeFailureResponse(c, fakeState(false), forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountDead, Reason: "token expired"},
	})

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "上游账号不可用") {
		t.Fatalf("body = %q, want contain '上游账号不可用'", body)
	}
}

func TestWriteFailureResponse_RateLimitedReturns429(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writeFailureResponse(c, fakeState(false), forwardExecution{
		outcome: sdk.ForwardOutcome{Kind: sdk.OutcomeAccountRateLimited},
	})

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "限流") {
		t.Fatalf("body = %q, want contain '限流'", body)
	}
}
