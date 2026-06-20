package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	appgroup "github.com/DevilGenius/airgate-core/internal/app/group"
	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	appproxy "github.com/DevilGenius/airgate-core/internal/app/proxy"
	appsubscription "github.com/DevilGenius/airgate-core/internal/app/subscription"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
)

func newHandlerTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	return c, w
}

func TestHandlerConstructors(t *testing.T) {
	if got := NewAccountHandler(nil, nil); got == nil {
		t.Fatal("NewAccountHandler returned nil")
	}
	if got := NewAPIKeyHandler(nil, nil); got == nil {
		t.Fatal("NewAPIKeyHandler returned nil")
	}
	if got := NewGroupHandler(nil, nil); got == nil {
		t.Fatal("NewGroupHandler returned nil")
	}
	if got := NewUserHandler(nil, nil, nil); got == nil {
		t.Fatal("NewUserHandler returned nil")
	}
	if got := NewProxyHandler(nil); got == nil {
		t.Fatal("NewProxyHandler returned nil")
	}
	if got := NewSubscriptionHandler(nil); got == nil {
		t.Fatal("NewSubscriptionHandler returned nil")
	}
	if got := NewMonitorHandler(nil); got == nil {
		t.Fatal("NewMonitorHandler returned nil")
	}
	if got := NewVersionHandler(); got == nil {
		t.Fatal("NewVersionHandler returned nil")
	}
}

func TestParseHelpers(t *testing.T) {
	parseTests := []struct {
		name string
		call func(string) (int, error)
	}{
		{name: "account", call: parseAccountID},
		{name: "api key", call: parseKeyID},
		{name: "group", call: parseGroupID},
		{name: "user", call: parseUserID},
		{name: "proxy", call: parseProxyID},
		{name: "subscription", call: parseSubscriptionID},
		{name: "monitor", call: parseMonitorID},
	}
	for _, tt := range parseTests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := tt.call("42"); err != nil || got != 42 {
				t.Fatalf("parse valid = %d, %v", got, err)
			}
			if _, err := tt.call("bad"); err == nil {
				t.Fatal("parse invalid returned nil error")
			}
		})
	}

	if got := parseOptionalInt(""); got != nil {
		t.Fatalf("parseOptionalInt empty = %v", *got)
	}
	if got := parseOptionalInt("bad"); got != nil {
		t.Fatalf("parseOptionalInt bad = %v", *got)
	}
	if got := parseOptionalInt("7"); got == nil || *got != 7 {
		t.Fatalf("parseOptionalInt valid = %v", got)
	}
	for _, raw := range []string{"1", "true", "YES", " y ", "on"} {
		if !parseOptionalBool(raw) {
			t.Fatalf("parseOptionalBool(%q) = false", raw)
		}
	}
	if parseOptionalBool("false") {
		t.Fatal("parseOptionalBool(false) = true")
	}
	ids := parseIDList("1, bad, 2, ,3")
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Fatalf("parseIDList = %#v", ids)
	}
	if got := parseIDList(""); got != nil {
		t.Fatalf("parseIDList empty = %#v", got)
	}
}

func TestMonitorTimeParsing(t *testing.T) {
	if got, err := parseMonitorTime("", false); err != nil || got != nil {
		t.Fatalf("empty monitor time = %v, %v", got, err)
	}
	ts := "2026-06-20T01:02:03Z"
	got, err := parseMonitorTime(ts, false)
	if err != nil {
		t.Fatalf("parse RFC3339: %v", err)
	}
	if got.UTC().Format(time.RFC3339) != ts {
		t.Fatalf("RFC3339 time = %v", got)
	}
	start, err := parseMonitorTime("2026-06-20", false)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	end, err := parseMonitorTime("2026-06-20", true)
	if err != nil {
		t.Fatalf("parse end date: %v", err)
	}
	if !end.After(*start) || end.Sub(*start) != 24*time.Hour-time.Nanosecond {
		t.Fatalf("end of day = %v start=%v", end, start)
	}
	if _, err := parseMonitorTime("not-a-date", false); err == nil {
		t.Fatal("invalid monitor time returned nil error")
	}
}

