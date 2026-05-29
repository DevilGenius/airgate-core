package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestDynamicRouterReturnsMethodNotAllowedForKnownPath(t *testing.T) {
	t.Parallel()

	router := NewDynamicRouter(&plugin.Forwarder{})
	router.AddRoutes("openai", []routeEntry{
		{Method: http.MethodPost, Path: "/v1/images/generations"},
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/images/generations", nil)

	router.Handle(c)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if allow := recorder.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", allow)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("body = %s, want method_not_allowed error", recorder.Body.String())
	}
}
