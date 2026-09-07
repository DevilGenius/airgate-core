package billing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/DevilGenius/airgate-core/ent"
	"github.com/lib/pq"
)

func sortedBillingIDs[V any](values map[int]V) []int {
	ids := make([]int, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// Acquire row locks explicitly before a bulk UPDATE: a VALUES/UPDATE plan by
// itself does not guarantee lock order. NO KEY UPDATE stays compatible with the
// foreign-key checks made by concurrent usage inserts.
func lockBillingRows(ctx context.Context, tx *ent.Tx, table string, ids []int, required bool) error {
	if len(ids) == 0 {
		return nil
	}
	var rows entsql.Rows
	query := "SELECT id FROM " + pq.QuoteIdentifier(table) + " WHERE id = ANY($1::int[]) ORDER BY id FOR NO KEY UPDATE"
	if err := tx.Driver().Query(ctx, query, []any{pq.Array(ids)}, &rows); err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if required && count != len(ids) {
		return fmt.Errorf("billing %s: expected %d rows, found %d", table, len(ids), count)
	}
	return nil
}

func execBillingUpdate(ctx context.Context, tx *ent.Tx, query string, args []any) error {
	var result entsql.Result
	return tx.Driver().Exec(ctx, query, args, &result)
}

func updateAccountTimesBatch(ctx context.Context, tx *ent.Tx, ids []int, latest map[int]time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	if err := lockBillingRows(ctx, tx, "accounts", ids, false); err != nil {
		return err
	}
	args := make([]any, 0, 2*len(ids))
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id, latest[id])
		values = append(values, fmt.Sprintf("($%d::int,$%d::timestamptz)", len(args)-1, len(args)))
	}
	return execBillingUpdate(ctx, tx, `UPDATE accounts AS a SET last_used_at=v.used_at, updated_at=now()
FROM (VALUES `+strings.Join(values, ",")+`) AS v(id,used_at)
WHERE a.id=v.id AND a.deleted_at IS NULL AND (a.last_used_at IS NULL OR a.last_used_at<v.used_at)`, args)
}

func applyUsageChargesBatch(ctx context.Context, tx *ent.Tx, users, billed, actual map[int]float64) error {
	userIDs := sortedBillingIDs(users)
	if err := lockBillingRows(ctx, tx, "users", userIDs, true); err != nil {
		return err
	}
	if len(userIDs) > 0 {
		var args []any
		var values []string
		for _, id := range userIDs {
			args = append(args, id, users[id])
			values = append(values, fmt.Sprintf("($%d::int,$%d::numeric)", len(args)-1, len(args)))
		}
		if err := execBillingUpdate(ctx, tx, `UPDATE users AS u SET balance=u.balance-v.cost, updated_at=now()
FROM (VALUES `+strings.Join(values, ",")+`) AS v(id,cost) WHERE u.id=v.id`, args); err != nil {
			return err
		}
	}
	keys := make(map[int]struct{}, len(billed)+len(actual))
	for id := range billed {
		keys[id] = struct{}{}
	}
	for id := range actual {
		keys[id] = struct{}{}
	}
	keyIDs := sortedBillingIDs(keys)
	if len(keyIDs) == 0 {
		return nil
	}
	if err := lockBillingRows(ctx, tx, "api_keys", keyIDs, true); err != nil {
		return err
	}
	var args []any
	var values []string
	for _, id := range keyIDs {
		args = append(args, id, billed[id], actual[id])
		values = append(values, fmt.Sprintf("($%d::int,$%d::numeric,$%d::numeric)", len(args)-2, len(args)-1, len(args)))
	}
	return execBillingUpdate(ctx, tx, `UPDATE api_keys AS k
SET used_quota=k.used_quota+v.billed, used_quota_actual=k.used_quota_actual+v.actual, updated_at=now()
FROM (VALUES `+strings.Join(values, ",")+`) AS v(id,billed,actual) WHERE k.id=v.id`, args)
}