func TestContextAndPointerHelpers(t *testing.T) {
	c, _ := newHandlerTestContext()
	if got, ok := currentUserID(c); ok || got != 0 {
		t.Fatalf("missing currentUserID = %d, %v", got, ok)
	}
	c.Set("user_id", "bad")
	if got, ok := currentUserID(c); ok || got != 0 {
		t.Fatalf("bad currentUserID = %d, %v", got, ok)
	}
	c.Set("user_id", 12)
	if got, ok := currentUserID(c); !ok || got != 12 {
		t.Fatalf("valid currentUserID = %d, %v", got, ok)
	}
	if got := scopedAPIKeyID(c); got != 0 {
		t.Fatalf("missing scopedAPIKeyID = %d", got)
	}
	c.Set(middleware.CtxKeyAPIKeyID, "bad")
	if got := scopedAPIKeyID(c); got != 0 {
		t.Fatalf("bad scopedAPIKeyID = %d", got)
	}
	c.Set(middleware.CtxKeyAPIKeyID, 99)
	if got := scopedAPIKeyID(c); got != 99 {
		t.Fatalf("valid scopedAPIKeyID = %d", got)
	}

	if got := ptrInt64Value(nil); got != 0 {
		t.Fatalf("ptrInt64Value nil = %d", got)
	}
	value := int64(55)
	if got := ptrInt64Value(&value); got != 55 {
		t.Fatalf("ptrInt64Value = %d", got)
	}

	if got := derefInt64Slice(nil); got != nil {
		t.Fatalf("derefInt64Slice nil = %#v", got)
	}
	input := []int64{1, 2}
	got := derefInt64Slice(&input)
	got[0] = 99
	if input[0] != 1 {
		t.Fatalf("derefInt64Slice shares backing array: %#v", input)
	}
}

func TestFamilyCooldownDTOsAndNilSchedulerFallbacks(t *testing.T) {
	h := NewAccountHandler(nil, nil)
	if got := h.familyCooldownsFor(context.Background(), 7); got != nil {
		t.Fatalf("familyCooldownsFor nil scheduler = %#v", got)
	}
	if got := h.familyCooldownsForAccounts(context.Background(), []int{7}); got != nil {
		t.Fatalf("familyCooldownsForAccounts nil scheduler = %#v", got)
	}
	if got := h.familyCooldownsForAccounts(context.Background(), nil); got != nil {
		t.Fatalf("familyCooldownsForAccounts empty = %#v", got)
	}
	if got := familyCooldownDTOs(nil); got != nil {
		t.Fatalf("familyCooldownDTOs nil = %#v", got)
	}
	until := time.Date(2026, 6, 20, 1, 2, 3, 0, time.FixedZone("test", 8*3600))
	dtos := familyCooldownDTOs([]scheduler.FamilyCooldownEntry{{Family: "gpt-image", Until: until, Reason: "429"}})
	if len(dtos) != 1 || dtos[0].Family != "gpt-image" || dtos[0].Until != "2026-06-19T17:02:03Z" || dtos[0].Reason != "429" {
		t.Fatalf("familyCooldownDTOs = %#v", dtos)
	}
}

