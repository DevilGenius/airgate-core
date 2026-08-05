package handler

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReadCompatibleImportRequest(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range map[string]string{
		"platform": " OpenAI ",
		"format":   " Codex ",
		"dry_run":  "true",
	} {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("WriteField(%s): %v", key, err)
		}
	}
	part, err := writer.CreateFormFile("files", `folder\account.auth.json`)
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err := part.Write([]byte(`{"access_token":"token"}`)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/import/compat", body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	req, err := readCompatibleImportRequest(c)
	if err != nil {
		t.Fatalf("readCompatibleImportRequest() error = %v", err)
	}
	if req.Platform != "openai" || req.Format != "codex" || !req.DryRun {
		t.Fatalf("request fields = %+v", req)
	}
	if len(req.Files) != 1 || req.Files[0].Name != "account.auth.json" || req.TotalBytes == 0 {
		t.Fatalf("request files = %+v", req.Files)
	}
}

func TestReadCompatibleImportRequestRejectsNonMultipart(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/import/compat", bytes.NewBufferString(`{}`))
	_, err := readCompatibleImportRequest(c)
	var requestErr *compatibleImportRequestError
	if err == nil || !errors.As(err, &requestErr) || requestErr.Status != http.StatusBadRequest {
		t.Fatalf("error = %#v, want bad request", err)
	}
}
