package store

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/DevilGenius/airgate-core/ent"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
)

// allocateAccountName participates in the account write transaction. Counters
// survive deletion and are never inferred from mutable display names.
func allocateAccountName(ctx context.Context, tx *ent.Tx, input appaccount.CreateInput) (string, error) {
	return allocateAccountNameAt(ctx, tx, input, time.Now())
}

func allocateAccountNameAt(ctx context.Context, tx *ent.Tx, input appaccount.CreateInput, now time.Time) (string, error) {
	if !input.AutoName {
		return input.Name, nil
	}
	current := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	plan := appaccount.AccountNamePlan(input.Credentials["plan_type"])
	const query = `INSERT INTO account_name_counters (platform, naming_date, plan_type, last_index)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (platform, naming_date, plan_type)
		DO UPDATE SET last_index = account_name_counters.last_index + 1
		RETURNING last_index`
	var rows sql.Rows
	if err := tx.Driver().Query(ctx, query, []any{input.Platform, current.Format("2006-01-02"), plan}, &rows); err != nil {
		return "", fmt.Errorf("allocate account name: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var index int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("account name allocation returned no index")
	}
	if err := rows.Scan(&index); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%d", current.Format("0102"), plan, index), nil
}
