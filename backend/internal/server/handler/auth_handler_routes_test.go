package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/DouDOU-start/airgate-core/internal/infra/mailer"
)

func TestVerifyCodeRejectsInvalidCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := mailer.NewVerifyCodeStore()
	code := store.Generate("user@example.com")
	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "111111"
	}

	router := gin.New()
	handler := &AuthHandler{codeStore: store}
	router.POST("/verify-code", handler.VerifyCode)

	body := `{"email":"user@example.com","code":"` + wrongCode + `"}`
	req := httptest.NewRequest(http.MethodPost, "/verify-code", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode invalid code status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !store.Check("user@example.com", code) {
		t.Fatal("invalid verification attempt should not consume the valid code")
	}
}

func TestVerifyCodeDoesNotConsumeValidCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	store := mailer.NewVerifyCodeStore()
	code := store.Generate("user@example.com")

	router := gin.New()
	handler := &AuthHandler{codeStore: store}
	router.POST("/verify-code", handler.VerifyCode)

	body := `{"email":"user@example.com","code":"` + code + `"}`
	req := httptest.NewRequest(http.MethodPost, "/verify-code", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("VerifyCode valid code status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !store.Check("user@example.com", code) {
		t.Fatal("first-step verification should not consume the code; register still needs to verify it")
	}
	if !store.Verify("user@example.com", code) {
		t.Fatal("registration should still be able to consume the verified code")
	}
	if store.Check("user@example.com", code) {
		t.Fatal("registration verification should consume the code")
	}
}
