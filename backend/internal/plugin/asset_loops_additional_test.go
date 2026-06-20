package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	enttask "github.com/DevilGenius/airgate-core/ent/task"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestAssetCleanupAndMigrationLoopsReturnOnNilOrCanceled(t *testing.T) {
	StartAssetCleanupLoop(context.Background(), nil)
	StartAssetMigrationLoop(context.Background(), nil)

	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "asset_loop_canceled", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	StartAssetCleanupLoop(canceled, db)
	StartAssetMigrationLoop(canceled, db)
}

func TestRunAssetCleanupOnceDisabledPolicy(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "asset_cleanup_disabled", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue(t.TempDir()).SaveX(ctx)
	db.Setting.Create().SetGroup("storage").SetKey(settingAssetRetentionGeneratedDays).SetValue("0").SaveX(ctx)

	runAssetCleanupOnce(ctx, db)
}

func TestRunAssetCleanupOnceCleansLocalAssetsAndOpenEndedTasks(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "asset_cleanup_once", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	localDir := t.TempDir()
	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue(localDir).SaveX(ctx)
	db.Setting.Create().SetGroup("storage").SetKey(settingAssetRetentionGeneratedDays).SetValue("1").SaveX(ctx)

	storage, err := NewAssetStorage(ctx, db)
	if err != nil {
		t.Fatalf("NewAssetStorage() error = %v", err)
	}
	oldAsset := mustStoreTestAsset(t, storage, ctx, 42, AssetPurposeGenerated)
	oldAt := time.Now().Add(-48 * time.Hour)
	mustSetAssetMTime(t, storage, oldAsset.ObjectKey, oldAt)
	expired := db.Task.Create().
		SetPluginID(generatedTaskExecutorPluginID).
		SetTaskType("image.edit").
		SetUserID(42).
		SetStatus(enttask.StatusFailed).
		SetCreatedAt(oldAt).
		SetOutput(map[string]interface{}{
			"asset_object_keys": []interface{}{oldAsset.ObjectKey},
		}).
		SaveX(ctx)
	retained := db.Task.Create().
		SetPluginID(generatedTaskExecutorPluginID).
		SetTaskType("image.edit").
		SetUserID(42).
		SetStatus(enttask.StatusFailed).
		SetCreatedAt(time.Now()).
		SaveX(ctx)

	runAssetCleanupOnce(ctx, db)

	assertTaskMissing(t, db, ctx, expired.ID)
	assertTaskExists(t, db, ctx, retained.ID)
	assertAssetMissing(t, storage, oldAsset.ObjectKey)
}

func TestRunAssetMigrationOnceSkipsWithoutS3(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "asset_migration_no_s3", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue(t.TempDir()).SaveX(ctx)

	runAssetMigrationOnce(ctx, db)
}

func TestAssetStorageSyncLocalToS3Edges(t *testing.T) {
	ctx := context.Background()

	var nilStorage *AssetStorage
	if got, err := nilStorage.SyncLocalToS3(ctx); err != nil || got != (AssetMigrationResult{}) {
		t.Fatalf("nil SyncLocalToS3() = %+v, %v", got, err)
	}
	localOnly := &AssetStorage{localDir: t.TempDir()}
	if got, err := localOnly.SyncLocalToS3(ctx); err != nil || got != (AssetMigrationResult{}) {
		t.Fatalf("local-only SyncLocalToS3() = %+v, %v", got, err)
	}
	missingDir := &AssetStorage{useS3: true, localDir: filepath.Join(t.TempDir(), "missing")}
	if got, err := missingDir.SyncLocalToS3(ctx); err != nil || got != (AssetMigrationResult{}) {
		t.Fatalf("missing-dir SyncLocalToS3() = %+v, %v", got, err)
	}
	fileDir := filepath.Join(t.TempDir(), "asset-file")
	if err := os.WriteFile(fileDir, []byte("not a dir"), 0644); err != nil {
		t.Fatalf("write file dir: %v", err)
	}
	notDir := &AssetStorage{useS3: true, localDir: fileDir}
	if got, err := notDir.SyncLocalToS3(ctx); err != nil || got != (AssetMigrationResult{}) {
		t.Fatalf("not-dir SyncLocalToS3() = %+v, %v", got, err)
	}

	canceledDir := t.TempDir()
	mustWriteFile(t, filepath.Join(canceledDir, "generated", "1", "old.png"), []byte("x"))
	canceledStorage := &AssetStorage{useS3: true, localDir: canceledDir}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := canceledStorage.SyncLocalToS3(canceled); err == nil {
		t.Fatal("canceled SyncLocalToS3() error = nil")
	}
}
