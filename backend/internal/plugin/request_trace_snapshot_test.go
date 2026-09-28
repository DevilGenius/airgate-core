package plugin

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestTraceIngressSnapshotIsImmutable(t *testing.T) {
	original := []byte(`{"model":"gpt-test","input":"original","service_tier":"priority"}`)
	body := bytes.Clone(original)
	trace := &requestTraceSession{}
	trace.captureRequestBody(body, "application/json", int64(len(body)), nil)
	for i := range body {
		body[i] = 'x'
	}
	if !bytes.Equal(trace.traceInput().RequestBody, original) {
		t.Fatal("forwarding mutation corrupted the ingress trace")
	}
}

func TestFinishedTraceDoesNotPinGinContext(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	trace := &requestTraceSession{eventHandled: true}
	trace.captureRequestBody([]byte("original"), "application/json", 8, nil)
	c.Set(ginCtxKeyRequestTrace, trace)
	queued := trace.traceInput()
	(&Forwarder{}).finishRequestTrace(c, trace)
	if trace.requestBody != nil || trace.requestHeaders != nil || trace.attempts != nil || requestTraceFromGinContext(c) != nil {
		t.Fatal("pooled context retained raw request")
	}
	if string(queued.RequestBody) != "original" {
		t.Fatal("queue ownership invalidated by request cleanup")
	}
}
