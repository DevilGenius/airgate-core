package bootstrap

import "testing"

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
