package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

func TestAssetStorageStoreS3SuccessFallbackAndInvalidPurpose(t *testing.T) {
	ctx := context.Background()
	storage, fake := newMigratingTestAssetStorage(t, "assets")
	storage.publicBaseURL = ""

	asset, err := storage.Store(ctx, 77, AssetPurposeUpload, "image/png", "png", []byte("remote-bytes"))
	if err != nil {
		t.Fatalf("Store(s3) error = %v", err)
	}
	if !fake.has(asset.ObjectKey) || !strings.HasSuffix(asset.ObjectKey, ".png") || asset.SizeBytes != int64(len("remote-bytes")) {
		t.Fatalf("stored asset=%+v remoteExists=%v", asset, fake.has(asset.ObjectKey))
	}
	if !strings.Contains(asset.PublicURL, "/assets/") || !strings.Contains(asset.PublicURL, "?") {
		t.Fatalf("expected presigned public URL, got %q", asset.PublicURL)
	}

	data, contentType, err := storage.getS3Bytes(ctx, asset.ObjectKey)
	if err != nil {
		t.Fatalf("getS3Bytes() error = %v", err)
	}
	if !strings.Contains(string(data), "remote-bytes") || contentType != "image/png" {
		t.Fatalf("s3 bytes = %q/%q", data, contentType)
	}

	fallback := &AssetStorage{
		bucket:     "assets",
		localDir:   t.TempDir(),
		presignTTL: time.Hour,
		useS3:      true,
	}
	localAsset, err := fallback.Store(ctx, 78, AssetPurposeUpload, "image/png", "png", []byte("local-fallback"))
	if err != nil {
		t.Fatalf("Store(s3 fallback local) error = %v", err)
	}
	if !strings.HasPrefix(localAsset.PublicURL, "/assets-runtime/") {
		t.Fatalf("fallback public URL = %q", localAsset.PublicURL)
	}
	if got, _, err := fallback.GetBytes(ctx, localAsset.ObjectKey); err != nil || string(got) != "local-fallback" {
		t.Fatalf("fallback bytes = %q err=%v", got, err)
	}

	if _, err := fallback.Store(ctx, 1, AssetPurpose("bad"), "text/plain", "txt", []byte("x")); err == nil {
		t.Fatal("Store(invalid purpose) error = nil")
	}
}

func TestAssetStorageS3HelpersWithoutConfiguredClient(t *testing.T) {
	ctx := context.Background()
	storage := newTestAssetStorage(t)
	if err := storage.putS3Bytes(ctx, "a.png", "image/png", []byte("x")); err == nil {
		t.Fatal("putS3Bytes without S3 error = nil")
	}
	if err := storage.putS3File(ctx, "a.png", "image/png", "missing.png"); err == nil {
		t.Fatal("putS3File without S3 error = nil")
	}
	if _, _, err := storage.getS3Bytes(ctx, "a.png"); err == nil {
		t.Fatal("getS3Bytes without S3 error = nil")
	}
	if exists, err := storage.objectExistsOnS3(ctx, "a.png"); err != nil || exists {
		t.Fatalf("objectExistsOnS3 without S3 = %v/%v", exists, err)
	}
	if got, err := storage.remotePublicURL(ctx, "a b.png"); err != nil || got != "/assets-runtime/a%20b.png" {
		t.Fatalf("remotePublicURL local = %q/%v", got, err)
	}
	storage.repairS3FromLocal("a.png", []byte("x"), "image/png")
	storage.repairS3FromLocal("", []byte("x"), "image/png")
}

func TestAssetStorageCleanupLocalAndS3Purposes(t *testing.T) {
	ctx := context.Background()
	local := &AssetStorage{localDir: t.TempDir(), prefix: "pfx"}
	oldKey := path.Join("pfx", string(AssetPurposeTemp), "old.png")
	oldPath, err := local.LocalPath(oldKey)
	if err != nil {
		t.Fatalf("old local path: %v", err)
	}
	mustWriteFile(t, oldPath, []byte("old"))
	mustWriteFile(t, oldPath+".w256.jpg", []byte("thumb"))
	oldTime := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old asset: %v", err)
	}
	if err := os.Chtimes(oldPath+".w256.jpg", oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old thumb: %v", err)
	}
	youngKey := path.Join("pfx", string(AssetPurposeTemp), "young.png")
	youngPath, err := local.LocalPath(youngKey)
	if err != nil {
		t.Fatalf("young local path: %v", err)
	}
	mustWriteFile(t, youngPath, []byte("young"))

	deleted, err := local.CleanupExpired(ctx, AssetRetentionPolicy{
		AssetPurposeTemp: time.Hour,
		AssetPurposeChat: 0,
	})
	if err != nil {
		t.Fatalf("CleanupExpired(local) error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("local deleted = %d, want 1", deleted)
	}
	assertPathMissing(t, oldPath)
	assertPathMissing(t, oldPath+".w256.jpg")
	if _, err := os.Stat(youngPath); err != nil {
		t.Fatalf("young asset should remain: %v", err)
	}

	storage, fake := newMigratingTestAssetStorage(t, "assets")
	storage.prefix = "pfx"
	oldRemoteKey := path.Join("pfx", string(AssetPurposeGenerated), "old.png")
	youngRemoteKey := path.Join("pfx", string(AssetPurposeGenerated), "young.png")
	otherRemoteKey := path.Join("pfx", string(AssetPurposeChat), "old.png")
	fake.mu.Lock()
	fake.objects[oldRemoteKey] = fakeS3Object{data: []byte("old"), contentType: "image/png", lastModified: time.Now().Add(-3 * time.Hour)}
	fake.objects[youngRemoteKey] = fakeS3Object{data: []byte("young"), contentType: "image/png", lastModified: time.Now()}
	fake.objects[otherRemoteKey] = fakeS3Object{data: []byte("other"), contentType: "image/png", lastModified: time.Now().Add(-3 * time.Hour)}
	fake.mu.Unlock()
	localMirror, err := storage.LocalPath(oldRemoteKey)
	if err != nil {
		t.Fatalf("local mirror path: %v", err)
	}
	mustWriteFile(t, localMirror, []byte("mirror"))
	mustWriteFile(t, localMirror+".w128.jpg", []byte("thumb"))

	deleted, err = storage.CleanupExpired(ctx, AssetRetentionPolicy{AssetPurposeGenerated: time.Hour})
	if err != nil {
		t.Fatalf("CleanupExpired(s3) error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("s3 deleted = %d, want 1", deleted)
	}
	if fake.has(oldRemoteKey) || !fake.has(youngRemoteKey) || !fake.has(otherRemoteKey) {
		t.Fatalf("remote state old=%v young=%v other=%v", fake.has(oldRemoteKey), fake.has(youngRemoteKey), fake.has(otherRemoteKey))
	}
	assertPathMissing(t, localMirror)
	assertPathMissing(t, localMirror+".w128.jpg")
}

func TestAssetStorageStoreFromURLDefaultsAndTooLarge(t *testing.T) {
	allowPrivateAssetDownloads(t)
	storage := newTestAssetStorage(t)
	server := httptestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain"))
	})

	asset, err := storage.StoreFromURL(context.Background(), 9, AssetPurposeUpload, server)
	if err != nil {
		t.Fatalf("StoreFromURL(default content type) error = %v", err)
	}
	if asset.ContentType != "text/plain" || !strings.HasSuffix(asset.ObjectKey, ".bin") {
		t.Fatalf("asset = %+v", asset)
	}
}

func httptestServer(t *testing.T, handler func(http.ResponseWriter, *http.Request)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(server.Close)
	return server.URL
}
