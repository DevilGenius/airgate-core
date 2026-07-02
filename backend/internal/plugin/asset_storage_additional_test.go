package plugin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	allowPrivateAssetDownloads(t)

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
	storage := newTestAssetStorage(t)
	if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, "file:///tmp/a.png"); err == nil {
		t.Fatal("StoreFromURL(file) error = nil, want error")
	}
	if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, "http://127.0.0.1/internal.png"); err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("StoreFromURL(loopback) error = %v", err)
	}

	allowPrivateAssetDownloads(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, server.URL); err == nil {
		t.Fatal("StoreFromURL(non-200) error = nil, want error")
	}
}

func TestAssetDownloadDialRejectsPrivateResolvedAddress(t *testing.T) {
	prevLookup := assetSourceLookupIPAddr
	prevAllowPrivate := allowPrivateAssetDownloadsForTesting.Load()
	assetSourceLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	allowPrivateAssetDownloadsForTesting.Store(false)
	t.Cleanup(func() {
		assetSourceLookupIPAddr = prevLookup
		allowPrivateAssetDownloadsForTesting.Store(prevAllowPrivate)
	})

	_, err := dialValidatedAssetSource(context.Background(), "tcp", "assets.example:80")
	if err == nil || !strings.Contains(err.Error(), "private or local") {
		t.Fatalf("dialValidatedAssetSource private resolver error = %v", err)
	}
}

func TestAssetDownloadHTTPClientReusesTransport(t *testing.T) {
	first := assetDownloadHTTPClient()
	second := assetDownloadHTTPClient()
	if first.Transport == nil || first.Transport != second.Transport {
		t.Fatal("asset download clients should share one transport")
	}
}

func TestAssetStorageStoreFromURLUsesDialValidationOncePerConnection(t *testing.T) {
	allowPrivateAssetDownloads(t)

	var dials atomic.Int32
	var conns sync.Map
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connID := r.RemoteAddr
		conns.Store(connID, struct{}{})
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			dials.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	storage := newTestAssetStorage(t)
	for i := 0; i < 2; i++ {
		if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, server.URL); err != nil {
			t.Fatalf("StoreFromURL(%d) error = %v", i, err)
		}
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("server connections = %d, want 1", got)
	}
	count := 0
	conns.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("remote connections = %d, want 1", count)
	}
}

func TestAssetStorageStoreFromURLDoesNotResolveBeforeDial(t *testing.T) {
	allowPrivateAssetDownloads(t)

	prevLookup := assetSourceLookupIPAddr
	var lookups atomic.Int32
	assetSourceLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		lookups.Add(1)
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	t.Cleanup(func() { assetSourceLookupIPAddr = prevLookup })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	t.Cleanup(server.Close)

	sourceURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	storage := newTestAssetStorage(t)
	if _, err := storage.StoreFromURL(context.Background(), 42, AssetPurposeUpload, sourceURL); err != nil {
		t.Fatalf("StoreFromURL() error = %v", err)
	}
	if got := lookups.Load(); got != 1 {
		t.Fatalf("download DNS lookups = %d, want 1", got)
	}
}

func allowPrivateAssetDownloads(t *testing.T) {
	t.Helper()
	prev := allowPrivateAssetDownloadsForTesting.Load()
	allowPrivateAssetDownloadsForTesting.Store(true)
	t.Cleanup(func() { allowPrivateAssetDownloadsForTesting.Store(prev) })
}
