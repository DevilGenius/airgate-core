package plugin

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/DevilGenius/airgate-core/ent"
	enttask "github.com/DevilGenius/airgate-core/ent/task"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTaskDispatcherBypassesDisabledPluginAndRefillsBeforeSlowTaskFinishes(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	db := testdb.OpenMemoryEnt(t, "task_fairness", schema.WithGlobalUniqueID(false))
	defer db.Close()
	started := make(chan int64, 16)
	releaseSlow := make(chan struct{})
	client, cleanup := newExtensionRuntimeClient(t, &taskRuntimeExtension{pluginRuntimeExtension: pluginRuntimeExtension{id: "fair-plugin"}, types: []string{"image"}, process: func(ctx context.Context, task sdk.HostTask) error {
		started <- task.ID
		if slow, _ := task.Input["slow"].(bool); slow {
			select {
			case <-releaseSlow:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}})
	defer cleanup()
	for range 12 {
		db.Task.Create().SetPluginID("disabled").SetTaskType("image").SetUserID(1).SetPriority(100).SaveX(ctx)
	}
	last := 0
	for i := range 6 {
		task := db.Task.Create().SetPluginID("fair-plugin").SetTaskType("image").SetUserID(1).SetInput(map[string]interface{}{"slow": i == 0}).SaveX(ctx)
		last = task.ID
	}
	m := NewManager(t.TempDir(), "debug", "", nil)
	m.hostFactory = &HostService{db: db}
	m.instances["fair-plugin"] = &PluginInstance{Name: "fair-plugin", Extension: client}
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); m.taskDispatchLoop(ctx) }()
	m.dispatchPendingTasks(ctx)
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	found := false
	for !found {
		select {
		case id := <-started:
			found = id == int64(last)
		case <-deadline.C:
			cancel()
			<-loopDone
			t.Fatal("free worker waited for disabled plugin or slow task")
		}
	}
	close(releaseSlow)
	if err := m.taskWorkers().wait(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-loopDone
	if n, err := db.Task.Query().Where(enttask.PluginIDEQ("disabled"), enttask.StatusEQ(enttask.StatusPending)).Count(t.Context()); err != nil || n != 12 {
		t.Fatal("disabled tasks were changed")
	}
}

func TestTaskAdmissionRejectsExcessQueueButPreservesIdempotency(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "task_admission", schema.WithGlobalUniqueID(false))
	defer db.Close()
	ctx := t.Context()
	host := &HostService{db: db}
	first, err := host.createTask(ctx, "plugin", hostCreateTaskRequest{UserID: 1, TaskType: "image", IdempotencyKey: "same"})
	if err != nil {
		t.Fatal(err)
	}
	builders := make([]*ent.TaskCreate, 0, maxUserQueuedTasks-1)
	for range maxUserQueuedTasks - 1 {
		builders = append(builders, db.Task.Create().SetPluginID("plugin").SetTaskType("image").SetUserID(1))
	}
	db.Task.CreateBulk(builders...).SaveX(ctx)
	if _, err := host.createTask(ctx, "plugin", hostCreateTaskRequest{UserID: 1, TaskType: "image"}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("full queue accepted task: %v", err)
	}
	again, err := host.createTask(ctx, "plugin", hostCreateTaskRequest{UserID: 1, TaskType: "image", IdempotencyKey: "same"})
	if err != nil {
		t.Fatal(err)
	}
	if first["task"].(map[string]interface{})["id"] != again["task"].(map[string]interface{})["id"] {
		t.Fatal("idempotent request created duplicate")
	}
}
