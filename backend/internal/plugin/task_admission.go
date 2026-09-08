package plugin

import (
	"context"
	"sync"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DevilGenius/airgate-core/ent"
	enttask "github.com/DevilGenius/airgate-core/ent/task"
)

const (
	maxQueuedTasks       = 2000
	maxPluginQueuedTasks = 500
	maxUserQueuedTasks   = 100
)

var localTaskAdmission sync.Mutex

func checkTaskQueueCapacity(ctx context.Context, db *ent.Client, pluginID string, userID int) error {
	var rows []struct {
		PluginID string `json:"plugin_id"`
		UserID   int    `json:"user_id"`
	}
	// Use the existing status index and inspect at most the active queue budget;
	// do not scan a user's potentially large completed-task history for each cap.
	err := db.Task.Query().Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying, enttask.StatusProcessing, enttask.StatusCancelling)).Limit(maxQueuedTasks).Select(enttask.FieldPluginID, enttask.FieldUserID).Scan(ctx, &rows)
	if err != nil {
		return err
	}
	pluginCount, userCount := 0, 0
	for _, row := range rows {
		if row.PluginID == pluginID {
			pluginCount++
		}
		if row.UserID == userID {
			userCount++
		}
	}
	if len(rows) >= maxQueuedTasks || pluginCount >= maxPluginQueuedTasks || userCount >= maxUserQueuedTasks {
		return status.Error(codes.ResourceExhausted, "task queue capacity exceeded; retry after current tasks finish")
	}
	return nil
}

func lockTaskAdmission(ctx context.Context, tx *ent.Tx) (func(), error) {
	if tx.Driver().Dialect() != dialect.Postgres {
		if !localTaskAdmission.TryLock() {
			return nil, status.Error(codes.ResourceExhausted, "task admission busy")
		}
		return localTaskAdmission.Unlock, nil
	}
	var rows entsql.Rows
	if err := tx.Driver().Query(ctx, "SELECT pg_try_advisory_xact_lock(20260908260000)", []any{}, &rows); err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var acquired bool
	if !rows.Next() {
		return nil, status.Error(codes.Unavailable, "task admission lock unavailable")
	}
	if err := rows.Scan(&acquired); err != nil {
		return nil, err
	}
	if !acquired {
		return nil, status.Error(codes.ResourceExhausted, "task admission busy")
	}
	return func() {}, nil
}
