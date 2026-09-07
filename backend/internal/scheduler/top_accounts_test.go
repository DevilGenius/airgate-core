package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
)

func TestLoadBalanceKeepsOnlyBestCandidatesWithoutRescanningLoads(t *testing.T) {
	candidates := make([]*ent.Account, 10000)
	for i := range candidates {
		candidates[i] = &ent.Account{ID: i + 1, Priority: 1, MaxConcurrency: 10001}
	}
	reads := 0
	s := &Scheduler{currentLoad: func(_ context.Context, id int) int { reads++; return id }}
	for range 20 {
		reads = 0
		chosen := s.selectByLoadBalance(t.Context(), candidates, time.Now(), nil)
		if chosen.ID < 1 || chosen.ID > maxLoadBalanceCandidates || reads != len(candidates) {
			t.Fatalf("selection=%d load reads=%d", chosen.ID, reads)
		}
	}
}

func BenchmarkLoadBalanceLargePool(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			candidates := make([]*ent.Account, size)
			for i := range candidates {
				candidates[i] = &ent.Account{ID: i + 1, MaxConcurrency: 10}
			}
			s := &Scheduler{}
			snapshot := &selectionSnapshot{hasLoads: true}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.selectByLoadBalance(b.Context(), candidates, time.Now(), snapshot)
			}
		})
	}
}
