package account

import (
	"strings"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestAdditionalOAuthPlanFilterBranches(t *testing.T) {
	for _, raw := range []string{"plain", "oauth_plan:openai", "oauth_plan::plus", "oauth_plan:openai:"} {
		if _, _, ok := parseOAuthPlanFilterID(raw); ok {
			t.Fatalf("parseOAuthPlanFilterID(%q) returned ok", raw)
		}
	}
	if platform, key, ok := parseOAuthPlanFilterID("oauth_plan: openai : plus "); !ok || platform != "openai" || key != "plus" {
		t.Fatalf("parse valid filter = %q %q %v", platform, key, ok)
	}

	if got := pluginOAuthPlanFilters(plugin.PluginMeta{}); got != nil {
		t.Fatalf("empty plugin filters = %#v", got)
	}
	if got := pluginOAuthPlanFilters(plugin.PluginMeta{Platform: "openai", Metadata: map[string]string{oauthPlanMetadataKey: "{"}}); got != nil {
		t.Fatalf("invalid plugin filters = %#v", got)
	}
	meta := plugin.PluginMeta{
		Platform: "openai",
		Metadata: map[string]string{oauthPlanMetadataKey: `[
			{"key":" ", "label":"ignored"},
			{"key":"plus", "label":" Plus ", "credential_key":" plan ", "match":"contains", "matches":["plus"," plus ",""]},
			{"key":"team", "matches":[" "]},
			{"key":"pro"}
		]`},
	}
	filters := pluginOAuthPlanFilters(meta)
	if len(filters) != 2 {
		t.Fatalf("filters = %+v, want two valid filters", filters)
	}
	if filters[0].Key != "plus" || filters[0].Label != "Plus" || filters[0].CredentialKey != "plan" ||
		filters[0].MatchMode != "contains" || len(filters[0].Matches) != 1 || filters[0].Matches[0] != "plus" {
		t.Fatalf("contains filter = %+v", filters[0])
	}
	if filters[1].Key != "pro" || filters[1].Label != "pro" || filters[1].CredentialKey != defaultOAuthPlanCredential ||
		filters[1].MatchMode != "exact" || len(filters[1].Matches) != 1 || filters[1].Matches[0] != "pro" {
		t.Fatalf("fallback filter = %+v", filters[1])
	}

	if got := normalizedPlanMatches([]string{" a ", "a", "", "b"}, "fallback"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("normalized matches = %#v", got)
	}
}

func TestAdditionalUsageContractCacheAndWindowBranches(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	if (accountUsageCachePayload{}).valid() {
		t.Fatal("empty payload should be invalid")
	}
	if _, ok := (accountUsageCachePayload{FetchedAt: "bad"}).cacheInfo(now); ok {
		t.Fatal("bad fetched_at cacheInfo should be false")
	}
	if got := accountUsageInfoExpiresAt(AccountUsageInfo{}, now); !got.Equal(now) {
		t.Fatalf("empty usage expires = %s, want now", got)
	}
	if got := accountUsageInfoExpiresAt(AccountUsageInfo{Credits: &AccountUsageCredits{Balance: 1}}, now); !got.Equal(now.Add(usageCacheMaxTTL)) {
		t.Fatalf("credits usage expires = %s", got)
	}

	resetAt := now.Add(time.Hour).Format(time.RFC3339)
	if got, ok := accountUsageWindowResetAt(AccountUsageWindow{ResetAt: resetAt}, now); !ok || got.Format(time.RFC3339) != resetAt {
		t.Fatalf("reset_at branch = %s %v", got, ok)
	}
	if got, ok := accountUsageWindowResetAt(AccountUsageWindow{ResetAt: "bad", ResetSeconds: 60}, now); !ok || got.Sub(now) != time.Minute {
		t.Fatalf("reset seconds branch = %s %v", got, ok)
	}
	if got, ok := accountUsageWindowResetAt(AccountUsageWindow{ResetAfterSeconds: 120}, now); !ok || got.Sub(now) != 2*time.Minute {
		t.Fatalf("reset after branch = %s %v", got, ok)
	}
	if _, ok := normalizeAccountUsageWindow(AccountUsageWindow{}); ok {
		t.Fatal("empty usage window should be rejected")
	}

	windows := []AccountUsageWindow{
		{Slot: "custom", Group: "b", Key: "b", Label: "b"},
		{Slot: "custom", Group: "a", Key: "z", Label: "z"},
		{Slot: "custom", Group: "a", Key: "a", Label: "z"},
		{Slot: "custom", Group: "a", Key: "a", Label: "a"},
	}
	sortAccountUsageWindows(windows)
	gotOrder := []string{windows[0].Label, windows[1].Label, windows[2].Key, windows[3].Group}
	wantOrder := []string{"a", "z", "z", "b"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("sorted order = %+v, want %+v; windows=%+v", gotOrder, wantOrder, windows)
		}
	}
	if slotSortRank("unknown") != 3 {
		t.Fatal("unknown slot should sort last")
	}
	if got := liveAccountUsageWindows(nil, now); len(got) != 0 {
		t.Fatalf("live nil windows = %+v", got)
	}
	expiredInfo := liveAccountUsageInfo(AccountUsageInfo{Credits: &AccountUsageCredits{Balance: 3}}, now, now.Add(-time.Second))
	if expiredInfo.Credits != nil {
		t.Fatalf("expired credits should be dropped: %+v", expiredInfo.Credits)
	}
}

