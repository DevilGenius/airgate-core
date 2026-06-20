package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func TestMonitorMapperAdditionalCoverage(t *testing.T) {
	accountID := 7
	createdAt := time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	resolvedAt := createdAt.Add(2 * time.Minute)
	autoResolveAt := createdAt.Add(3 * time.Minute)
	expiresAt := createdAt.Add(time.Hour)
	lastNotifiedAt := createdAt.Add(4 * time.Minute)
	nextNotifyAt := createdAt.Add(5 * time.Minute)
	detail := map[string]interface{}{"reason": "rate-limit"}

	event := appmonitor.Event{
		ID:                  11,
		Type:                "account_error",
		Severity:            "critical",
		Status:              "active",
		RecoveryMode:        "auto",
		Source:              "scheduler",
		SubjectType:         "account",
		SubjectID:           "7",
		Hash:                "hash",
		Title:               "title",
		Message:             "message",
		AccountID:           &accountID,
		AccountNameSnapshot: "primary",
		Platform:            "openai",
		PluginID:            "gateway-openai",
		TaskType:            "quota",
		ErrorCode:           "429",
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
		ResolvedAt:          &resolvedAt,
		AutoResolveAt:       &autoResolveAt,
		ExpiresAt:           expiresAt,
		LastNotifiedAt:      &lastNotifiedAt,
		NextNotifyAt:        &nextNotifyAt,
		NotifyError:         "smtp failed",
		Detail:              detail,
	}

	resp := toMonitorEventResp(event)
	if resp.ID != event.ID || resp.Type != event.Type || resp.Severity != event.Severity || resp.Status != event.Status ||
		resp.RecoveryMode != event.RecoveryMode || resp.Source != event.Source || resp.SubjectType != event.SubjectType ||
		resp.SubjectID != event.SubjectID || resp.Hash != event.Hash || resp.Title != event.Title || resp.Message != event.Message ||
		resp.AccountID == nil || *resp.AccountID != accountID || resp.AccountNameSnapshot != event.AccountNameSnapshot ||
		resp.Platform != event.Platform || resp.PluginID != event.PluginID || resp.TaskType != event.TaskType ||
		resp.ErrorCode != event.ErrorCode || resp.NotifyError != event.NotifyError || resp.Detail["reason"] != "rate-limit" {
		t.Fatalf("monitor event response did not copy fields: %+v", resp)
	}
	if resp.CreatedAt != "2026-06-20T01:02:03Z" || resp.UpdatedAt != "2026-06-20T01:03:03Z" ||
		resp.ExpiresAt != "2026-06-20T02:02:03Z" {
		t.Fatalf("monitor event time fields = %+v", resp)
	}
	if resp.ResolvedAt == nil || *resp.ResolvedAt != "2026-06-20T01:04:03Z" ||
		resp.AutoResolveAt == nil || *resp.AutoResolveAt != "2026-06-20T01:05:03Z" ||
		resp.LastNotifiedAt == nil || *resp.LastNotifiedAt != "2026-06-20T01:06:03Z" ||
		resp.NextNotifyAt == nil || *resp.NextNotifyAt != "2026-06-20T01:07:03Z" {
		t.Fatalf("monitor event pointer time fields = %+v", resp)
	}

	if got := toMonitorListResp(appmonitor.ListResult{
		List:       []appmonitor.Event{event},
		HasMore:    true,
		NextCursor: &appmonitor.ListCursor{UpdatedAt: updatedAt, ID: 10},
	}); len(got.List) != 1 || !got.HasMore || got.NextCursor == nil || got.NextCursor.ID != 10 || got.NextCursor.UpdatedAt != "2026-06-20T01:03:03Z" {
		t.Fatalf("monitor list response = %+v", got)
	}
	if got := toMonitorListResp(appmonitor.ListResult{}); got.NextCursor != nil || len(got.List) != 0 {
		t.Fatalf("empty monitor list response = %+v", got)
	}

	summary := toMonitorSummaryResp(appmonitor.Summary{
		ActiveTotal: 9, CriticalTotal: 8, CriticalActiveTotal: 7, ErrorTotal: 6, ErrorActiveTotal: 5,
		WarningTotal: 4, WarningActiveTotal: 3, InfoTotal: 2, InfoActiveTotal: 1,
		ByType:      []appmonitor.TypeCount{{Type: "quota", Count: 12}},
		TopAccounts: []appmonitor.SubjectCount{{ID: 7, Name: "primary", Count: 4}},
		Recent:      []appmonitor.Event{event},
	})
	if summary.ActiveTotal != 9 || summary.CriticalActiveTotal != 7 || summary.InfoActiveTotal != 1 ||
		len(summary.ByType) != 1 || summary.ByType[0].Type != "quota" || len(summary.TopAccounts) != 1 ||
		summary.TopAccounts[0].Name != "primary" || len(summary.Recent) != 1 {
		t.Fatalf("monitor summary response = %+v", summary)
	}
}

