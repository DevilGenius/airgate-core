package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	enttask "github.com/DevilGenius/airgate-core/ent/task"
	"github.com/DevilGenius/airgate-core/internal/safego"
	pb "github.com/DevilGenius/airgate-sdk/protocol/proto"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

const (
	taskDispatchInterval = 3 * time.Second
	taskProcessTimeout   = 10 * time.Minute
	taskStaleThreshold   = 10 * time.Minute
	taskRecoverInterval  = 30 * time.Second
	taskRecoverLimit     = 100
	maxPluginConcurrency = 5
)

// taskTypesCache caches GetTaskTypes results per plugin to avoid gRPC calls every dispatch cycle.
type taskTypesCache struct {
	mu    sync.RWMutex
	types map[string][]string // pluginID → task types
}

func (c *taskTypesCache) get(pluginID string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.types[pluginID]
	return t, ok
}

func (c *taskTypesCache) set(pluginID string, types []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.types == nil {
		c.types = make(map[string][]string)
	}
	c.types[pluginID] = types
}

// StartTaskDispatcher 启动任务分发循环。在 Manager 启动时调用。
// 启动前先将所有遗留的 processing 任务重置为 retrying，确保服务重启后立即恢复。
func (m *Manager) StartTaskDispatcher(ctx context.Context) {
	p := m.taskWorkers()
	p.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		p.mu.Lock()
		p.cancel = cancel
		stopping := p.stopping
		p.mu.Unlock()
		if stopping {
			cancel()
			return
		}
		m.resetProcessingTasks(ctx)
		safego.Go("task_dispatch_loop", func() { m.taskDispatchLoop(ctx) })
		safego.Go("task_recover_loop", func() { m.taskRecoverLoop(ctx) })
		slog.Info("task_dispatcher_started")
	})
}

// resetProcessingTasks 将服务重启前残留的 processing 任务重置为 retrying/failed，
// 使 dispatch 循环能立即重新接管。
func (m *Manager) resetProcessingTasks(ctx context.Context) {
	if m.hostFactory == nil || m.hostFactory.db == nil {
		return
	}
	db := m.hostFactory.db

	staleTasks, err := db.Task.Query().
		Where(enttask.StatusEQ(enttask.StatusProcessing)).
		All(ctx)
	if err != nil || len(staleTasks) == 0 {
		return
	}

	now := time.Now()
	var recoveredCount, failedCount int
	for _, st := range staleTasks {
		if st.Attempts < st.MaxAttempts {
			if err := db.Task.UpdateOneID(st.ID).
				SetStatus(enttask.StatusRetrying).
				SetStage("recovered_on_startup").
				SetErrorMessage("recovered: service restarted").
				Exec(ctx); err != nil {
				slog.Error("task_startup_recover_failed", "task_id", st.ID, sdk.LogFieldError, err)
			} else {
				recoveredCount++
			}
		} else {
			if err := db.Task.UpdateOneID(st.ID).
				SetStatus(enttask.StatusFailed).
				SetStage("failed").
				SetErrorMessage(fmt.Sprintf("service restarted after %d attempts", st.MaxAttempts)).
				SetCompletedAt(now).
				Exec(ctx); err != nil {
				slog.Error("task_startup_fail_failed", "task_id", st.ID, sdk.LogFieldError, err)
			} else {
				failedCount++
			}
		}
	}
	if recoveredCount > 0 || failedCount > 0 {
		slog.Info("task_startup_reset", "recovered", recoveredCount, "failed", failedCount)
	}
}

func (m *Manager) taskDispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(taskDispatchInterval)
	defer ticker.Stop()
	wake := m.taskWorkers().wake

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.dispatchPendingTasks(ctx)
		case <-wake:
			m.dispatchPendingTasks(ctx)
		}
	}
}

var ttCache = &taskTypesCache{}

