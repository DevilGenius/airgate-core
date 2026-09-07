package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestDeferredUsageIndexesPreserveConstraintsAndDescriptors(t *testing.T) {
	usage := &schema.Table{Name: "usage_logs", Indexes: []*schema.Index{{Name: "usage_date"}, {Name: "billing_event", Unique: true}}}
	users := &schema.Table{Name: "users", Indexes: []*schema.Index{{Name: "user_date"}}}
	next := schema.CreateFunc(func(_ context.Context, tables ...*schema.Table) error {
		if len(tables[0].Indexes) != 1 || tables[0].Indexes[0].Name != "billing_event" {
			t.Fatal("required uniqueness was removed or a large index remained")
		}
		if tables[1] != users || len(tables[1].Indexes) != 1 {
			t.Fatal("unrelated schema changed")
		}
		return nil
	})
	if err := DeferUsageIndexes(next).Create(t.Context(), usage, users); err != nil {
		t.Fatal(err)
	}
	if len(usage.Indexes) != 2 {
		t.Fatal("generated descriptor was mutated")
	}
}

func TestLocalInvalidConcurrentIndexCanBeRebuilt(t *testing.T) {
	path := os.Getenv("AIRGATE_REVIEW_CONFIG")
	if path == "" {
		t.Skip("set AIRGATE_REVIEW_CONFIG for isolated PostgreSQL index tests")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal("load local configuration failed")
	}
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	name := "ag_review_r08_index_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	index := name + "_idx"
	if _, err = conn.ExecContext(ctx, "CREATE TABLE public."+pq.QuoteIdentifier(name)+" (value integer)"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := conn.ExecContext(cleanup, "DROP TABLE public."+pq.QuoteIdentifier(name)); err != nil {
			t.Error(err)
		}
	}()
	if _, err = conn.ExecContext(ctx, "INSERT INTO public."+pq.QuoteIdentifier(name)+" VALUES(1),(1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, "CREATE UNIQUE INDEX CONCURRENTLY "+pq.QuoteIdentifier(index)+" ON public."+pq.QuoteIdentifier(name)+"(value)"); err == nil {
		t.Fatal("duplicate unique index unexpectedly succeeded")
	}
	created, err := ensureUsageIndex(ctx, conn, index, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+pq.QuoteIdentifier(index)+" ON public."+pq.QuoteIdentifier(name)+"(value)")
	if err != nil || !created {
		t.Fatalf("invalid index was not repaired: %v/%v", created, err)
	}
	var count int
	if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM public."+pq.QuoteIdentifier(name)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("index repair changed data")
	}
}