func TestMonitorRequestMapperAdditionalCoverage(t *testing.T) {
	apiKeyID, userID, groupID, accountID := 3, 4, 5, 6
	httpStatus, upstreamStatus := 400, 429
	createdAt := time.Date(2026, 6, 20, 3, 4, 5, 0, time.UTC)
	expiresAt := createdAt.Add(time.Hour)
	item := appmonitor.RequestEvent{
		ID:                  21,
		Type:                "request_error",
		Severity:            "warning",
		Source:              "proxy",
		Hash:                "hash",
		Fingerprint:         "fingerprint",
		Title:               "title",
		Message:             "message",
		RequestID:           "req-1",
		APIKeyID:            &apiKeyID,
		APIKeyNameSnapshot:  "key",
		UserID:              &userID,
		UserEmailSnapshot:   "u@example.com",
		GroupID:             &groupID,
		AccountID:           &accountID,
		AccountNameSnapshot: "primary",
		Platform:            "openai",
		PluginID:            "gateway-openai",
		Method:              http.MethodPost,
		Endpoint:            "/v1/chat/completions",
		Model:               "gpt-4.1",
		HTTPStatus:          &httpStatus,
		UpstreamStatus:      &upstreamStatus,
		ErrorCode:           "rate_limit",
		DurationMS:          123,
		CreatedAt:           createdAt,
		ExpiresAt:           expiresAt,
		Detail:              map[string]interface{}{"attempt": float64(2)},
	}

	resp := toMonitorRequestEventResp(item)
	if resp.ID != item.ID || resp.Type != item.Type || resp.Severity != item.Severity || resp.Source != item.Source ||
		resp.Hash != item.Hash || resp.Fingerprint != item.Fingerprint || resp.RequestID != item.RequestID ||
		resp.APIKeyID == nil || *resp.APIKeyID != apiKeyID || resp.UserID == nil || *resp.UserID != userID ||
		resp.GroupID == nil || *resp.GroupID != groupID || resp.AccountID == nil || *resp.AccountID != accountID ||
		resp.HTTPStatus == nil || *resp.HTTPStatus != httpStatus || resp.UpstreamStatus == nil || *resp.UpstreamStatus != upstreamStatus ||
		resp.Method != http.MethodPost || resp.Endpoint != item.Endpoint || resp.Model != item.Model ||
		resp.CreatedAt != "2026-06-20T03:04:05Z" || resp.ExpiresAt != "2026-06-20T04:04:05Z" ||
		resp.Detail["attempt"] != float64(2) {
		t.Fatalf("request monitor response did not copy fields: %+v", resp)
	}

	listResp := toMonitorRequestListResp(appmonitor.RequestListResult{
		List:       []appmonitor.RequestEvent{item},
		HasMore:    true,
		NextCursor: &appmonitor.RequestListCursor{CreatedAt: createdAt, ID: 20},
	})
	if len(listResp.List) != 1 || !listResp.HasMore || listResp.NextCursor == nil ||
		listResp.NextCursor.ID != 20 || listResp.NextCursor.CreatedAt != "2026-06-20T03:04:05Z" {
		t.Fatalf("request monitor list response = %+v", listResp)
	}
	if got := toMonitorRequestListResp(appmonitor.RequestListResult{}); got.NextCursor != nil || len(got.List) != 0 {
		t.Fatalf("empty request monitor list response = %+v", got)
	}
}

