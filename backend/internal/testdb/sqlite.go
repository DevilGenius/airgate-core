package testdb

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/migrate"
)

// OpenEnt opens a SQLite-backed ent client for tests.
// The ent dialect stays SQLite while the database/sql driver is selected by
// build tags, so tests work both with cgo and with CGO_ENABLED=0.
func OpenEnt(t *testing.T, dsn string, migrateOpts ...entschema.MigrateOption) *ent.Client {
	t.Helper()

	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		t.Fatalf("open sqlite test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		t.Fatalf("enable sqlite foreign keys: %v", err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	tables, err := entschema.CopyTables(migrate.Tables)
	if err != nil {
		_ = client.Close()
		t.Fatalf("copy migration tables: %v", err)
	}
	if err := migrate.Create(context.Background(), client.Schema, tables, migrateOpts...); err != nil {
		_ = client.Close()
		t.Fatalf("migrate sqlite test db: %v", err)
	}
	return client
}

func OpenMemoryEnt(t *testing.T, name string, migrateOpts ...entschema.MigrateOption) *ent.Client {
	t.Helper()
	return OpenEnt(t, "file:"+name+"?mode=memory&cache=shared&_fk=1", migrateOpts...)
}
