package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	enttask "github.com/DevilGenius/airgate-core/ent/task"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type taskRuntimeExtension struct {
	pluginRuntimeExtension
	types   []string
	process func(context.Context, sdk.HostTask) error
}

func (p *taskRuntimeExtension) TaskTypes() []string {
	return p.types
}

func (p *taskRuntimeExtension) ProcessTask(ctx context.Context, task sdk.HostTask) error {
	if p.process != nil {
		return p.process(ctx, task)
	}
	return nil
}

func TestTaskTypesCacheAndManagerTaskRecoveryWithSQLite(t *testing.T) {
	cache := &taskTypesCache{}
	if got, ok := cache.get("missing"); ok || got != nil {
		t.Fatalf("empty cache get = %v/%v", got, ok)
	}
	cache.set("plugin-a", []string{"image", "video"})
	if got, ok := cache.get("plugin-a"); !ok || len(got) != 2 || got[0] != "image" || got[1] != "video" {
		t.Fatalf("cache get = %v/%v", got, ok)
	}

	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_task_recovery", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.hostFactory = &HostService{db: db}
	retryOnStartup, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(1).
		SetStatus(enttask.StatusProcessing).
		SetAttempts(0).
		SetMaxAttempts(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create retry startup task: %v", err)
	}
	failOnStartup, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(1).
		SetStatus(enttask.StatusProcessing).
		SetAttempts(2).
		SetMaxAttempts(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create fail startup task: %v", err)
	}
	manager.resetProcessingTasks(ctx)
	assertTaskStatus(t, db, retryOnStartup.ID, enttask.StatusRetrying, "recovered_on_startup")
	assertTaskStatus(t, db, failOnStartup.ID, enttask.StatusFailed, "failed")

	oldUpdatedAt := time.Now().Add(-taskStaleThreshold - time.Minute)
	retryStale, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(1).
		SetStatus(enttask.StatusProcessing).
		SetAttempts(0).
		SetMaxAttempts(2).
		SetUpdatedAt(oldUpdatedAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create retry stale task: %v", err)
	}
	failStale, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(1).
		SetStatus(enttask.StatusProcessing).
		SetAttempts(2).
		SetMaxAttempts(2).
		SetUpdatedAt(oldUpdatedAt).
		Save(ctx)
	if err != nil {
		t.Fatalf("create fail stale task: %v", err)
	}
	manager.recoverStaleTasks(ctx)
	assertTaskStatus(t, db, retryStale.ID, enttask.StatusRetrying, "recovered_retrying")
	assertTaskStatus(t, db, failStale.ID, enttask.StatusFailed, "failed")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	manager.StartTaskDispatcher(cancelled)
}

func TestDispatchPendingTasksMarksUnsupportedTaskTypesWithSQLite(t *testing.T) {
	oldCache := ttCache
	ttCache = &taskTypesCache{}
	t.Cleanup(func() { ttCache = oldCache })

	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_task_dispatch_unsupported", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	unsupported, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("video").
		SetUserID(1).
		SetStatus(enttask.StatusPending).
		Save(ctx)
	if err != nil {
		t.Fatalf("create unsupported task: %v", err)
	}
	missingPlugin, err := db.Task.Create().
		SetPluginID("missing-plugin").
		SetTaskType("image").
		SetUserID(1).
		SetStatus(enttask.StatusPending).
		Save(ctx)
	if err != nil {
		t.Fatalf("create missing plugin task: %v", err)
	}

	ttCache.set("plugin-a", []string{"image"})
	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.hostFactory = &HostService{db: db}
	manager.instances["plugin-a"] = &PluginInstance{Name: "plugin-a", Extension: &sdkgrpc.ExtensionGRPCClient{}}

	manager.dispatchPendingTasks(ctx)

	updatedUnsupported, err := db.Task.Get(ctx, unsupported.ID)
	if err != nil {
		t.Fatalf("get unsupported task: %v", err)
	}
	if updatedUnsupported.Status != enttask.StatusFailed ||
		updatedUnsupported.ErrorType != "invalid_task_type" ||
		updatedUnsupported.ErrorMessage != "plugin does not support task type" {
		t.Fatalf("unsupported task = status %s type %q message %q", updatedUnsupported.Status, updatedUnsupported.ErrorType, updatedUnsupported.ErrorMessage)
	}
	updatedMissing, err := db.Task.Get(ctx, missingPlugin.ID)
	if err != nil {
		t.Fatalf("get missing plugin task: %v", err)
	}
	if updatedMissing.Status != enttask.StatusPending {
		t.Fatalf("missing plugin task status = %s, want pending", updatedMissing.Status)
	}
}

