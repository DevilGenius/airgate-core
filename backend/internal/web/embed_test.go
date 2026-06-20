package web

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

func TestIndexHTMLReadsEmbeddedFile(t *testing.T) {
	data, err := IndexHTML()
	if err != nil {
		t.Skipf("跳过: 嵌入资源不可用 (需先构建前端): %v", err)
	}
	if !strings.Contains(string(data), "<!doctype html>") && !strings.Contains(string(data), "<!DOCTYPE html>") {
		t.Fatalf("index.html 内容不像 HTML: %q", string(data[:min(len(data), 32)]))
	}
}

func TestFSContainsIndexHTML(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Skipf("跳过: 嵌入资源不可用 (需先构建前端): %v", err)
	}
	info, err := fs.Stat(sub, "index.html")
	if err != nil {
		t.Fatalf("嵌入文件系统缺少 index.html: %v", err)
	}
	if info.IsDir() {
		t.Fatal("index.html 不应是目录")
	}
}

func TestSubFSWithIndex(t *testing.T) {
	sub, err := subFSWithIndex(fstest.MapFS{
		"webdist/index.html": {Data: []byte("<!doctype html>")},
	}, "webdist")
	if err != nil {
		t.Fatalf("subFSWithIndex success returned error: %v", err)
	}
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if string(data) != "<!doctype html>" {
		t.Fatalf("index.html = %q", data)
	}

	if _, err := subFSWithIndex(fstest.MapFS{}, "webdist"); err == nil || !strings.Contains(err.Error(), "embedded web/dist is empty") {
		t.Fatalf("missing index error = %v", err)
	}
	if _, err := subFSWithIndex(fstest.MapFS{}, "../webdist"); err == nil {
		t.Fatal("invalid sub dir returned nil error")
	}
}

func TestSetIndexHTMLCacheHeaders(t *testing.T) {
	header := http.Header{}

	SetIndexHTMLCacheHeaders(header)

	if got := header.Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := header.Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q", got)
	}
	if got := header.Get("Expires"); got != "0" {
		t.Fatalf("Expires = %q", got)
	}
}
