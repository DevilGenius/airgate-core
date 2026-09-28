package monitor

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

// The real encoder, including shared redaction, JSON, hashing and gzip. Disk/DB
// latency is excluded; this work runs in the dedicated background trace worker.
func BenchmarkRequestTraceEncode(b *testing.B) {
	for _, size := range []int{4 << 10, 64 << 10, 1 << 20, 8 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			body := []byte(`{"input":"` + strings.Repeat("history context ", size/16) + `"}`)
			input := requestmonitoring.TraceInput{ObservedAt: time.Unix(1700000000, 0), Method: "POST", Path: "/v1/responses", RequestBody: body, RequestHeaders: http.Header{"Content-Type": {"application/json"}}, Attempts: []requestmonitoring.TraceAttempt{{Number: 1, OutboundRequests: []requestmonitoring.TraceOutboundRequest{{Transport: "http", Method: "POST", URL: "https://fixture.invalid/responses", Headers: http.Header{"Content-Type": {"application/json"}}, Body: body}}}}, Final: requestmonitoring.TraceFinalError{HTTPStatus: 422}}
			b.ReportAllocs()
			b.SetBytes(int64(len(body) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				stored, err := encodeRequestTrace(input)
				if err != nil {
					b.Fatal(err)
				}
				if len(stored.Payload) == 0 {
					b.Fatal("empty trace")
				}
			}
		})
	}
}

// Queue insertion plus a synchronous drain, without encoding or database I/O.
func BenchmarkRequestTraceEnqueue(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			s := NewService(&traceLifecycleRepo{}, WithRequestTrace(true))
			trace := requestmonitoring.TraceInput{RequestBody: make([]byte, size)}
			event := requestmonitoring.EventInput{Method: "POST", RequestPath: "/v1/responses", Model: "gpt-test"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !s.RecordRequestTrace(context.Background(), event, trace) {
					b.Fatal("enqueue failed")
				}
				item := <-s.traceQueue
				s.traceQueuedBytes.Add(-item.Bytes)
			}
		})
	}
}
