package account

import (
	"testing"
	"time"
)

func TestNormalizeAccountUsageWindowContractFields(t *testing.T) {
	cases := []struct {
		name        string
		input       AccountUsageWindow
		wantSlot    string
		wantGroup   string
		wantDisplay string
	}{
		{
			name:        "openai model window",
			input:       AccountUsageWindow{Key: "model:5h:gpt-5.3-codex-spark", Label: "5h gpt-5.3-codex-spark"},
			wantSlot:    "5h",
			wantGroup:   "model:gpt-5.3-codex-spark",
			wantDisplay: "5h",
		},
		{
			name:        "model suffix window",
			input:       AccountUsageWindow{Key: "model:gpt-5.3-codex-spark:7d"},
			wantSlot:    "7d",
			wantGroup:   "model:gpt-5.3-codex-spark",
			wantDisplay: "7d",
		},
		{
			name:        "claude sonnet window",
			input:       AccountUsageWindow{Key: "7d_sonnet", Label: "7d Sonnet"},
			wantSlot:    "7d",
			wantGroup:   "model:sonnet",
			wantDisplay: "7d",
		},
		{
			name:        "kiro monthly credits window",
			input:       AccountUsageWindow{Key: "monthly", Label: "Cr 12/100", ResetAfterSeconds: 60},
			wantSlot:    "monthly",
			wantGroup:   "base",
			wantDisplay: "Cr",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeAccountUsageWindow(tc.input)
			if !ok {
				t.Fatalf("normalizeAccountUsageWindow rejected %+v", tc.input)
			}
			if got.Slot != tc.wantSlot || got.Group != tc.wantGroup || got.DisplayLabel != tc.wantDisplay {
				t.Fatalf("normalized = %+v, want slot=%q group=%q display=%q", got, tc.wantSlot, tc.wantGroup, tc.wantDisplay)
			}
			if tc.input.ResetAfterSeconds > 0 && got.ResetSeconds != tc.input.ResetAfterSeconds {
				t.Fatalf("ResetSeconds = %d, want %d", got.ResetSeconds, tc.input.ResetAfterSeconds)
			}
		})
	}
}

func TestUsageCacheExpiresAtKeepsWindowsUntilTheirOwnReset(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	info := AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "5h", ResetSeconds: int64((3 * time.Hour).Seconds())},
			{Key: "7d", ResetSeconds: int64((7 * 24 * time.Hour).Seconds())},
		},
	}
	if got, want := accountUsageInfoExpiresAt(info, now), now.Add(7*24*time.Hour); !got.Equal(want) {
		t.Fatalf("expiresAt = %s, want %s", got, want)
	}

	info = AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "7d", ResetSeconds: int64((7 * 24 * time.Hour).Seconds())},
		},
	}
	if got, want := accountUsageInfoExpiresAt(info, now), now.Add(7*24*time.Hour); !got.Equal(want) {
		t.Fatalf("expiresAt = %s, want %s", got, want)
	}
}

func TestLiveAccountUsageInfoDropsExpiredWindowAndKeepsLiveWindow(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	now := fetchedAt.Add(6 * time.Hour)
	info := accountUsageInfoWithAbsoluteResets(AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "5h", Label: "5h", ResetSeconds: int64((5 * time.Hour).Seconds()), UsedPercent: 10},
			{Key: "7d", Label: "7d", ResetSeconds: int64((7 * 24 * time.Hour).Seconds()), UsedPercent: 20},
		},
	}, fetchedAt)

	live := liveAccountUsageInfo(info, now, fetchedAt.Add(usageCacheMaxTTL))
	if len(live.Windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1: %+v", len(live.Windows), live.Windows)
	}
	if got := live.Windows[0]; got.Key != "7d" || got.UsedPercent != 20 || got.ResetSeconds <= 0 {
		t.Fatalf("live window = %+v, want unexpired 7d window", got)
	}
}

func TestAccountUsageCachePayloadDoesNotSlideRelativeResetSeconds(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	now := fetchedAt.Add(time.Hour)
	payload := newAccountUsageCachePayload(AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "5h", Label: "5h", ResetSeconds: int64((5 * time.Hour).Seconds()), UsedPercent: 10},
		},
	}, fetchedAt)

	info, ok := payload.cacheInfo(now)
	if !ok || len(info.Windows) != 1 {
		t.Fatalf("cacheInfo ok=%v info=%+v, want one live window", ok, info)
	}
	got := info.Windows[0].ResetSeconds
	want := int64((4 * time.Hour).Seconds())
	if got != want {
		t.Fatalf("ResetSeconds = %d, want %d", got, want)
	}
}