func TestMonitorTimeStringHelpers(t *testing.T) {
	if got := monitorTimeString(time.Time{}); got != "" {
		t.Fatalf("zero monitor time string = %q", got)
	}
	localTime := time.Date(2026, 6, 20, 9, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	if got := monitorTimeString(localTime); got != "2026-06-20T01:00:00Z" {
		t.Fatalf("monitor time string = %q", got)
	}
	if got := monitorTimePtrString(nil); got != nil {
		t.Fatalf("nil monitor time pointer string = %v", *got)
	}
	zero := time.Time{}
	if got := monitorTimePtrString(&zero); got != nil {
		t.Fatalf("zero monitor time pointer string = %v", *got)
	}
	if got := monitorTimePtrString(&localTime); got == nil || *got != "2026-06-20T01:00:00Z" {
		t.Fatalf("monitor time pointer string = %v", got)
	}
}

func TestMonitorListFilterFromQuery(t *testing.T) {
	accountID := 7
	c, _ := newHandlerTestContext()

	filter, ok := monitorListFilterFromQuery(c, dto.MonitorListQuery{
		Status: "active", Severity: "critical", Type: "quota", Source: "scheduler", SubjectType: "account",
		AccountID: &accountID, Platform: "openai", PluginID: "gateway-openai", TaskType: "refresh", ErrorCode: "429",
		From: "2026-06-20T01:02:03Z", To: "2026-06-21", Limit: 25,
		Cursor: "2026-06-20T00:00:00Z", CursorID: 99,
	})
	if !ok {
		t.Fatal("monitor list filter rejected valid query")
	}
	if filter.Status != "active" || filter.Severity != "critical" || filter.Type != "quota" ||
		filter.Source != "scheduler" || filter.SubjectType != "account" || filter.AccountID == nil ||
		*filter.AccountID != accountID || filter.Platform != "openai" || filter.PluginID != "gateway-openai" ||
		filter.TaskType != "refresh" || filter.ErrorCode != "429" || filter.Limit != 25 {
		t.Fatalf("monitor list filter did not copy fields: %+v", filter)
	}
	if filter.From == nil || filter.From.UTC().Format(time.RFC3339) != "2026-06-20T01:02:03Z" ||
		filter.To == nil || !filter.To.After(*filter.From) || filter.Cursor == nil || filter.Cursor.ID != 99 ||
		filter.Cursor.UpdatedAt.UTC().Format(time.RFC3339) != "2026-06-20T00:00:00Z" {
		t.Fatalf("monitor list filter time/cursor fields = %+v", filter)
	}

	filter, ok = monitorListFilterFromQuery(c, dto.MonitorListQuery{
		CursorUpdatedAt: "2026-06-20T02:00:00Z",
		CursorID:        100,
	})
	if !ok || filter.Cursor == nil || filter.Cursor.ID != 100 || filter.Cursor.UpdatedAt.UTC().Format(time.RFC3339) != "2026-06-20T02:00:00Z" {
		t.Fatalf("monitor list filter cursor_updated_at branch = %+v ok=%v", filter, ok)
	}

	for _, tt := range []struct {
		name  string
		query dto.MonitorListQuery
	}{
		{name: "bad from", query: dto.MonitorListQuery{From: "not-a-date"}},
		{name: "bad to", query: dto.MonitorListQuery{To: "not-a-date"}},
		{name: "bad cursor time", query: dto.MonitorListQuery{Cursor: "not-a-date", CursorID: 1}},
		{name: "cursor without id", query: dto.MonitorListQuery{Cursor: "2026-06-20T00:00:00Z"}},
		{name: "id without cursor", query: dto.MonitorListQuery{CursorID: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, w := newHandlerTestContext()
			if _, ok := monitorListFilterFromQuery(c, tt.query); ok {
				t.Fatal("invalid monitor list query accepted")
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMonitorRequestListFilterFromQuery(t *testing.T) {
	apiKeyID, groupID, accountID := 1, 2, 3
	httpStatus, upstreamStatus := 400, 429
	c, _ := newHandlerTestContext()

	filter, ok := monitorRequestListFilterFromQuery(c, dto.MonitorRequestListQuery{
		Severity: "warning", Type: "request_error", Source: "proxy", APIKeyID: &apiKeyID, GroupID: &groupID,
		AccountID: &accountID, Platform: "openai", PluginID: "gateway-openai", Method: http.MethodPost,
		Endpoint: "/v1/responses", Model: "gpt-4.1", HTTPStatus: &httpStatus, UpstreamStatus: &upstreamStatus,
		ErrorCode: "rate_limit", From: "2026-06-20T01:02:03Z", To: "2026-06-21", Limit: 30,
		Cursor: "2026-06-20T00:00:00Z", CursorID: 88,
	})
	if !ok {
		t.Fatal("request monitor filter rejected valid query")
	}
	if filter.Severity != "warning" || filter.Type != "request_error" || filter.Source != "proxy" ||
		filter.APIKeyID == nil || *filter.APIKeyID != apiKeyID || filter.GroupID == nil || *filter.GroupID != groupID ||
		filter.AccountID == nil || *filter.AccountID != accountID || filter.Platform != "openai" ||
		filter.PluginID != "gateway-openai" || filter.Method != http.MethodPost || filter.Endpoint != "/v1/responses" ||
		filter.Model != "gpt-4.1" || filter.HTTPStatus == nil || *filter.HTTPStatus != httpStatus ||
		filter.UpstreamStatus == nil || *filter.UpstreamStatus != upstreamStatus || filter.ErrorCode != "rate_limit" ||
		filter.Limit != 30 || filter.Cursor == nil || filter.Cursor.ID != 88 {
		t.Fatalf("request monitor filter did not copy fields: %+v", filter)
	}

	filter, ok = monitorRequestListFilterFromQuery(c, dto.MonitorRequestListQuery{
		CursorCreatedAt: "2026-06-20T03:00:00Z",
		CursorID:        101,
	})
	if !ok || filter.Cursor == nil || filter.Cursor.ID != 101 || filter.Cursor.CreatedAt.UTC().Format(time.RFC3339) != "2026-06-20T03:00:00Z" {
		t.Fatalf("request monitor cursor_created_at branch = %+v ok=%v", filter, ok)
	}

	for _, tt := range []struct {
		name  string
		query dto.MonitorRequestListQuery
	}{
		{name: "bad from", query: dto.MonitorRequestListQuery{From: "not-a-date"}},
		{name: "bad to", query: dto.MonitorRequestListQuery{To: "not-a-date"}},
		{name: "bad cursor time", query: dto.MonitorRequestListQuery{Cursor: "not-a-date", CursorID: 1}},
		{name: "cursor without id", query: dto.MonitorRequestListQuery{Cursor: "2026-06-20T00:00:00Z"}},
		{name: "id without cursor", query: dto.MonitorRequestListQuery{CursorID: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, w := newHandlerTestContext()
			if _, ok := monitorRequestListFilterFromQuery(c, tt.query); ok {
				t.Fatal("invalid request monitor query accepted")
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestDecodeRawJSONBodyAdditionalCoverage(t *testing.T) {
	c, _ := newHandlerTestContext()
	got, err := decodeRawJSONBody(c)
	if err != nil || got != nil {
		t.Fatalf("missing raw body = %#v, %v", got, err)
	}

	c.Set(gin.BodyBytesKey, "not-bytes")
	got, err = decodeRawJSONBody(c)
	if err != nil || got != nil {
		t.Fatalf("non-byte raw body = %#v, %v", got, err)
	}

	c.Set(gin.BodyBytesKey, []byte{})
	got, err = decodeRawJSONBody(c)
	if err != nil || got != nil {
		t.Fatalf("empty raw body = %#v, %v", got, err)
	}

	c.Set(gin.BodyBytesKey, []byte(`{"extra":{"enabled":true},"proxy_id":null}`))
	got, err = decodeRawJSONBody(c)
	if err != nil {
		t.Fatalf("decode valid raw body: %v", err)
	}
	if _, ok := got["extra"]; !ok {
		t.Fatalf("raw body missing extra key: %#v", got)
	}
	if string(got["proxy_id"]) != "null" {
		t.Fatalf("raw proxy_id = %s", got["proxy_id"])
	}

	c.Set(gin.BodyBytesKey, []byte(`{`))
	if _, err := decodeRawJSONBody(c); err == nil {
		t.Fatal("invalid JSON raw body returned nil error")
	}
}

func TestCustomerAPIKeyRateAdditionalCoverage(t *testing.T) {
	if got := customerAPIKeyRate(1.5, 2); got != 3 {
		t.Fatalf("customerAPIKeyRate valid = %v", got)
	}
	if got := customerAPIKeyRate(0, -1); got != 1 {
		t.Fatalf("customerAPIKeyRate invalid fallbacks = %v", got)
	}
	if got := customerAPIKeyRate(2, 0); got != 0 {
		t.Fatalf("customerAPIKeyRate free sell rate = %v", got)
	}
	if got := customerAPIKeyRate(math.Inf(1), 2); got != 2 {
		t.Fatalf("customerAPIKeyRate invalid group fallback = %v", got)
	}
}

func TestDefaultUserMaxConcurrencyWithSettingsService(t *testing.T) {
	ctx := context.Background()
	if got := defaultUserMaxConcurrency(ctx, appsettings.NewService(testSettingsRepo{
		items: []appsettings.Setting{
			{Key: "other", Value: "99", Group: "defaults"},
			{Key: "default_concurrency", Value: "bad", Group: "defaults"},
			{Key: "default_concurrency", Value: "-1", Group: "defaults"},
			{Key: "default_concurrency", Value: " 12 ", Group: "defaults"},
		},
	})); got != 12 {
		t.Fatalf("defaultUserMaxConcurrency from settings = %d", got)
	}
	if got := defaultUserMaxConcurrency(ctx, appsettings.NewService(testSettingsRepo{
		err: errors.New("store down"),
	})); got != fallbackDefaultUserMaxConcurrency {
		t.Fatalf("defaultUserMaxConcurrency store error = %d", got)
	}
	if got := defaultUserMaxConcurrency(ctx, appsettings.NewService(testSettingsRepo{
		items: []appsettings.Setting{{Key: "default_concurrency", Value: "0", Group: "defaults"}},
	})); got != fallbackDefaultUserMaxConcurrency {
		t.Fatalf("defaultUserMaxConcurrency invalid value = %d", got)
	}
}

type testSettingsRepo struct {
	items []appsettings.Setting
	err   error
}

func (r testSettingsRepo) List(context.Context, string) ([]appsettings.Setting, error) {
	return r.items, r.err
}

func (r testSettingsRepo) UpsertMany(context.Context, []appsettings.ItemInput) error {
	return nil
}

func TestMonitorFilterContextCanUseBareGinContext(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	if _, ok := monitorListFilterFromQuery(c, dto.MonitorListQuery{}); !ok {
		t.Fatalf("bare context monitor filter rejected empty query; status=%d", w.Code)
	}
}