func TestHandlerErrorMappings(t *testing.T) {
	accountHandler := NewAccountHandler(nil, nil)
	for _, tt := range []struct {
		err    error
		status int
	}{
		{appaccount.ErrAccountNotFound, 404},
		{appaccount.ErrPluginNotFound, 500},
		{appaccount.ErrReauthRequired, 422},
		{appaccount.ErrModelRequired, 400},
		{errors.New("other"), 500},
	} {
		if status, _ := accountHandler.handleError("account", "public", tt.err); status != tt.status {
			t.Fatalf("account handleError(%v) = %d, want %d", tt.err, status, tt.status)
		}
	}

	apiKeyHandler := NewAPIKeyHandler(nil, nil)
	for _, tt := range []struct {
		err    error
		status int
	}{
		{appapikey.ErrKeyNotFound, 404},
		{appapikey.ErrGroupForbidden, 403},
		{appapikey.ErrInvalidSellRate, 400},
		{errors.New("other"), 500},
	} {
		if status, _ := apiKeyHandler.handleError("key", "public", tt.err); status != tt.status {
			t.Fatalf("api key handleError(%v) = %d, want %d", tt.err, status, tt.status)
		}
	}

	groupHandler := NewGroupHandler(nil, nil)
	for _, tt := range []struct {
		err    error
		status int
	}{
		{appgroup.ErrGroupNotFound, 404},
		{appgroup.ErrGroupHasSubscriptions, 400},
		{appgroup.ErrInvalidRateMultiplier, 400},
		{errors.New("other"), 500},
	} {
		if status, _ := groupHandler.handleError("group", "public", tt.err); status != tt.status {
			t.Fatalf("group handleError(%v) = %d, want %d", tt.err, status, tt.status)
		}
	}

	userHandler := NewUserHandler(nil, nil, nil)
	for _, tt := range []struct {
		err    error
		status int
	}{
		{appuser.ErrUserNotFound, 404},
		{appuser.ErrEmailAlreadyExists, 400},
		{appuser.ErrInvalidRateMultiplier, 400},
		{errors.New("other"), 500},
	} {
		if status, _ := userHandler.handleError("user", "public", tt.err); status != tt.status {
			t.Fatalf("user handleError(%v) = %d, want %d", tt.err, status, tt.status)
		}
	}

	if status, _ := NewProxyHandler(nil).handleError("proxy", "public", appproxy.ErrProxyNotFound); status != 404 {
		t.Fatalf("proxy not found status = %d", status)
	}
	if status, _ := NewProxyHandler(nil).handleError("proxy", "public", errors.New("other")); status != 500 {
		t.Fatalf("proxy default status = %d", status)
	}
	if status, _ := NewSubscriptionHandler(nil).handleError("subscription", "public", appsubscription.ErrSubscriptionNotFound); status != 404 {
		t.Fatalf("subscription not found status = %d", status)
	}
	if status, _ := NewSubscriptionHandler(nil).handleError("subscription", "public", appsubscription.ErrInvalidExpiresAt); status != 400 {
		t.Fatalf("subscription invalid status = %d", status)
	}
	if status, _ := NewSubscriptionHandler(nil).handleError("subscription", "public", errors.New("other")); status != 500 {
		t.Fatalf("subscription default status = %d", status)
	}
	if status, _ := handleMonitorError("monitor", "public", appmonitor.ErrEventNotFound); status != 404 {
		t.Fatalf("monitor not found status = %d", status)
	}
	if status, _ := handleMonitorError("monitor", "public", appmonitor.ErrEventNotRecoverable); status != 409 {
		t.Fatalf("monitor not recoverable status = %d", status)
	}
	if status, _ := handleMonitorError("monitor", "public", errors.New("other")); status != 500 {
		t.Fatalf("monitor default status = %d", status)
	}
}

func TestVersionHandlerGetVersion(t *testing.T) {
	c, w := newHandlerTestContext()

	NewVersionHandler().GetVersion(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body == "" || !strings.Contains(body, "go_version") || !strings.Contains(body, "platform") {
		t.Fatalf("version body missing fields: %s", body)
	}
}

func TestDefaultUserMaxConcurrencyNilService(t *testing.T) {
	if got := defaultUserMaxConcurrency(context.Background(), nil); got != fallbackDefaultUserMaxConcurrency {
		t.Fatalf("defaultUserMaxConcurrency = %d", got)
	}
}