func TestAccountUsageCachePayloadNormalizesLegacyWindows(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	now := fetchedAt.Add(time.Hour)
	payload := newAccountUsageCachePayload(AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "7d_sonnet", Label: "7d Sonnet", ResetSeconds: int64((7 * 24 * time.Hour).Seconds()), UsedPercent: 20},
			{Label: "5h", ResetSeconds: int64((5 * time.Hour).Seconds()), UsedPercent: 10},
		},
	}, fetchedAt)

	info, ok := payload.cacheInfo(now)
	if !ok || len(info.Windows) != 2 {
		t.Fatalf("cacheInfo ok=%v windows=%+v, want two live windows", ok, info.Windows)
	}
	if got := info.Windows[0]; got.Slot != "5h" || got.Group != "base" || got.Key != "5h" {
		t.Fatalf("first window = %+v, want normalized base 5h", got)
	}
	if got := info.Windows[1]; got.Slot != "7d" || got.Group != "model:sonnet" || got.DisplayLabel != "7d" {
		t.Fatalf("second window = %+v, want normalized 7d sonnet", got)
	}
}

func TestMergeAccountUsageInfoPreservesLiveMissingWindows(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	existing := AccountUsageInfo{
		UpdatedAt: "2026-05-20T11:55:00Z",
		Windows: []AccountUsageWindow{
			{
				Key:          "5h",
				Label:        "5h",
				DisplayLabel: "5h",
				Slot:         "5h",
				Group:        "base",
				UsedPercent:  31,
				ResetAt:      now.Add(2 * time.Hour).Format(time.RFC3339),
			},
			{
				Key:          "7d",
				Label:        "7d",
				DisplayLabel: "7d",
				Slot:         "7d",
				Group:        "base",
				UsedPercent:  44,
				ResetAt:      now.Add(48 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	incoming := AccountUsageInfo{
		UpdatedAt: "2026-05-20T12:00:00Z",
		Windows: []AccountUsageWindow{
			{
				Key:          "7d",
				Label:        "7d",
				DisplayLabel: "7d",
				Slot:         "7d",
				Group:        "base",
				UsedPercent:  55,
			},
		},
	}

	merged := mergeAccountUsageInfo(existing, incoming, now)
	if len(merged.Windows) != 2 {
		t.Fatalf("len(windows) = %d, want 2: %+v", len(merged.Windows), merged.Windows)
	}
	if got := merged.Windows[0]; got.Key != "5h" || got.UsedPercent != 31 || got.ResetSeconds <= 0 {
		t.Fatalf("preserved 5h window = %+v, want live cached 5h sorted first", got)
	}
	if got := merged.Windows[1]; got.Key != "7d" || got.UsedPercent != 55 || got.ResetSeconds <= 0 {
		t.Fatalf("merged 7d window = %+v, want incoming usage with preserved reset", got)
	}
}

func TestMergeAccountUsageInfoMatchesLegacyAndCanonicalWindowIdentity(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	existing := AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{
				Key:         "7d_sonnet",
				Label:       "7d Sonnet",
				UsedPercent: 10,
				ResetAt:     now.Add(48 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	incoming := AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{
				Key:         "model:sonnet:7d",
				Label:       "7d Sonnet",
				UsedPercent: 30,
			},
		},
	}

	merged := mergeAccountUsageInfo(existing, incoming, now)
	if len(merged.Windows) != 1 {
		t.Fatalf("len(windows) = %d, want 1: %+v", len(merged.Windows), merged.Windows)
	}
	got := merged.Windows[0]
	if got.UsedPercent != 30 || got.ResetSeconds <= 0 || got.Group != "model:sonnet" || got.Slot != "7d" {
		t.Fatalf("merged window = %+v, want canonical incoming usage with preserved reset", got)
	}
}

func TestMergeAccountUsageInfoDropsExpiredMissingWindows(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	existing := AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{
				Key:         "5h",
				Label:       "5h",
				UsedPercent: 31,
				ResetAt:     now.Add(-time.Minute).Format(time.RFC3339),
			},
		},
	}
	incoming := AccountUsageInfo{Windows: []AccountUsageWindow{{Key: "7d", Label: "7d", UsedPercent: 55}}}

	merged := mergeAccountUsageInfo(existing, incoming, now)
	if len(merged.Windows) != 1 || merged.Windows[0].Key != "7d" {
		t.Fatalf("windows = %+v, want only incoming 7d", merged.Windows)
	}
}

func TestMergeAccountUsageInfoStableSortByDuration(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	existing := AccountUsageInfo{}
	incoming := AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "monthly", Slot: "monthly", Label: "monthly", UsedPercent: 11, ResetAt: now.Add(15 * 24 * time.Hour).Format(time.RFC3339)},
			{Key: "7d", Slot: "7d", Label: "7d", UsedPercent: 22, ResetAt: now.Add(48 * time.Hour).Format(time.RFC3339)},
			{Key: "5h", Slot: "5h", Label: "5h", UsedPercent: 33, ResetAt: now.Add(2 * time.Hour).Format(time.RFC3339)},
		},
	}

	merged := mergeAccountUsageInfo(existing, incoming, now)
	got := []string{merged.Windows[0].Slot, merged.Windows[1].Slot, merged.Windows[2].Slot}
	want := []string{"5h", "7d", "monthly"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("window[%d] slot = %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}
}

func TestMergeAccountUsageInfoStableSortUsesSortOrderFirst(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	incoming := AccountUsageInfo{
		Windows: []AccountUsageWindow{
			{Key: "5h", Slot: "5h", Label: "5h", UsedPercent: 33, SortOrder: 20},
			{Key: "monthly", Slot: "monthly", Label: "monthly", UsedPercent: 11, SortOrder: 10},
			{Key: "7d", Slot: "7d", Label: "7d", UsedPercent: 22},
		},
	}

	merged := mergeAccountUsageInfo(AccountUsageInfo{}, incoming, now)
	got := []string{merged.Windows[0].Slot, merged.Windows[1].Slot, merged.Windows[2].Slot}
	want := []string{"monthly", "5h", "7d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("window[%d] slot = %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}
}

func TestMergeAccountUsageWindowCarriesMissingFieldsAndReset(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	existing := AccountUsageWindow{
		Key:          "5h",
		Label:        "5h",
		DisplayLabel: "5h window",
		Slot:         "5h",
		Group:        "base",
		UpdatedAt:    "2026-05-20T11:55:00Z",
		UsedPercent:  10,
		ResetAt:      now.Add(2 * time.Hour).Format(time.RFC3339),
	}
	incoming := AccountUsageWindow{Key: "5h", UsedPercent: 42}

	merged := mergeAccountUsageWindow(existing, incoming, now)
	if merged.Label != "5h" || merged.DisplayLabel != "5h window" || merged.Slot != "5h" || merged.Group != "base" {
		t.Fatalf("label-family not carried: %+v", merged)
	}
	if merged.UpdatedAt != existing.UpdatedAt {
		t.Fatalf("UpdatedAt = %q, want carry-over %q", merged.UpdatedAt, existing.UpdatedAt)
	}
	if merged.ResetSeconds <= 0 || merged.ResetAfterSeconds <= 0 || merged.ResetAt == "" {
		t.Fatalf("expected reset fields to be filled from existing window: %+v", merged)
	}
}

func TestMergeAccountUsageWindowKeepsIncomingResetWhenProvided(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	existing := AccountUsageWindow{Key: "5h", ResetAt: now.Add(10 * time.Hour).Format(time.RFC3339)}
	incoming := AccountUsageWindow{Key: "5h", ResetAfterSeconds: 60}

	merged := mergeAccountUsageWindow(existing, incoming, now)
	if merged.ResetAfterSeconds != 60 || merged.ResetAt != "" {
		t.Fatalf("expected incoming reset to win: %+v", merged)
	}
}

func TestAccountUsageWindowIdentityFallbacks(t *testing.T) {
	cases := []struct {
		name   string
		window AccountUsageWindow
		want   string
	}{
		{"group+slot wins over key", AccountUsageWindow{Key: " primary ", Group: "base", Slot: "5h", Label: "5h"}, "base:5h"},
		{"group+slot+display", AccountUsageWindow{Group: "base", Slot: "5h", DisplayLabel: "5h window"}, "base:5h"},
		{"group+slot+label fallback", AccountUsageWindow{Group: "model:opus", Slot: "7d", Label: "7d Opus"}, "model:opus:7d"},
		{"key fallback", AccountUsageWindow{Key: " primary ", Label: "5h"}, "primary"},
		{"label-only", AccountUsageWindow{Label: " 5h "}, "5h"},
		{"empty", AccountUsageWindow{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := accountUsageWindowIdentity(tc.window); got != tc.want {
				t.Fatalf("identity = %q, want %q", got, tc.want)
			}
		})
	}
}