func TestDispatchPluginTasksProcessesSupportedTasksWithSQLite(t *testing.T) {
	oldCache := ttCache
	ttCache = &taskTypesCache{}
	t.Cleanup(func() { ttCache = oldCache })

	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_task_dispatch_success", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	var processed sdk.HostTask
	client, cleanup := newExtensionRuntimeClient(t, &taskRuntimeExtension{
		pluginRuntimeExtension: pluginRuntimeExtension{id: "plugin-a"},
		types:                  []string{"image"},
		process: func(_ context.Context, task sdk.HostTask) error {
			processed = task
			return nil
		},
	})
	defer cleanup()

	task, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(7).
		SetStatus(enttask.StatusPending).
		SetInput(map[string]interface{}{"prompt": "sky"}).
		SetMaxAttempts(3).
		Save(ctx)
	if err != nil {
		t.Fatalf("create supported task: %v", err)
	}

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.hostFactory = &HostService{db: db}
	manager.instances["plugin-a"] = &PluginInstance{Name: "plugin-a", Extension: client}
	manager.dispatchPendingTasks(ctx)

	updated, err := db.Task.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("get processed task: %v", err)
	}
	if updated.Status != enttask.StatusCompleted || updated.Stage != "completed" || updated.Progress != 100 || updated.Attempts != 1 {
		t.Fatalf("processed task status/stage/progress/attempts = %s/%q/%d/%d", updated.Status, updated.Stage, updated.Progress, updated.Attempts)
	}
	if processed.ID != int64(task.ID) || processed.UserID != 7 || processed.TaskType != "image" || processed.Input["prompt"] != "sky" {
		t.Fatalf("processed host task = %+v", processed)
	}
}

func TestProcessOneTaskFailureRetryAndFinalFailureWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_task_process_failure", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	processErr := errors.New("processor failed")
	client, cleanup := newExtensionRuntimeClient(t, &taskRuntimeExtension{
		pluginRuntimeExtension: pluginRuntimeExtension{id: "plugin-a"},
		types:                  []string{"image"},
		process: func(context.Context, sdk.HostTask) error {
			return processErr
		},
	})
	defer cleanup()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.hostFactory = &HostService{db: db}
	inst := &PluginInstance{Name: "plugin-a", Extension: client}

	retryTask, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(7).
		SetStatus(enttask.StatusProcessing).
		SetAttempts(0).
		SetMaxAttempts(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create retry task: %v", err)
	}
	manager.processOneTask(ctx, inst, retryTask)
	retryUpdated, err := db.Task.Get(ctx, retryTask.ID)
	if err != nil {
		t.Fatalf("get retry task: %v", err)
	}
	if retryUpdated.Status != enttask.StatusRetrying || retryUpdated.Stage != "retrying" || retryUpdated.ErrorMessage != processErr.Error() {
		t.Fatalf("retry task = status %s stage %q error %q", retryUpdated.Status, retryUpdated.Stage, retryUpdated.ErrorMessage)
	}

	finalTask, err := db.Task.Create().
		SetPluginID("plugin-a").
		SetTaskType("image").
		SetUserID(7).
		SetStatus(enttask.StatusProcessing).
		SetAttempts(1).
		SetMaxAttempts(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create final task: %v", err)
	}
	manager.processOneTask(ctx, inst, finalTask)
	finalUpdated, err := db.Task.Get(ctx, finalTask.ID)
	if err != nil {
		t.Fatalf("get final task: %v", err)
	}
	if finalUpdated.Status != enttask.StatusFailed || finalUpdated.Stage != "failed" || finalUpdated.CompletedAt == nil {
		t.Fatalf("final task = status %s stage %q completed %v", finalUpdated.Status, finalUpdated.Stage, finalUpdated.CompletedAt)
	}
}

func assertTaskStatus(t *testing.T, db *ent.Client, id int, status enttask.Status, stage string) {
	t.Helper()
	task, err := db.Task.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get task %d: %v", id, err)
	}
	if task.Status != status || task.Stage != stage {
		t.Fatalf("task %d status/stage = %s/%q, want %s/%q", id, task.Status, task.Stage, status, stage)
	}
}
