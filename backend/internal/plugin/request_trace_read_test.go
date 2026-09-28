package plugin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/httpguard"
	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
)

type ingressReadTraceRecorder struct {
	captureRequestMonitorRecorder
	traces []requestmonitoring.TraceInput
}

func (r *ingressReadTraceRecorder) RecordRequestTrace(_ context.Context, _ requestmonitoring.EventInput, input requestmonitoring.TraceInput) bool {
	r.traces = append(r.traces, input)
	return true
}

type ingressFailingReader struct {
	io.Reader
	err error
}

func (r ingressFailingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, r.err
	}
	return n, err
}

func TestFailedIngressReadsNeverQueuePartialBody(t *testing.T) {
	const prefix = `{"model":"gpt-test"}`
	for _, tc := range []struct {
		name          string
		contentLength int64
		partial       string
		readErr       error
		limited       bool
		status        int
	}{
		{"known_length_limit", 200, prefix, nil, true, 413},
		{"unknown_length_limit", -1, prefix, nil, true, 413},
		{"known_length_read_failure", 200, prefix, io.ErrUnexpectedEOF, false, 400},
		{"unknown_length_read_failure", -1, prefix, io.ErrUnexpectedEOF, false, 400},
		{"failure_before_any_bytes", -1, "", io.ErrUnexpectedEOF, false, 400},
		{"admission_budget", -1, prefix, httpguard.ErrBodyBudget, false, 503},
		{"read_timeout", -1, prefix, context.DeadlineExceeded, false, 408},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.ContentLength = tc.contentLength
			if tc.limited {
				c.Request.Body = http.MaxBytesReader(w, io.NopCloser(strings.NewReader(tc.partial+"excess")), int64(len(tc.partial)))
			} else {
				c.Request.Body = io.NopCloser(ingressFailingReader{strings.NewReader(tc.partial), tc.readErr})
			}
			defer c.Request.Body.Close()
			c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{})
			recorder := &ingressReadTraceRecorder{}
			f := &Forwarder{requestMonitor: recorder}
			f.requestTraceEnabled.Store(true)
			session := f.beginRequestTrace(c)
			if _, ok := f.parseRequest(c); ok || w.Code != tc.status {
				t.Fatalf("read failure response changed: accepted=%v status=%d", ok, w.Code)
			}
			f.finishRequestTrace(c, session)
			if len(recorder.traces) != 1 {
				t.Fatalf("queued traces = %d", len(recorder.traces))
			}
			input := recorder.traces[0]
			if !input.RequestBodyIncomplete || len(input.RequestBody) != 0 || input.RequestBodyOriginalSize != max(tc.contentLength, int64(len(tc.partial))) {
				t.Fatalf("partial body or incorrect capture metadata queued: %+v", input)
			}
			if len(input.Attempts) != 0 {
				t.Fatal("ingress failure fabricated an upstream attempt")
			}
		})
	}
}

func TestIngressCaptureDetectsShortDeclaredBodyWithoutReadError(t *testing.T) {
	session := &requestTraceSession{}
	session.captureRequestBody([]byte("complete"), "text/plain", 8, nil)
	session.captureRequestBody([]byte("short"), "text/plain", 20, nil)
	input := session.traceInput()
	if !input.RequestBodyIncomplete || len(input.RequestBody) != 0 || input.RequestBodyOriginalSize != 20 {
		t.Fatal("incomplete capture retained a previous body or lost its declared length")
	}
}
