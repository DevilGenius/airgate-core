package plugin

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

type mustNotReadBody struct{ t *testing.T }

func (b mustNotReadBody) Read([]byte) (int, error) {
	b.t.Error("rejected request body was read")
	return 0, io.ErrUnexpectedEOF
}
func (mustNotReadBody) Close() error { return nil }

func TestClientConcurrencyCheckedBeforeRequestBody(t *testing.T) {
	f := &Forwarder{clientLimiter: newClientLimiter()}
	release, _, _ := f.getClientLimiter().acquire(1, 2, 1, 1)
	defer release()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Body = mustNotReadBody{t}
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{UserID: 1, KeyID: 2, UserBalance: 10, UserMaxConcurrency: 1, KeyMaxConcurrency: 1})
	f.Forward(c)
	if w.Code != 429 {
		t.Fatalf("status = %d", w.Code)
	}
}
