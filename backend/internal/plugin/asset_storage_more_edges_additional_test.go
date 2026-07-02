package plugin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/minio/minio-go/v7"

	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestNewAssetStorageUsesS3SettingsAndEnvLocalDir(t *testing.T) {
	ctx := context.Background()
	fake := newFakeS3Server(t, "assets")
	localDir := t.TempDir()
	t.Setenv("ASSETS_DIR", localDir)

	db := testdb.OpenMemoryEnt(t, "asset_storage_s3_settings", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	settings := map[string]string{
		"s3_path_prefix":         " /prefix/ ",
		"s3_public_base_url":     "https://cdn.example.test/assets/",
		"s3_presign_ttl_minutes": "15",
		"s3_endpoint":            fake.server.URL,
		"s3_bucket":              "assets",
		"s3_access_key":          "access",
		"s3_secret_key":          "secret",
		"s3_use_ssl":             "true",
	}
	for key, value := range settings {
		db.Setting.Create().SetGroup("storage").SetKey(key).SetValue(value).SaveX(ctx)
	}

	storage, err := NewAssetStorage(ctx, db)
	if err != nil {
		t.Fatalf("NewAssetStorage() error = %v", err)
	}
	if !storage.useS3 || storage.client == nil || storage.bucket != "assets" {
		t.Fatalf("storage S3 config not applied: %+v", storage)
	}
	if storage.prefix != "prefix" || storage.publicBaseURL != "https://cdn.example.test/assets" || storage.presignTTL != 15*time.Minute || storage.localDir != localDir {
		t.Fatalf("storage settings = prefix %q public %q ttl %s local %q", storage.prefix, storage.publicBaseURL, storage.presignTTL, storage.localDir)
	}
}

func TestAssetStorageLocalDeleteAndThumbnailHelpers(t *testing.T) {
	ctx := context.Background()
	storage := newTestAssetStorage(t)
	objectKey := path.Join("upload", "7", "asset.png")
	localPath, err := storage.LocalPath(objectKey)
	if err != nil {
		t.Fatalf("local path: %v", err)
	}
	mustWriteFile(t, localPath, []byte("asset"))
	mustWriteFile(t, localPath+".w128.jpg", []byte("thumb"))
	mustWriteFile(t, localPath+".w256.jpg.tmp", []byte("tmp-thumb"))

	if src, ok := sourcePathFromThumbPath(localPath + ".w128.jpg"); !ok || src != localPath {
		t.Fatalf("sourcePathFromThumbPath jpg = %q/%v", src, ok)
	}
	if src, ok := sourcePathFromThumbPath(localPath + ".w256.jpg.tmp"); !ok || src != localPath {
		t.Fatalf("sourcePathFromThumbPath tmp = %q/%v", src, ok)
	}
	if isLocalThumbVariantPath(localPath+".wbad.jpg") || isLocalThumbVariantPath(localPath+".jpg") {
		t.Fatal("invalid thumbnail paths detected as variants")
	}

	if err := storage.Delete(ctx, objectKey); err != nil {
		t.Fatalf("Delete(local) error = %v", err)
	}
	assertPathMissing(t, localPath)
	assertPathMissing(t, localPath+".w128.jpg")
	assertPathMissing(t, localPath+".w256.jpg.tmp")
	if err := storage.Delete(ctx, objectKey); err != nil {
		t.Fatalf("Delete(local missing) error = %v", err)
	}
	var nilStorage *AssetStorage
	if err := nilStorage.Delete(ctx, objectKey); err != nil {
		t.Fatalf("Delete(nil) error = %v", err)
	}
}

func TestAssetStoragePublicURLAndNotFoundHelpers(t *testing.T) {
	ctx := context.Background()
	storage, _ := newMigratingTestAssetStorage(t, "assets")
	missingRemote := path.Join("generated", "7", "missing.png")
	url, err := storage.PublicURL(ctx, missingRemote)
	if err != nil {
		t.Fatalf("PublicURL(remote missing) error = %v", err)
	}
	if url != "https://cdn.example.com/assets/"+missingRemote {
		t.Fatalf("PublicURL(remote missing) = %q", url)
	}

	localOnly := path.Join("generated", "7", "local.png")
	localPath, err := storage.LocalPath(localOnly)
	if err != nil {
		t.Fatalf("local path: %v", err)
	}
	mustWriteFile(t, localPath, []byte("local"))
	url, err = storage.PublicURL(ctx, localOnly)
	if err != nil {
		t.Fatalf("PublicURL(local fallback) error = %v", err)
	}
	if !strings.HasPrefix(url, "/assets-runtime/") {
		t.Fatalf("PublicURL(local fallback) = %q", url)
	}

	if !isS3NotFoundError(minio.ErrorResponse{StatusCode: http.StatusNotFound}) {
		t.Fatal("StatusCode 404 should be treated as S3 not found")
	}
	if !isS3NotFoundError(minio.ErrorResponse{Code: "NoSuchKey"}) {
		t.Fatal("NoSuchKey should be treated as S3 not found")
	}
	if !isS3NotFoundError(minio.ErrorResponse{Code: "XMinioNoSuchKey"}) {
		t.Fatal("XMinioNoSuchKey should be treated as S3 not found")
	}
	if !isS3NotFoundError(os.ErrNotExist) {
		t.Fatal("os.ErrNotExist should be treated as S3 not found")
	}
	if isS3NotFoundError(errors.New("boom")) {
		t.Fatal("generic error should not be treated as S3 not found")
	}
}

func TestAssetStorageCleanupLocalPurposeEdges(t *testing.T) {
	ctx := context.Background()
	storage := &AssetStorage{localDir: t.TempDir(), prefix: "pfx"}
	rootFile := filepath.Join(storage.localDir, filepath.FromSlash(path.Join("pfx", string(AssetPurposeTemp))))
	mustWriteFile(t, rootFile, []byte("not-dir"))
	deleted, err := storage.cleanupLocalPurpose(ctx, AssetPurposeTemp, time.Hour)
	if err != nil || deleted != 0 {
		t.Fatalf("cleanupLocalPurpose(root file) = %d/%v", deleted, err)
	}

	storage = &AssetStorage{localDir: t.TempDir(), prefix: "pfx"}
	oldPath := filepath.Join(storage.localDir, filepath.FromSlash(path.Join("pfx", string(AssetPurposeTemp), "old.png")))
	mustWriteFile(t, oldPath, []byte("old"))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := storage.cleanupLocalPurpose(canceled, AssetPurposeTemp, time.Hour); err == nil {
		t.Fatal("cleanupLocalPurpose(canceled) error = nil")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestAssetStorageStoreFromURLTooLarge(t *testing.T) {
	allowPrivateAssetDownloads(t)
	storage := newTestAssetStorage(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.CopyN(w, zeroReader{}, maxAssetDownloadSize+1)
	}))
	t.Cleanup(server.Close)

	if _, err := storage.StoreFromURL(context.Background(), 1, AssetPurposeUpload, server.URL); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("StoreFromURL(too large) error = %v", err)
	}
}
