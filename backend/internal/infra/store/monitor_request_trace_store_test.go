package store

import (
	"testing"
	"time"
)

func TestClearRequestTracesUsesStrictLastSeenCutoff(t *testing.T) {
	db := enttestOpen(t)
	defer func() { _ = db.Close() }()
	cutoff := time.Unix(1700000000, 0).UTC()
	for _, row := range []struct {
		hash     string
		lastSeen time.Time
	}{
		{"old", cutoff.Add(-time.Second)}, {"equal", cutoff}, {"recent", cutoff.Add(time.Second)},
	} {
		_, err := db.MonitorRequestTrace.Create().SetHash(row.hash).SetPayload([]byte(row.hash)).
			SetFirstSeenAt(cutoff.Add(-24 * time.Hour)).SetLastSeenAt(row.lastSeen).Save(t.Context())
		if err != nil {
			t.Fatal(err)
		}
	}
	store := NewMonitorStore(db)
	deleted, err := store.ClearRequestTraces(t.Context(), &cutoff)
	if err != nil || deleted != 1 {
		t.Fatalf("cutoff cleanup = %d / %v", deleted, err)
	}
	rows, err := db.MonitorRequestTrace.Query().All(t.Context())
	if err != nil || len(rows) != 2 {
		t.Fatalf("remaining traces = %d / %v", len(rows), err)
	}
	for _, row := range rows {
		if row.Hash == "old" {
			t.Fatal("old trace survived cutoff")
		}
	}
	zero := time.Time{}
	deleted, err = store.ClearRequestTraces(t.Context(), &zero)
	if err != nil || deleted != 2 {
		t.Fatalf("zero cutoff cleanup = %d / %v", deleted, err)
	}
}