func (m *Manager) dispatchPendingTasks(ctx context.Context) {
	if m.hostFactory == nil || m.hostFactory.db == nil {
		return
	}
	if !m.taskDispatchMu.TryLock() {
		return
	}
	defer m.taskDispatchMu.Unlock()
	db := m.hostFactory.db
	p := m.taskWorkers()
	m.mu.RLock()
	plugins := make([]string, 0, len(m.instances))
	for id, inst := range m.instances {
		if inst != nil && inst.Extension != nil {
			plugins = append(plugins, id)
		}
	}
	m.mu.RUnlock()
	if len(plugins) == 0 {
		return
	}
	sort.Strings(plugins)
	p.mu.Lock()
	start := p.cursor % len(plugins)
	p.cursor++
	p.mu.Unlock()
	for i := range plugins {
		pluginID := plugins[(start+i)%len(plugins)]
		free := p.available(pluginID)
		if free <= 0 {
			continue
		}
		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		tasks, err := db.Task.Query().Where(enttask.PluginIDEQ(pluginID), enttask.Or(enttask.StatusEQ(enttask.StatusPending), enttask.And(enttask.StatusEQ(enttask.StatusRetrying), enttask.UpdatedAtLTE(time.Now().Add(-taskDispatchInterval))))).Order(ent.Desc(enttask.FieldPriority), ent.Asc(enttask.FieldCreatedAt), ent.Asc(enttask.FieldID)).Limit(free).All(queryCtx)
		cancel()
		if err != nil {
			slog.Warn("task_dispatch_query_failed", sdk.LogFieldPluginID, pluginID, sdk.LogFieldError, err)
			continue
		}
		m.dispatchPluginTasks(ctx, pluginID, tasks)
	}
}

