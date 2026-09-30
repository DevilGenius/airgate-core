// Package accountcredentials owns automatic credential persistence. Manual account
// edits intentionally bypass this path so administrators can change a locked plan.
package accountcredentials

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"

	"github.com/DevilGenius/airgate-core/ent"
	entaccount "github.com/DevilGenius/airgate-core/ent/account"
	"github.com/DevilGenius/airgate-core/internal/accountidentity"
	"github.com/DevilGenius/airgate-core/internal/accountscope"
)

const PlanTypeLockedKey = "plan_type_locked"

func PlanTypeLocked(extra map[string]any) bool {
	locked, _ := extra[PlanTypeLockedKey].(bool)
	return locked
}

// UpdateAutomatic merges an automatic patch against the latest stored account.
// The optimistic write guard retries if an administrator changes the plan or its
// lock while the refresh is in flight, without holding a lock during network I/O.
func UpdateAutomatic(ctx context.Context, db *ent.Client, id int, patch map[string]string) (*ent.Account, error) {
	for attempt := 0; attempt < 5; attempt++ {
		current, err := accountscope.QueryByID(db, id).Only(ctx)
		if err != nil {
			return nil, accountscope.NormalizeNotFoundError(err)
		}
		email, credentials, err := accountidentity.Resolve(current.Email, current.Credentials)
		if err != nil {
			return nil, err
		}
		if credentials == nil {
			credentials = map[string]string{}
		}
		for key, value := range patch {
			if key == "email" {
				normalized, normalizeErr := accountidentity.NormalizeOptional(&value)
				if normalizeErr != nil {
					slog.Warn("invalid_automatic_account_email", "account_id", id, "error", normalizeErr)
				} else {
					email = normalized
				}
				continue
			}
			if key == "plan_type" && PlanTypeLocked(current.Extra) {
				continue
			}
			credentials[key] = value
		}
		credentials = accountidentity.SyncCredentials(credentials, email)
		emailEqual := (email == nil && current.Email == nil) || (email != nil && current.Email != nil && *email == *current.Email)
		if emailEqual && maps.Equal(credentials, current.Credentials) {
			return current, nil
		}
		builder := accountscope.UpdateOneID(db, id).
			Where(func(selector *sql.Selector) {
				if plan, ok := current.Credentials["plan_type"]; ok {
					selector.Where(sqljson.ValueEQ(entaccount.FieldCredentials, plan, sqljson.Path("plan_type")))
				} else {
					selector.Where(sql.Not(sqljson.HasKey(entaccount.FieldCredentials, sqljson.Path("plan_type"))))
				}
				if locked, ok := current.Extra[PlanTypeLockedKey]; ok && locked != nil {
					selector.Where(sqljson.ValueEQ(entaccount.FieldExtra, locked, sqljson.Path(PlanTypeLockedKey)))
				} else {
					selector.Where(sql.Or(
						sql.Not(sqljson.HasKey(entaccount.FieldExtra, sqljson.Path(PlanTypeLockedKey))),
						sqljson.ValueIsNull(entaccount.FieldExtra, sqljson.Path(PlanTypeLockedKey)),
					))
				}
			}).SetCredentials(credentials)
		if email == nil {
			builder.ClearEmail()
		} else {
			builder.SetEmail(*email)
		}
		updated, err := builder.Save(ctx)
		if ent.IsNotFound(err) {
			continue
		}
		return updated, err
	}
	return nil, fmt.Errorf("account %d changed repeatedly during credential refresh", id)
}
