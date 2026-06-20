package plugin

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestBuildPluginDSNQuotesOptionsSearchPath(t *testing.T) {
	t.Parallel()

	provisioner := &pluginDSNProvisioner{
		adminFields: dsnFields{
			"host":   "localhost",
			"port":   "5432",
			"dbname": "airgate",
		},
	}

	dsn := provisioner.buildPluginDSN("plugin_airgate-playground_role", "secret", "plugin_airgate-playground")

	if !strings.Contains(dsn, `options='-c search_path="plugin_airgate-playground"'`) {
		t.Fatalf("dsn = %q, want quoted options search_path", dsn)
	}
	if !strings.Contains(dsn, "user=plugin_airgate-playground_role") {
		t.Fatalf("dsn = %q, want plugin role", dsn)
	}
	if strings.Contains(dsn, " options=-c ") {
		t.Fatalf("dsn = %q, options value must not be split by spaces", dsn)
	}
}

func TestQuoteConninfoValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "plain", want: "plain"},
		{in: "", want: "''"},
		{in: "-c search_path=plugin", want: "'-c search_path=plugin'"},
		{in: "pa'ss\\word", want: `'pa\'ss\\word'`},
	}

	for _, tc := range cases {
		if got := quoteConninfoValue(tc.in); got != tc.want {
			t.Fatalf("quoteConninfoValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPluginDSNProvisionerPasswordPersistenceWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_dsn_password", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	provisioner := newPluginDSNProvisioner(db, "host=localhost dbname=airgate")
	password, err := provisioner.loadOrCreatePassword(ctx, "airgate-health")
	if err != nil {
		t.Fatalf("loadOrCreatePassword create: %v", err)
	}
	if len(password) != 48 {
		t.Fatalf("password length = %d, want 48", len(password))
	}
	for _, r := range password {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("password contains non-hex rune %q in %q", r, password)
		}
	}
	again, err := provisioner.loadOrCreatePassword(ctx, "airgate-health")
	if err != nil {
		t.Fatalf("loadOrCreatePassword read: %v", err)
	}
	if again != password {
		t.Fatalf("password changed: %q -> %q", password, again)
	}
}

func TestPluginDSNProvisionerErrorBranches(t *testing.T) {
	ctx := context.Background()

	if _, err := (&pluginDSNProvisioner{}).loadOrCreatePassword(ctx, "demo"); err == nil {
		t.Fatal("loadOrCreatePassword without ent db should fail")
	}
	if _, err := (&pluginDSNProvisioner{}).EnsureFor(ctx, "bad.id"); err == nil || !strings.Contains(err.Error(), "不合法") {
		t.Fatalf("EnsureFor invalid plugin id error = %v", err)
	}

	closedDB := testdb.OpenMemoryEnt(t, "plugin_dsn_password_closed", schema.WithGlobalUniqueID(false))
	if err := closedDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if _, err := newPluginDSNProvisioner(closedDB, "host=localhost").loadOrCreatePassword(ctx, "demo"); err == nil {
		t.Fatal("loadOrCreatePassword with closed db should fail")
	}
}
