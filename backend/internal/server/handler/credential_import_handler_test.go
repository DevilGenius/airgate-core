package handler

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent/account"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appcredentialimport "github.com/DevilGenius/airgate-core/internal/app/credentialimport"
	apppluginadmin "github.com/DevilGenius/airgate-core/internal/app/pluginadmin"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
)

type namingImportParser struct{}

func (namingImportParser) ResolveGatewayCapability(string, string) (apppluginadmin.CapabilityTarget, error) {
	return apppluginadmin.CapabilityTarget{PluginName: "openai", Metadata: `{"formats":["account_json"]}`}, nil
}

func (namingImportParser) Proxy(context.Context, apppluginadmin.ProxyInput) (apppluginadmin.ProxyResult, error) {
	return apppluginadmin.ProxyResult{StatusCode: http.StatusOK, Body: []byte(`{"accounts":[{"name":"Original File Name","type":"oauth","credentials":{"access_token":"complete","plan_type":"plus"},"priority":50,"max_concurrency":10,"rate_multiplier":1}]}`)}, nil
}

func TestCompatibleAndCredentialAPIShareAutomaticNaming(t *testing.T) {
	db := testdb.OpenMemoryEnt(t, "compatible_naming")
	defer db.Close()
	svc := appaccount.NewService(store.NewAccountStore(db), nil, scheduler.NewConcurrencyManager(nil), nil)
	h := NewCredentialImportHandler(NewAccountHandler(svc, nil), appcredentialimport.NewService(namingImportParser{}))
	for _, tc := range []struct {
		path      string
		dry       bool
		wantCount int
	}{
		{"/api/v1/admin/accounts/import/compat", true, 0},
		{"/api/v1/admin/accounts/import/compat", false, 1},
		{"/api/v1/credentials/accounts/import/compat", false, 2},
	} {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		for key, value := range map[string]string{"platform": "openai", "format": "account_json", "dry_run": strconv.FormatBool(tc.dry)} {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		part, err := writer.CreateFormFile("files", "account.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(`{"access_token":"complete"}`)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, tc.path, body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		h.ImportCompatibleAccounts(c)
		items, err := db.Account.Query().Order(account.ByID()).All(t.Context())
		if recorder.Code != http.StatusOK || err != nil || len(items) != tc.wantCount {
			t.Fatalf("path=%s dry=%v count=%d err=%v body=%s", tc.path, tc.dry, len(items), err, recorder.Body.String())
		}
		for index, item := range items {
			want := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("0102") + "-Plus-" + strconv.Itoa(index+1)
			if item.Name != want {
				t.Fatalf("got %q, want %q", item.Name, want)
			}
		}
	}
}

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

func TestCredentialImportBanAccountUpdatesOnlyUndeletedAccount(t *testing.T) {
	ctx := t.Context()
	db := testdb.OpenMemoryEnt(t, "credential_account_ban", schema.WithGlobalUniqueID(false))
	defer func() { _ = db.Close() }()

	item, err := db.Account.Create().
		SetName("ban-me").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "secret"}).
		SetState(account.StateRateLimited).
		SetStateUntil(time.Now().Add(time.Hour)).
		SetErrorMsg("old reason").
		Save(ctx)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	deleted, err := db.Account.Create().
		SetName("already-deleted").
		SetPlatform("openai").
		SetType("oauth").
		SetCredentials(map[string]string{"access_token": "deleted"}).
		SetState(account.StateDisabled).
		SetErrorMsg("keep me").
		SetDeletedAt(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create deleted account: %v", err)
	}

	accountScheduler := scheduler.NewScheduler(db, nil)
	accountService := appaccount.NewService(store.NewAccountStore(db), nil, scheduler.NewConcurrencyManager(nil), accountScheduler)
	accountHandler := NewAccountHandler(accountService, accountScheduler)
	handler := NewCredentialImportHandler(accountHandler, nil)

	invalid := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/ban",
		`{"id":1,"name":"must-reject"}`,
		nil,
		nil,
		handler.BanAccount,
	)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown ban field status = %d, body=%s", invalid.Code, invalid.Body.String())
	}

	valid := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/ban",
		`{"id":`+strconv.Itoa(item.ID)+`}`,
		nil,
		nil,
		handler.BanAccount,
	)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid ban status = %d, body=%s", valid.Code, valid.Body.String())
	}
	banned, err := db.Account.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get banned account: %v", err)
	}
	if banned.State != account.StateDisabled || banned.StateUntil != nil || banned.ErrorMsg != "Banned" || banned.DeletedAt != nil {
		t.Fatalf("banned account = state=%s until=%v reason=%q deleted_at=%v", banned.State, banned.StateUntil, banned.ErrorMsg, banned.DeletedAt)
	}

	deletedResponse := invokeHandlerForValidation(
		http.MethodPost,
		"/credentials/accounts/ban",
		`{"id":`+strconv.Itoa(deleted.ID)+`}`,
		nil,
		nil,
		handler.BanAccount,
	)
	if deletedResponse.Code != http.StatusNotFound {
		t.Fatalf("deleted account ban status = %d, body=%s", deletedResponse.Code, deletedResponse.Body.String())
	}
	unchanged, err := db.Account.Get(ctx, deleted.ID)
	if err != nil {
		t.Fatalf("get deleted account: %v", err)
	}
	if unchanged.State != account.StateDisabled || unchanged.ErrorMsg != "keep me" || unchanged.DeletedAt == nil {
		t.Fatalf("deleted account changed = state=%s reason=%q deleted_at=%v", unchanged.State, unchanged.ErrorMsg, unchanged.DeletedAt)
	}
}
