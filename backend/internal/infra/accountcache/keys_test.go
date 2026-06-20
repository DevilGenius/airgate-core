package accountcache

import (
	"reflect"
	"testing"
	"time"
)

func TestCacheTTLs(t *testing.T) {
	if TodayStatsTTL != 48*time.Hour {
		t.Fatalf("TodayStatsTTL = %v", TodayStatsTTL)
	}
	if ImageTotalTTL != 30*24*time.Hour {
		t.Fatalf("ImageTotalTTL = %v", ImageTotalTTL)
	}
	if ProfileTTL != 24*time.Hour {
		t.Fatalf("ProfileTTL = %v", ProfileTTL)
	}
}

func TestDayUsesLocalDate(t *testing.T) {
	ts := time.Date(2026, 6, 20, 12, 34, 56, 0, time.UTC)
	want := ts.In(time.Local).Format("20060102")
	if got := Day(ts); got != want {
		t.Fatalf("Day() = %q, want %q", got, want)
	}
}

func TestKeys(t *testing.T) {
	if got := ProfileKey(42); got != "ag:account:profile:42" {
		t.Fatalf("ProfileKey = %q", got)
	}
	if got := UsageKey(42); got != "ag:account:usage:42" {
		t.Fatalf("UsageKey = %q", got)
	}
	if got := UsagePattern(); got != "ag:account:usage:*" {
		t.Fatalf("UsagePattern = %q", got)
	}
	if got := TodayStatsKey("20260620"); got != "ag:account:stats:today:20260620" {
		t.Fatalf("TodayStatsKey = %q", got)
	}
	if got := TodayStatsField(42, "requests"); got != "42:requests" {
		t.Fatalf("TodayStatsField = %q", got)
	}
	wantFields := []string{"42:requests", "42:tokens", "42:account_cost", "42:user_cost", "42:updated_at"}
	if got := TodayStatsFields(42); !reflect.DeepEqual(got, wantFields) {
		t.Fatalf("TodayStatsFields = %#v, want %#v", got, wantFields)
	}
	if got := ImageTotalKey(42); got != "ag:account:image:total:42" {
		t.Fatalf("ImageTotalKey = %q", got)
	}
	if got := ImageTodayKey("20260620", 42); got != "ag:account:image:today:20260620:42" {
		t.Fatalf("ImageTodayKey = %q", got)
	}
	if got := PlatformKey("openai"); got != "ag:account:platform:openai" {
		t.Fatalf("PlatformKey = %q", got)
	}
}
