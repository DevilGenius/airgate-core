package usageprojection

import (
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// These are the only source fields needed by projections; large metadata and
// request diagnostics never enter maintenance batches or transition projections.
const sourceColumns = `id,created_at,api_key_usage_logs,user_id_snapshot,user_usage_logs,user_email_snapshot,group_usage_logs,account_usage_logs,platform,model,input_tokens,output_tokens,cached_input_tokens,cache_creation_tokens,actual_cost,total_cost,billed_cost,duration_ms,first_event_ms,first_token_ms`

func shadowName(p projection, id string) string { return p.name + "_new_" + id }
func backupName(p projection, id string) string { return p.name + "_old_" + id }
func ledgerName(id string) string               { return "usage_rollup_events_" + id }
func captureName(id string) string              { return "usage_rollup_capture_" + id }

func (p projection) upsertSQL(target string) string {
	columns := append([]string(nil), p.keys...)
	selects := append([]string(nil), p.keyExpressions...)
	var groups, updates []string
	for i := range p.keys {
		groups = append(groups, fmt.Sprint(i+1))
	}
	for _, m := range p.metrics {
		columns = append(columns, m.name)
		selects = append(selects, "COALESCE("+m.expression+",0)")
		updates = append(updates, m.name+"=existing."+m.name+"+EXCLUDED."+m.name)
	}
	if p.name == "usage_hourly_rollups" {
		columns = append(columns, "user_email")
		selects = append(selects, "COALESCE(MAX(NULLIF(user_email_snapshot,'')),'')")
		updates = append(updates, "user_email=CASE WHEN EXCLUDED.user_email<>'' THEN EXCLUDED.user_email ELSE existing.user_email END")
	}
	columns = append(columns, "updated_at")
	selects = append(selects, "now()")
	updates = append(updates, "updated_at=now()")
	return "INSERT INTO " + pq.QuoteIdentifier(target) + " AS existing (" + strings.Join(columns, ",") + ") SELECT " + strings.Join(selects, ",") + " FROM batch GROUP BY " + strings.Join(groups, ",") + " ORDER BY " + strings.Join(groups, ",") + " ON CONFLICT (" + strings.Join(p.keys, ",") + ") DO UPDATE SET " + strings.Join(updates, ",") + " RETURNING 1"
}

func applyBatchSQL(id, source, finalSelect string, extraCTEs ...string) string {
	var ctes []string
	ctes = append(ctes, "picked AS MATERIALIZED ("+source+")")
	ctes = append(ctes, "claimed AS (INSERT INTO "+pq.QuoteIdentifier(ledgerName(id))+" (usage_id) SELECT id FROM picked ORDER BY id ON CONFLICT DO NOTHING RETURNING usage_id)")
	ctes = append(ctes, "batch AS MATERIALIZED (SELECT picked.* FROM picked JOIN claimed ON claimed.usage_id=picked.id)")
	for i, p := range projections() {
		ctes = append(ctes, fmt.Sprintf("projection_%d AS (%s)", i, p.upsertSQL(shadowName(p, id))))
	}
	ctes = append(ctes, extraCTEs...)
	return "WITH " + strings.Join(ctes, ",\n") + " " + finalSelect
}

func captureFunctionSQL(id, schema string) string {
	statement := applyBatchSQL(id, "SELECT "+sourceColumns+" FROM inserted_usage_rows", "SELECT COUNT(*) INTO applied_count FROM claimed")
	return "CREATE FUNCTION " + pq.QuoteIdentifier(captureName(id)) + `() RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, ` + pq.QuoteIdentifier(schema) + `, pg_temp AS $capture$
DECLARE applied_count bigint;
BEGIN
` + statement + `;
RETURN NULL;
END
$capture$`
}
