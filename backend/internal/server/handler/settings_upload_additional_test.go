package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsUploadFileBranches(t *testing.T) {
	t.Chdir(t.TempDir())
	handler := NewSettingsHandler(nil, "secret", nil)

	body, contentType := multipartBodyWithFile(t, "file", "logo.png", []byte("png"))
	w := invokeMultipartPluginHandler(http.MethodPost, "/settings/upload", body, contentType, nil, handler.UploadFile)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"/uploads/`) {
		t.Fatalf("upload body = %s", w.Body.String())
	}
	matches, err := filepath.Glob(filepath.Join("data", "uploads", "*.png"))
	if err != nil {
		t.Fatalf("glob upload: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("uploaded files = %v, want one png", matches)
	}

	body, contentType = multipartBodyWithFile(t, "file", "notes.txt", []byte("not an image"))
	w = invokeMultipartPluginHandler(http.MethodPost, "/settings/upload", body, contentType, nil, handler.UploadFile)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid extension status = %d body=%s", w.Code, w.Body.String())
	}

	body, contentType = multipartBodyWithFile(t, "file", "huge.png", bytes.Repeat([]byte("x"), (2<<20)+1))
	w = invokeMultipartPluginHandler(http.MethodPost, "/settings/upload", body, contentType, nil, handler.UploadFile)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("large upload status = %d body=%s", w.Code, w.Body.String())
	}
}

func multipartBodyWithFile(t *testing.T, fieldName, fileName string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}
