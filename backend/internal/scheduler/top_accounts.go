package scheduler

import (
	"container/heap"
	"math/rand"

	"github.com/DevilGenius/airgate-core/ent"
)

const maxLoadBalanceCandidates = 32

type scoredAccount struct {
	account *ent.Account
	score   float64
	tie     uint64
}
type bestAccounts []scoredAccount

func (h bestAccounts) Len() int { return len(h) }
func (h bestAccounts) Less(i, j int) bool {
	return h[i].score < h[j].score || (h[i].score == h[j].score && h[i].tie < h[j].tie)
}
func (h bestAccounts) Swap(i, j int)   { h[i], h[j] = h[j], h[i] }
func (h *bestAccounts) Push(value any) { *h = append(*h, value.(scoredAccount)) }
func (h *bestAccounts) Pop() any {
	old := *h
	v := old[len(old)-1]
	old[len(old)-1] = scoredAccount{}
	*h = old[:len(old)-1]
	return v
}
func (h *bestAccounts) consider(account *ent.Account, score float64) {
	item := scoredAccount{account: account, score: score, tie: rand.Uint64()}
	if len(*h) < maxLoadBalanceCandidates {
		heap.Push(h, item)
		return
	}
	first := (*h)[0]
	if item.score > first.score || (item.score == first.score && item.tie > first.tie) {
		(*h)[0] = item
		heap.Fix(h, 0)
	}
}
