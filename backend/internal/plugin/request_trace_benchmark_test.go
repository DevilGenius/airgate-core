package plugin

import (
	"fmt"
	"runtime"
	"testing"
)

func BenchmarkTraceIngressSnapshot(b *testing.B) {
	for _, size := range []int{4 << 10, 64 << 10, 1 << 20, 8 << 20} {
		body := make([]byte, size)
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("%dKiB/on=%t", size>>10, enabled), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size))
				for i := 0; i < b.N; i++ {
					var trace *requestTraceSession
					if enabled {
						trace = &requestTraceSession{}
					}
					trace.captureRequestBody(body, "application/json", int64(len(body)), nil)
					runtime.KeepAlive(trace)
				}
			})
		}
	}
}
