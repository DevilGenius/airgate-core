package handler

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent/account"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
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

func TestCredentialImportDeleteAccountAcceptsOnlyPrimaryID(t *testing.T) {
	ctx := t.Context()
	db := testdb.OpenMemoryEnt(t, "credential_account_delete", schema.WithGlobalUniqueID(false))
	defer func() { _ = db.Close() }()

	item, err := db.Account.Create().
		SetName("delete-me").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "secret"}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	accountService := appaccount.NewService(store.NewAccountStore(db), nil, scheduler.NewConcurrencyManager(nil), nil)
	accountHandler := NewAccountHandler(accountService, nil)
	handler := NewCredentialImportHandler(accountHandler, nil)

	invalid := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/delete",
		`{"id":1,"name":"must-reject"}`,
		nil,
		nil,
		handler.DeleteAccount,
	)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown delete field status = %d, body=%s", invalid.Code, invalid.Body.String())
	}
	unchanged, err := db.Account.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get unchanged account: %v", err)
	}
	if unchanged.State != account.StateActive || unchanged.DeletedAt != nil {
		t.Fatalf("invalid delete changed account: state=%s deleted_at=%v", unchanged.State, unchanged.DeletedAt)
	}

	valid := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/delete",
		`{"id":`+strconv.Itoa(item.ID)+`}`,
		nil,
		nil,
		handler.DeleteAccount,
	)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid delete status = %d, body=%s", valid.Code, valid.Body.String())
	}
	deleted, err := db.Account.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get deleted account: %v", err)
	}
	if deleted.State != account.StateDisabled || deleted.DeletedAt == nil {
		t.Fatalf("valid delete did not soft-delete account: state=%s deleted_at=%v", deleted.State, deleted.DeletedAt)
	}
}
