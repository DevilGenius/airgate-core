package store

import (
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
)

func TestMapUsageLogPreservesUpstreamAccountType(t *testing.T) {
	deletedAt := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		account *ent.Account
		want    string
	}{
		{name: "API Key", account: &ent.Account{ID: 7, Type: "apikey"}, want: "apikey"},
		{name: "OAuth", account: &ent.Account{ID: 7, Type: "oauth"}, want: "oauth"},
		{name: "deleted API Key", account: &ent.Account{ID: 7, Type: "apikey", DeletedAt: &deletedAt}, want: "apikey"},
		{name: "missing account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := mapUsageLog(&ent.UsageLog{
				Stream:       false,
				DurationMs:   10001,
				FirstTokenMs: 10000,
				Edges:        ent.UsageLogEdges{Account: tc.account},
			})
			if record.AccountType != tc.want || record.Stream || record.DurationMs != 10001 || record.FirstTokenMs != 10000 {
				t.Fatalf("unexpected account type or timing: type=%q stream=%v duration=%d firstToken=%d", record.AccountType, record.Stream, record.DurationMs, record.FirstTokenMs)
			}
		})
	}
}
