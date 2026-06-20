package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetStorageParseAndNormalizeHelpers(t *testing.T) {
	t.Parallel()

	for _, purpose := range []AssetPurpose{
		AssetPurposeChat,
		AssetPurposeUpload,
		AssetPurposeGenerated,
		AssetPurposeTaskInput,
		AssetPurposeTemp,
	} {
		got, ok := parseAssetPurpose(string(purpose))
		if !ok || got != purpose {
			t.Fatalf("parseAssetPurpose(%q) = %q/%v", purpose, got, ok)
		}
	}
	if got, ok := parseAssetPurpose("other"); ok || got != "" {
		t.Fatalf("parseAssetPurpose(other) = %q/%v, want empty false", got, ok)
	}

	endpoint, useSSL := normalizeAssetEndpoint("https://s3.example.test:9000/")
	if endpoint != "s3.example.test:9000" || useSSL == nil || !*useSSL {
		t.Fatalf("normalize https = %q/%v", endpoint, useSSL)
	}
	endpoint, useSSL = normalizeAssetEndpoint("http://s3.example.test")
	if endpoint != "s3.example.test" || useSSL == nil || *useSSL {
		t.Fatalf("normalize http = %q/%v", endpoint, useSSL)
	}
	endpoint, useSSL = normalizeAssetEndpoint("s3.example.test/")
	if endpoint != "s3.example.test" || useSSL != nil {
		t.Fatalf("normalize bare = %q/%v", endpoint, useSSL)
	}
	if cleanAssetPrefix(" /prefix/ ") != "prefix" || cleanAssetPrefix(".") != "" {
		t.Fatalf("cleanAssetPrefix edge cases failed")
	}
	if cleanAssetExtension("png") != ".png" || cleanAssetExtension(".JPG") != ".jpg" || cleanAssetExtension("bad/ext") != ".bin" || cleanAssetExtension("") != ".bin" {
		t.Fatalf("cleanAssetExtension edge cases failed")
	}
	for _, truthy := range []string{"1", "t", "true", "yes", "on", " TRUE "} {
		if !parseBool(truthy) {
			t.Fatalf("parseBool(%q) = false", truthy)
		}
	}
	if parseBool("false") || parseBool("") {
		t.Fatalf("parseBool false cases failed")
	}
}

func TestAssetStorageContentTypeHelpers(t *testing.T) {
	t.Parallel()

	exts := map[string]string{
		"image/jpeg":    ".jpg",
		"image/png":     ".png",
		"image/webp":    ".webp",
		"image/gif":     ".gif",
		"image/svg+xml": ".svg",
		"video/mp4":     ".mp4",
		"audio/mpeg":    ".mp3",
		"text/plain":    ".bin",
	}
	for ct, want := range exts {
		if got := extensionForContentType(ct); got != want {
			t.Fatalf("extensionForContentType(%q) = %q, want %q", ct, got, want)
		}
	}
	types := map[string]string{
		"a.jpg":  "image/jpeg",
		"a.jpeg": "image/jpeg",
		"a.png":  "image/png",
		"a.webp": "image/webp",
		"a.gif":  "image/gif",
		"a.bin":  "application/octet-stream",
	}
	for key, want := range types {
		if got := contentTypeForAssetKey(key); got != want {
			t.Fatalf("contentTypeForAssetKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestAssetStorageLocalErrorBranches(t *testing.T) {
	t.Parallel()

	storage := newTestAssetStorage(t)
	if _, err := storage.localPath(""); err == nil {
		t.Fatal("localPath empty key error = nil, want error")
	}
	if exists, err := storage.localObjectExists("missing.png"); err != nil || exists {
		t.Fatalf("localObjectExists(missing) = %v/%v, want false nil", exists, err)
	}
	if _, _, err := storage.getLocalBytes("missing.png"); err == nil {
		t.Fatal("getLocalBytes(missing) error = nil, want error")
	}
	if err := storage.storeLocalBytes("", []byte("x")); err == nil {
		t.Fatal("storeLocalBytes(empty key) error = nil, want error")
	}
	outside := filepath.Join(t.TempDir(), "asset.png")
	if _, err := storage.localObjectKey(outside); err == nil {
		t.Fatal("localObjectKey(outside) error = nil, want error")
	}
}

func TestAssetStorageStoreFromURLLocal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png; charset=utf-8")
		_, _ = w.Write([]byte("png-data"))
	}))
	t.Cleanup(server.Close)

	storage := newTestAssetStorage(t)
	asset, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, server.URL)
	if err != nil {
		t.Fatalf("StoreFromURL() error = %v", err)
	}
	if asset.ContentType != "image/png" || !strings.HasSuffix(asset.ObjectKey, ".png") || asset.SizeBytes != int64(len("png-data")) {
		t.Fatalf("asset = %+v", asset)
	}
	data, contentType, err := storage.GetBytes(context.Background(), asset.ObjectKey)
	if err != nil {
		t.Fatalf("GetBytes() error = %v", err)
	}
	if string(data) != "png-data" || contentType != "image/png" {
		t.Fatalf("stored data = %q/%q", data, contentType)
	}
}

func TestAssetStorageStoreFromURLErrors(t *testing.T) {
	t.Parallel()

	storage := newTestAssetStorage(t)
	if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, "file:///tmp/a.png"); err == nil {
		t.Fatal("StoreFromURL(file) error = nil, want error")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, server.URL); err == nil {
		t.Fatal("StoreFromURL(non-200) error = nil, want error")
	}
}
