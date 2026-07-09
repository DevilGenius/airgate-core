package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	appsubscription "github.com/DevilGenius/airgate-core/internal/app/subscription"
)

func TestAccountAndCredentialSchemaMappersCoverOptionalFields(t *testing.T) {
	lastUsed := time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC)
	stateUntil := time.Date(2026, 6, 20, 9, 2, 3, 0, time.FixedZone("cst", 8*3600))
	deletedAt := time.Date(2026, 6, 21, 9, 2, 3, 0, time.FixedZone("cst", 8*3600))
	email := "oauth@example.com"
	resp := toAccountResp(appaccount.Account{
		ID:                 9,
		Name:               "oauth",
		Email:              &email,
		Platform:           "openai",
		Type:               "oauth",
		Credentials:        map[string]string{"access_token": "tok"},
		State:              "rate_limited",
		StateUntil:         &stateUntil,
		Priority:           3,
		MaxConcurrency:     4,
		CurrentConcurrency: 2,
		RateMultiplier:     1.5,
		ErrorMsg:           "429",
		UpstreamIsPool:     true,
		LastUsedAt:         &lastUsed,
		DeletedAt:          &deletedAt,
		Proxy:              &appaccount.Proxy{ID: 7},
		Extra:              map[string]any{"plan": "plus"},
		ImageStats:         &appaccount.AccountImageStats{TodayCount: 5, TotalCount: 8},
		CreatedAt:          lastUsed,
		UpdatedAt:          lastUsed,
	})
	if resp.ID != 9 || resp.Email == nil || *resp.Email != email || resp.ProxyID == nil || *resp.ProxyID != 7 || resp.LastUsedAt == nil || *resp.LastUsedAt != "2026-06-20T01:02:03Z" {
		t.Fatalf("account response optional fields = %+v", resp)
	}
	if resp.StateUntil == nil || *resp.StateUntil != "2026-06-20T01:02:03Z" {
		t.Fatalf("state until = %#v, want UTC formatted", resp.StateUntil)
	}
	if resp.DeletedAt == nil || *resp.DeletedAt != "2026-06-21T01:02:03Z" {
		t.Fatalf("deleted at = %#v, want UTC formatted", resp.DeletedAt)
	}
	if resp.TodayImageCount == nil || *resp.TodayImageCount != 5 || resp.TotalImageCount == nil || *resp.TotalImageCount != 8 {
		t.Fatalf("image counters = %#v/%#v", resp.TodayImageCount, resp.TotalImageCount)
	}
	if resp.GroupIDs == nil || len(resp.GroupIDs) != 0 {
		t.Fatalf("nil group IDs should map to empty slice: %#v", resp.GroupIDs)
	}

	schema := toCredentialSchemaResp(appaccount.CredentialSchema{
		Fields: []appaccount.CredentialField{{
			Key: "api_key", Label: "API Key", Type: "password", Required: true, Placeholder: "sk-", EditDisabled: true,
		}},
		AccountTypes: []appaccount.AccountType{{
			Key: "oauth", Label: "OAuth", Description: "Browser login",
			Fields: []appaccount.CredentialField{{Key: "refresh_token", Label: "Refresh", Type: "textarea"}},
		}},
	})
	if len(schema.Fields) != 1 || !schema.Fields[0].EditDisabled || len(schema.AccountTypes) != 1 || len(schema.AccountTypes[0].Fields) != 1 {
		t.Fatalf("credential schema response = %+v", schema)
	}
}

func TestToAccountExportItemContainsOnlyPortableFields(t *testing.T) {
	email := "demo@example.com"
	item := toAccountExportItem(appaccount.Account{
		Name:           "demo",
		Email:          &email,
		Platform:       "openai",
		Type:           "apikey",
		Credentials:    map[string]string{"api_key": "secret"},
		Priority:       2,
		MaxConcurrency: 4,
		RateMultiplier: 1.5,
		GroupIDs:       []int64{2, 1},
		Proxy: &appaccount.Proxy{
			ID: 7,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	if item.Email == nil || *item.Email != email {
		t.Fatalf("expected export email %q, got %#v", email, item.Email)
	}

	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal export item: %v", err)
	}
	jsonText := string(payload)
	if strings.Contains(jsonText, "group_ids") {
		t.Fatalf("expected export JSON to omit group_ids, got %s", jsonText)
	}
	if strings.Contains(jsonText, "proxy_id") {
		t.Fatalf("expected export JSON to omit proxy_id, got %s", jsonText)
	}
}

func TestAPIKeyAndSubscriptionMapperOptionalBranches(t *testing.T) {
	expires := time.Date(2026, 6, 20, 1, 2, 3, 0, time.FixedZone("utc+8", 8*3600))
	groupID := 12
	keyResp := toAPIKeyResp(appapikey.Key{
		ID:                    3,
		Name:                  "prod",
		KeyHint:               "sk-1234",
		PlainKey:              "sk-live",
		UserID:                4,
		GroupID:               &groupID,
		GroupRate:             1.2,
		IPWhitelist:           []string{"127.0.0.1"},
		IPBlacklist:           []string{"10.0.0.1"},
		QuotaUSD:              50,
		UsedQuota:             5,
		UsedQuotaActual:       4,
		SellRate:              1.1,
		MaxConcurrency:        6,
		BalanceAlertEnabled:   true,
		BalanceAlertEmail:     "ops@example.com",
		BalanceAlertThreshold: 7,
		TodayCost:             1,
		TodayActualCost:       0.9,
		ThirtyDayCost:         10,
		ThirtyDayActualCost:   9,
		Status:                "active",
		ExpiresAt:             &expires,
	})
	if keyResp.GroupID == nil || *keyResp.GroupID != 12 || keyResp.ExpiresAt == nil || *keyResp.ExpiresAt != "2026-06-20T01:02:03+08:00" {
		t.Fatalf("api key response optionals = %+v", keyResp)
	}

	noOptionalKey := toAPIKeyResp(appapikey.Key{ID: 5, UserID: 6})
	if noOptionalKey.GroupID != nil || noOptionalKey.ExpiresAt != nil {
		t.Fatalf("nil API key optionals should stay nil: %+v", noOptionalKey)
	}

	progress := toSubscriptionProgressRespFromDomain(appsubscription.SubscriptionProgress{
		GroupID:   9,
		GroupName: "pro",
		Daily:     &appsubscription.UsageWindow{Used: 1, Limit: 2, Reset: "day"},
		Weekly:    &appsubscription.UsageWindow{Used: 3, Limit: 4, Reset: "week"},
		Monthly:   &appsubscription.UsageWindow{Used: 5, Limit: 6, Reset: "month"},
	})
	if progress.Daily == nil || progress.Weekly == nil || progress.Monthly == nil || progress.Monthly.Reset != "month" {
		t.Fatalf("subscription progress = %+v", progress)
	}
	if cloned := cloneUsage(nil); cloned != nil {
		t.Fatalf("cloneUsage(nil) = %#v", cloned)
	}
}