func TestAdditionalUsageWindowInferenceAndMapBranches(t *testing.T) {
	if got := accountUsageWindowIdentity(AccountUsageWindow{Slot: "5h", Label: "Five Hours"}); got != "base:5h" {
		t.Fatalf("slot-only identity = %q", got)
	}
	if got := accountUsageWindowIdentity(AccountUsageWindow{Group: "model:opus", DisplayLabel: "Window"}); got != "model:opus:Window" {
		t.Fatalf("group label identity = %q", got)
	}
	if got := inferUsageWindowDisplayLabel("key", "", "7d"); got != "7d" {
		t.Fatalf("slot display label = %q", got)
	}
	if got := inferUsageWindowDisplayLabel(" key ", "", ""); got != "key" {
		t.Fatalf("key display label = %q", got)
	}
	if got := inferUsageWindowSlot("", "custom window"); got != "custom" {
		t.Fatalf("label slot fallback = %q", got)
	}
	if got := inferUsageWindowSlot("", ""); got != "" {
		t.Fatalf("empty slot fallback = %q", got)
	}
	for _, tt := range []struct {
		key, label, slot string
		want             string
	}{
		{"model::gpt-5", "", "", "model:gpt-5"},
		{"5h-opus", "", "5h", "model:opus"},
		{"", "7d Sonnet 4", "7d", "model:sonnet-4"},
		{"", "", "", "base"},
	} {
		if got := inferUsageWindowGroup(tt.key, tt.label, tt.slot); got != tt.want {
			t.Fatalf("infer group (%q,%q,%q) = %q, want %q", tt.key, tt.label, tt.slot, got, tt.want)
		}
	}
	if got := usageWindowLabelSuffix("7d Sonnet 4", "7d"); got != "Sonnet 4" {
		t.Fatalf("label suffix = %q", got)
	}
	if got := usageWindowLabelSuffix("Monthly", "7d"); got != "" {
		t.Fatalf("mismatched label suffix = %q", got)
	}
	if got := inferUsageWindowKey("", "", "Label Only"); got != "Label Only" {
		t.Fatalf("label-only key = %q", got)
	}
	if got := inferUsageWindowKey("base", "5h", "ignored"); got != "5h" {
		t.Fatalf("base key = %q", got)
	}
	if got := inferUsageWindowKey("model:opus", "7d", "ignored"); got != "model:opus:7d" {
		t.Fatalf("model key = %q", got)
	}

	enforce := false
	infoMap := accountUsageInfoToMap(AccountUsageInfo{
		UpdatedAt: "2026-06-20T12:00:00Z",
		Credits:   &AccountUsageCredits{Balance: 10, Unlimited: true},
		Windows: []AccountUsageWindow{{
			Key: "model:opus:7d", Label: "7d Opus", DisplayLabel: "7d", Slot: "7d", Group: "model:opus",
			UsedPercent: 80, ResetAt: "2026-06-21T12:00:00Z", ResetSeconds: 60, ResetAfterSeconds: 60,
			UpdatedAt: "2026-06-20T12:00:00Z", IgnoreLimit: true, EnforceLimit: &enforce, SortOrder: 3,
		}},
	})
	if infoMap["updated_at"] == "" || infoMap["credits"] == nil || len(infoMap["windows"].([]any)) != 1 {
		t.Fatalf("usage info map = %+v", infoMap)
	}
	windowMap := infoMap["windows"].([]any)[0].(map[string]any)
	for _, key := range []string{"key", "label", "display_label", "slot", "group", "used_percent", "reset_at", "reset_seconds", "reset_after_seconds", "updated_at", "ignore_limit", "enforce_limit", "sort_order"} {
		if _, ok := windowMap[key]; !ok {
			t.Fatalf("window map missing %q: %+v", key, windowMap)
		}
	}
	emptyMap := accountUsageInfoToMap(AccountUsageInfo{})
	if len(emptyMap) != 0 {
		t.Fatalf("empty usage info map = %+v", emptyMap)
	}
}

func TestAdditionalStatsBranches(t *testing.T) {
	if page, size := NormalizePage(3, 50); page != 3 || size != 50 {
		t.Fatalf("NormalizePage positive = %d %d", page, size)
	}
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	if _, _, err := ResolveStatsRange(now, StatsQuery{StartDate: "bad"}); err != ErrInvalidDateRange {
		t.Fatalf("bad start date err = %v", err)
	}
	if _, _, err := ResolveStatsRange(now, StatsQuery{EndDate: "bad"}); err != ErrInvalidDateRange {
		t.Fatalf("bad end date err = %v", err)
	}
	if _, _, err := ResolveStatsRange(now, StatsQuery{StartDate: "2026-06-21", EndDate: "2026-06-20"}); err != ErrInvalidDateRange {
		t.Fatalf("reversed date err = %v", err)
	}

	result := BuildStatsResult(Account{ID: 1, Name: "A", Platform: "openai", State: "active"}, []UsageLog{
		{Model: "b-model", AccountCost: 3, TotalCost: 3, ActualCost: 2, DurationMs: 10, CreatedAt: now.Add(-time.Hour)},
		{Model: "a-model", AccountCost: 2, TotalCost: 2, ActualCost: 1, DurationMs: 20, CreatedAt: now.Add(-time.Hour)},
		{Model: "gpt-image-1", AccountCost: 5, TotalCost: 5, ActualCost: 4, DurationMs: 30, CreatedAt: now},
	}, now, now.AddDate(0, 0, -1), now)
	if len(result.Models) != 3 || result.Models[0].Model != "a-model" {
		t.Fatalf("model tie sort = %+v", result.Models)
	}
	if result.Today.ImageCount != 1 || result.Today.ImageCost != 5 || result.Range.ImageCount != 1 {
		t.Fatalf("image stats = today=%+v range=%+v", result.Today, result.Range)
	}
	if !strings.HasPrefix(result.PeakCostDay.Date, "2026-06-20") || result.PeakCostDay.AccountCost != 10 {
		t.Fatalf("peak cost day = %+v", result.PeakCostDay)
	}
}
