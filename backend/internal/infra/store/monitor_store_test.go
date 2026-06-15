package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/migrate"
	entmonitorevent "github.com/DevilGenius/airgate-core/ent/monitorevent"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestMonitorStoreSummaryAggregatesSeverityCounts(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "monitor_store_summary", migrate.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now()
	createMonitorEvent(t, db, 1, entmonitorevent.SeverityCritical, entmonitorevent.StatusActive, now)
	createMonitorEvent(t, db, 2, entmonitorevent.SeverityCritical, entmonitorevent.StatusResolved, now)
	createMonitorEvent(t, db, 3, entmonitorevent.SeverityError, entmonitorevent.StatusActive, now)
	createMonitorEvent(t, db, 4, entmonitorevent.SeverityError, entmonitorevent.StatusActive, now)
	createMonitorEvent(t, db, 5, entmonitorevent.SeverityWarning, entmonitorevent.StatusActive, now)
	createMonitorEvent(t, db, 6, entmonitorevent.SeverityInfo, entmonitorevent.StatusResolved, now)

	summary, err := NewMonitorStore(db).Summary(ctx)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}

	if summary.ActiveTotal != 4 {
		t.Fatalf("ActiveTotal = %d, want 4", summary.ActiveTotal)
	}
	if summary.CriticalTotal != 2 || summary.CriticalActiveTotal != 1 {
		t.Fatalf("critical counts = %d/%d, want 2/1", summary.CriticalTotal, summary.CriticalActiveTotal)
	}
	if summary.ErrorTotal != 2 || summary.ErrorActiveTotal != 2 {
		t.Fatalf("error counts = %d/%d, want 2/2", summary.ErrorTotal, summary.ErrorActiveTotal)
	}
	if summary.WarningTotal != 1 || summary.WarningActiveTotal != 1 {
		t.Fatalf("warning counts = %d/%d, want 1/1", summary.WarningTotal, summary.WarningActiveTotal)
	}
	if summary.InfoTotal != 1 || summary.InfoActiveTotal != 0 {
		t.Fatalf("info counts = %d/%d, want 1/0", summary.InfoTotal, summary.InfoActiveTotal)
	}
}

func createMonitorEvent(t *testing.T, db *ent.Client, id int, severity entmonitorevent.Severity, status entmonitorevent.Status, now time.Time) {
	t.Helper()

	if _, err := db.MonitorEvent.Create().
		SetType(entmonitorevent.TypeSystemError).
		SetSeverity(severity).
		SetStatus(status).
		SetHash(fmt.Sprintf("hash-%d", id)).
		SetTitle("test event").
		SetCreatedAt(now.Add(time.Duration(id) * time.Second)).
		SetUpdatedAt(now.Add(time.Duration(id) * time.Second)).
		SetExpiresAt(now.Add(time.Hour)).
		Save(context.Background()); err != nil {
		t.Fatalf("create monitor event %d: %v", id, err)
	}
}
