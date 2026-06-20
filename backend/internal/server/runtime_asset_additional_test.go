package server

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/testdb"
)

func TestGenerateThumbnailFromBytesAdditionalBranches(t *testing.T) {
	dir := t.TempDir()

	if _, err := generateThumbnail(filepath.Join(dir, "missing.png"), filepath.Join(dir, "missing.jpg"), 256); err == nil {
		t.Fatal("generateThumbnail missing source returned nil error")
	}
	if _, err := generateThumbnailFromBytes([]byte("not an image"), filepath.Join(dir, "invalid.jpg"), 256); err == nil {
		t.Fatal("generateThumbnailFromBytes invalid image returned nil error")
	}

	wideSrc := filepath.Join(dir, "wide.png")
	writePNG(t, wideSrc, 1024, 1)
	wideData, err := os.ReadFile(wideSrc)
	if err != nil {
		t.Fatalf("read wide png: %v", err)
	}
	wideThumb := filepath.Join(dir, "cache", "wide.w512.jpg")
	thumb, err := generateThumbnailFromBytes(wideData, wideThumb, 512)
	if err != nil {
		t.Fatalf("generate wide thumbnail: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("decode wide thumbnail: %v", err)
	}
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 1 {
		t.Fatalf("wide thumbnail bounds = %v", img.Bounds())
	}
	if _, err := os.Stat(wideThumb); err != nil {
		t.Fatalf("wide thumbnail cache not written: %v", err)
	}

	smallSrc := filepath.Join(dir, "small.png")
	writePNG(t, smallSrc, 16, 16)
	smallData, err := os.ReadFile(smallSrc)
	if err != nil {
		t.Fatalf("read small png: %v", err)
	}
	if _, err := generateThumbnailFromBytes(smallData, filepath.Join(dir, "small.jpg"), 256); !errors.Is(err, errSkipThumb) {
		t.Fatalf("small thumbnail error = %v", err)
	}

	parentFile := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if _, err := generateThumbnailFromBytes(wideData, filepath.Join(parentFile, "thumb.jpg"), 512); err == nil {
		t.Fatal("thumbnail with file parent returned nil error")
	}

	writeFail := filepath.Join(dir, "write-fail.jpg")
	if err := os.Mkdir(writeFail+".tmp", 0o755); err != nil {
		t.Fatalf("mkdir tmp collision: %v", err)
	}
	if _, err := generateThumbnailFromBytes(wideData, writeFail, 512); err == nil {
		t.Fatal("thumbnail with tmp directory collision returned nil error")
	}

	renameTarget := filepath.Join(dir, "rename-target")
	if err := os.Mkdir(renameTarget, 0o755); err != nil {
		t.Fatalf("mkdir rename target: %v", err)
	}
	if _, err := generateThumbnailFromBytes(wideData, renameTarget, 512); err == nil {
		t.Fatal("thumbnail rename over directory returned nil error")
	}
	if _, err := os.Stat(renameTarget + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("rename failure left tmp file, stat err=%v", err)
	}
}

func TestHandleRuntimeAssetLocalStorageBranches(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "server_runtime_assets", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	assetDir := t.TempDir()
	if _, err := db.Setting.Create().
		SetGroup("storage").
		SetKey("local_storage_dir").
		SetValue(assetDir).
		Save(context.Background()); err != nil {
		t.Fatalf("create storage setting: %v", err)
	}

	writeAsset := func(rel string, data []byte) {
		t.Helper()
		full := filepath.Join(assetDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir asset: %v", err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatalf("write asset: %v", err)
		}
	}

	server := &Server{db: db}
	writeAsset("chat/1/note.json", []byte(`{"ok":true}`))
	c, w := newServerTestContext(http.MethodGet, "/assets-runtime/chat/1/note.json", gin.Params{{Key: "path", Value: "/chat/1/note.json"}})
	server.handleRuntimeAsset(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "application/json") || w.Body.String() != `{"ok":true}` {
		t.Fatalf("json asset response = %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if cacheControl := w.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "immutable") {
		t.Fatalf("cache control = %q", cacheControl)
	}

	c, w = newServerTestContext(http.MethodGet, "/assets-runtime/chat/1/missing.png", gin.Params{{Key: "path", Value: "/chat/1/missing.png"}})
	server.handleRuntimeAsset(c)
	if status := c.Writer.Status(); status != http.StatusNotFound {
		t.Fatalf("missing runtime asset status = %d recorder=%d", status, w.Code)
	}

	bigPath := filepath.Join(assetDir, "generated", "1", "big.png")
	if err := os.MkdirAll(filepath.Dir(bigPath), 0o755); err != nil {
		t.Fatalf("mkdir big asset: %v", err)
	}
	writePNG(t, bigPath, 640, 320)
	c, w = newServerTestContext(http.MethodGet, "/assets-runtime/generated/1/big.png?w=256", gin.Params{{Key: "path", Value: "/generated/1/big.png"}})
	server.handleRuntimeAsset(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "image/jpeg") || w.Body.Len() == 0 {
		t.Fatalf("thumbnail asset response = %d %q len=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}
	cachePath := thumbCachePath(bigPath, 256)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("thumbnail cache missing: %v", err)
	}

	c, w = newServerTestContext(http.MethodGet, "/assets-runtime/generated/1/big.png?w=256", gin.Params{{Key: "path", Value: "/generated/1/big.png"}})
	server.handleRuntimeAsset(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "image/jpeg") || w.Body.Len() == 0 {
		t.Fatalf("cached thumbnail response = %d %q len=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}

	smallPath := filepath.Join(assetDir, "generated", "1", "small.png")
	writePNG(t, smallPath, 32, 32)
	c, w = newServerTestContext(http.MethodGet, "/assets-runtime/generated/1/small.png?w=256", gin.Params{{Key: "path", Value: "/generated/1/small.png"}})
	server.handleRuntimeAsset(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "image/png") || w.Body.Len() == 0 {
		t.Fatalf("small image fallback response = %d %q len=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}
}
