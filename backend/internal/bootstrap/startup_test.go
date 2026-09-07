package bootstrap

import (
	"strings"
	"testing"
)

func TestSplitSQLStatements(t *testing.T) {
	sql := `
-- comment ; stays with next statement
SELECT 'a;b';
DO $$
BEGIN
	RAISE NOTICE 'x;y';
END $$;
/* block ; comment */
CREATE INDEX CONCURRENTLY idx_example ON public.example (created_at);
`

	got := splitSQLStatements(sql)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(got), got)
	}
	if got[0] != "-- comment ; stays with next statement\nSELECT 'a;b'" {
		t.Fatalf("stmt[0] = %q", got[0])
	}
	if got[1] != "DO $$\nBEGIN\n\tRAISE NOTICE 'x;y';\nEND $$" {
		t.Fatalf("stmt[1] = %q", got[1])
	}
	if got[2] != "/* block ; comment */\nCREATE INDEX CONCURRENTLY idx_example ON public.example (created_at)" {
		t.Fatalf("stmt[2] = %q", got[2])
	}
}

func TestValidateSystemUpgradeFilename(t *testing.T) {
	valid := "20260528143015_usage_logs_upgrade.sql"
	if err := validateSystemUpgradeFilename(valid); err != nil {
		t.Fatalf("valid filename rejected: %v", err)
	}

	invalid := []string{
		"20260528_usage_logs_upgrade.sql",
		"202605281430_usage_logs_upgrade.sql",
		"20260528143015.sql",
		"20261328143015_usage_logs_upgrade.sql",
	}
	for _, name := range invalid {
		if err := validateSystemUpgradeFilename(name); err == nil {
			t.Fatalf("invalid filename accepted: %s", name)
		}
	}
}

func TestSystemUpgradeChecksumIgnoresLineEndings(t *testing.T) {
	lf := "-- description: Upgrade usage_logs table.\nSELECT 1;\n"
	crlf := "-- description: Upgrade usage_logs table.\r\nSELECT 1;\r\n"

	normalizedLF := normalizeSystemUpgradeSQL([]byte(lf))
	normalizedCRLF := normalizeSystemUpgradeSQL([]byte(crlf))
	if normalizedLF != normalizedCRLF {
		t.Fatalf("normalized SQL mismatch:\nLF:   %q\nCRLF: %q", normalizedLF, normalizedCRLF)
	}
	if systemUpgradeChecksum(normalizedLF) != systemUpgradeChecksum(normalizedCRLF) {
		t.Fatal("checksum should be stable across LF and CRLF line endings")
	}
}

func TestLegacyRollupDataIsNotBackfilledDuringStartup(t *testing.T) {
	for _, upgrade := range loadSystemUpgrades() {
		if upgrade.ID != "20260701012000_usage_hourly_rollups" {
			continue
		}
		checksum := systemUpgradeChecksum(upgrade.SQL)
		ddl, err := startupUpgradeSQL(upgrade)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(ddl, "CREATE TABLE") || strings.Contains(ddl, "TRUNCATE") || strings.Contains(ddl, "FROM public.usage_logs") {
			t.Fatalf("unsafe startup SQL: %s", ddl)
		}
		if systemUpgradeChecksum(upgrade.SQL) != checksum || !strings.Contains(upgrade.SQL, "TRUNCATE") {
			t.Fatal("published migration was changed")
		}
		return
	}
	t.Fatal("legacy migration missing")
}
