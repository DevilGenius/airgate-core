package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestIndexHTMLReadsEmbeddedFile(t *testing.T) {
	data, err := IndexHTML()
	if err != nil {
		t.Fatalf("读取 index.html 失败: %v", err)
	}
	if !strings.Contains(string(data), "<!doctype html>") && !strings.Contains(string(data), "<!DOCTYPE html>") {
		t.Fatalf("index.html 内容不像 HTML: %q", string(data[:min(len(data), 32)]))
	}
}

func TestFSContainsIndexHTML(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Fatalf("获取嵌入文件系统失败: %v", err)
	}
	info, err := fs.Stat(sub, "index.html")
	if err != nil {
		t.Fatalf("嵌入文件系统缺少 index.html: %v", err)
	}
	if info.IsDir() {
		t.Fatal("index.html 不应是目录")
	}
}
