package store

import (
	"context"
	stdsql "database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/lib/pq"

	"github.com/DevilGenius/airgate-core/ent/usagelog"
	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
)

// BuildPageIndex streams IDs only; it never loads UsageLog entities or historical
// bodies/costs. Compressing/ranking on Core avoids a large DB sort/hash aggregate
// on the small ARM server. The one SELECT defines the immutable browsing view.
func (s *UsageStore) BuildPageIndex(ctx context.Context, filter appusage.ListFilter) (*appusage.PageIndex, error) {
	driver := s.db.Driver()
	var models []string
	if driver.Dialect() == dialect.Postgres && filter.Model != "" {
		// Resolve substring/exclusion filters against the small model dictionary
		// using the same DB LIKE semantics. A repeatable-read view prevents a
		// newly inserted model between the two queries from being silently missed.
		tx, err := s.db.BeginTx(ctx, &stdsql.TxOptions{Isolation: stdsql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()
		driver = tx.Driver()
		matched, all, complete, err := usagePaginationModels(ctx, driver, filter.Model)
		if err != nil {
			return nil, err
		}
		if complete {
			if len(matched) == 0 {
				return &appusage.PageIndex{}, nil
			}
			filter.Model = ""
			if len(matched) < all {
				models = matched
			}
		}
	}
	table := sql.Table(usagelog.Table)
	selector := sql.Dialect(driver.Dialect()).Select(table.C(usagelog.FieldID)).From(table)
	if filter.UserID != nil {
		// Same owner semantics as usageUserPredicate, including legacy rows with
		// a zero snapshot. Foreign keys guarantee that the relation is valid.
		selector.Where(sql.P(func(b *sql.Builder) {
			b.WriteString(fmt.Sprintf("COALESCE(NULLIF(%s, 0), %s, 0) = ", selector.C(usagelog.FieldUserIDSnapshot), selector.C(usagelog.UserColumn))).Arg(*filter.UserID)
		}))
	}
	for _, predicate := range usageListPredicates(filter) {
		predicate(selector)
	}
	if len(models) > 0 {
		usagelog.ModelIn(models...)(selector)
	}
	selector.OrderBy(sql.Desc(table.C(usagelog.FieldID)))
	query, args := selector.Query()
	var rows sql.Rows
	if err := driver.Query(ctx, query, args, &rows); err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	index := &appusage.PageIndex{}
	var id int64
	for rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if err := index.AddID(id); err != nil {
			return nil, err
		}
		if index.Total%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return index, nil
}

// Seek to the next distinct model using the existing model B-tree, rather than
// DISTINCT-scanning millions of index entries. Limit the dictionary; on overflow
// or nondeterministic collation retain the original predicate without truncation.
func usagePaginationModels(ctx context.Context, driver dialect.Driver, raw string) ([]string, int, bool, error) {
	table := sql.Table("model_names")
	selector := sql.Dialect(dialect.Postgres).Select(table.C("model")).From(table).Where(sql.NotNull(table.C("model")))
	for _, predicate := range usageStatsPredicates(appusage.StatsFilter{Model: raw}) {
		predicate(selector)
	}
	matched, args := selector.Query()
	query := `WITH RECURSIVE model_names(model, depth) AS (
		SELECT MIN(model), 1 FROM public.usage_logs
		UNION ALL
		SELECT (SELECT MIN(u.model) FROM public.usage_logs u WHERE u.model > m.model), m.depth+1
		FROM model_names m WHERE m.model IS NOT NULL AND m.depth < 513
	), matching_models AS (` + matched + `)
	SELECT COALESCE(array_agg(model), ARRAY[]::text[]),
		(SELECT COUNT(*) FROM model_names WHERE model IS NOT NULL),
		NOT EXISTS (
			SELECT 1 FROM pg_attribute a JOIN pg_collation c ON a.attcollation=c.oid
			WHERE a.attrelid='public.usage_logs'::regclass AND a.attname='model' AND NOT c.collisdeterministic
		)
	FROM matching_models`
	var rows sql.Rows
	if err := driver.Query(ctx, query, args, &rows); err != nil {
		return nil, 0, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, 0, false, rows.Err()
	}
	var models pq.StringArray
	var count int
	var deterministic bool
	if err := rows.Scan(&models, &count, &deterministic); err != nil {
		return nil, 0, false, err
	}
	return []string(models), count, deterministic && count < 513, rows.Err()
}