func (m *Manager) getPluginTaskTypes(ctx context.Context, pluginID string, ext *sdkgrpc.ExtensionGRPCClient) (map[string]bool, error) {
	cached, ok := ttCache.get(pluginID)
	if ok {
		result := make(map[string]bool, len(cached))
		for _, t := range cached {
			result[t] = true
		}
		return result, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	types, err := ext.GetTaskTypes(ctx)
	if err != nil {
		return nil, err
	}
	ttCache.set(pluginID, types)

	result := make(map[string]bool, len(types))
	for _, t := range types {
		result[t] = true
	}
	return result, nil
}

func (m *Manager) dispatchPluginTasks(ctx context.Context, pluginID string, tasks []*ent.Task) {
	m.mu.RLock()
	inst, ok := m.instances[pluginID]
	m.mu.RUnlock()
	if !ok || inst == nil || inst.Extension == nil {
		slog.Warn("task_dispatch_plugin_not_found", sdk.LogFieldPluginID, pluginID)
		return
	}

	typeSet, err := m.getPluginTaskTypes(ctx, pluginID, inst.Extension)
	if err != nil {
		slog.Warn("task_dispatch_get_types_failed", sdk.LogFieldPluginID, pluginID, sdk.LogFieldError, err)
		return
	}

	db := m.hostFactory.db
	pool := m.taskWorkers()

	for _, t := range tasks {
		if !typeSet[t.TaskType] {
			slog.Warn("task_dispatch_unsupported_type",
				sdk.LogFieldPluginID, pluginID, "task_type", t.TaskType, "task_id", t.ID)
			if err := db.Task.UpdateOneID(t.ID).
				SetStatus(enttask.StatusFailed).
				SetErrorType("invalid_task_type").
				SetErrorMessage("plugin does not support task type").
				SetCompletedAt(time.Now()).
				Exec(ctx); err != nil {
				slog.Error("task_dispatch_unsupported_type_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			}
			continue
		}

		// Mark as processing
		if !pool.begin(pluginID) {
			return
		}
		if !inst.acquireRequest() {
			pool.finish(pluginID, false)
			return
		}
		if err := claimDispatchTask(ctx, db, t); err != nil {
			inst.releaseRequest()
			pool.finish(pluginID, false)
			if ent.IsNotFound(err) {
				continue
			}
			slog.Error("task_dispatch_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			continue
		}

		task := t
		safego.Go("task_process", func() {
			defer pool.finish(pluginID, true)
			defer inst.releaseRequest()
			m.processOneTask(ctx, inst, task)
		})
	}
}

func claimDispatchTask(ctx context.Context, db *ent.Client, task *ent.Task) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := db.Task.UpdateOneID(task.ID).Where(enttask.StatusIn(enttask.StatusPending, enttask.StatusRetrying)).SetStatus(enttask.StatusProcessing).SetStage("dispatching").SetStartedAt(time.Now()).SetAttempts(task.Attempts + 1).Save(ctx)
	return err
}

func (m *Manager) processOneTask(ctx context.Context, inst *PluginInstance, t *ent.Task) {
	taskCtx, cancel := context.WithTimeout(ctx, taskProcessTimeout)
	defer cancel()

	inputJSON, _ := json.Marshal(t.Input)
	resp, err := inst.Extension.ProcessTask(taskCtx, &pb.ProcessTaskRequest{
		TaskId:   int64(t.ID),
		TaskType: t.TaskType,
		Input:    inputJSON,
		UserId:   int64(t.UserID),
	})

	db := m.hostFactory.db

	if err != nil || (resp != nil && !resp.Success) {
		errMsg := "processing failed"
		if err != nil {
			errMsg = err.Error()
		} else if resp != nil && resp.ErrorMessage != "" {
			errMsg = resp.ErrorMessage
		}

		slog.Error("task_process_failed",
			"task_id", t.ID, sdk.LogFieldPluginID, inst.Name, sdk.LogFieldError, errMsg)

		// t.Attempts is the pre-increment value; DB already has attempts+1
		if t.Attempts+1 < t.MaxAttempts {
			if err := db.Task.UpdateOneID(t.ID).
				SetStatus(enttask.StatusRetrying).
				SetStage("retrying").
				SetErrorMessage(errMsg).
				Exec(ctx); err != nil {
				slog.Error("task_retry_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			}
		} else {
			now := time.Now()
			if err := db.Task.UpdateOneID(t.ID).
				SetStatus(enttask.StatusFailed).
				SetStage("failed").
				SetErrorMessage(errMsg).
				SetCompletedAt(now).
				Exec(ctx); err != nil {
				slog.Error("task_fail_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
			}
		}
		return
	}

	// Plugin reported success. If the plugin already called host.UpdateTask(completed),
	// the task is already marked done. If not, mark it completed as a safety net.
	current, err := db.Task.Get(ctx, t.ID)
	if err == nil && current.Status == enttask.StatusProcessing {
		now := time.Now()
		if err := db.Task.UpdateOneID(t.ID).
			SetStatus(enttask.StatusCompleted).
			SetProgress(100).
			SetStage("completed").
			SetCompletedAt(now).
			Exec(ctx); err != nil {
			slog.Error("task_complete_update_failed", "task_id", t.ID, sdk.LogFieldError, err)
		}
	}

	slog.Info("task_process_completed", "task_id", t.ID, sdk.LogFieldPluginID, inst.Name)
}

// taskRecoverLoop 定期恢复僵尸任务（processing 超时未完成）。
func (m *Manager) taskRecoverLoop(ctx context.Context) {
	ticker := time.NewTicker(taskRecoverInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.recoverStaleTasks(ctx)
		}
	}
}

func (m *Manager) recoverStaleTasks(ctx context.Context) {
	if m.hostFactory == nil || m.hostFactory.db == nil {
		return
	}
	db := m.hostFactory.db
	threshold := time.Now().Add(-taskStaleThreshold)

	staleTasks, err := db.Task.Query().
		Where(
			enttask.StatusEQ(enttask.StatusProcessing),
			enttask.UpdatedAtLT(threshold),
		).
		Limit(taskRecoverLimit).
		All(ctx)
	if err != nil || len(staleTasks) == 0 {
		return
	}

	now := time.Now()
	recoveredCount := 0
	failedCount := 0
	for _, st := range staleTasks {
		if st.Attempts < st.MaxAttempts {
			if err := db.Task.UpdateOneID(st.ID).
				SetStatus(enttask.StatusRetrying).
				SetStage("recovered_retrying").
				SetErrorMessage("recovered: processing timeout").
				Exec(ctx); err != nil {
				slog.Error("task_recover_update_failed", "task_id", st.ID, sdk.LogFieldError, err)
			}
			recoveredCount++
		} else {
			if err := db.Task.UpdateOneID(st.ID).
				SetStatus(enttask.StatusFailed).
				SetStage("failed").
				SetErrorMessage(fmt.Sprintf("timed out after %d attempts", st.MaxAttempts)).
				SetCompletedAt(now).
				Exec(ctx); err != nil {
				slog.Error("task_fail_update_failed", "task_id", st.ID, sdk.LogFieldError, err)
			}
			failedCount++
		}
	}
	if recoveredCount > 0 {
		slog.Warn("task_recovered_to_retrying", "count", recoveredCount)
	}
	if failedCount > 0 {
		slog.Warn("task_failed_timeout", "count", failedCount)
	}
}
