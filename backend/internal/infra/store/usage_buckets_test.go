package store

import (
	"reflect"
	"testing"
	"time"

	appusage "github.com/DevilGenius/airgate-core/internal/app/usage"
)

func TestUsageBucketsAggregateBeforeLoading(t *testing.T) {
	db := enttestOpen(t)
	defer db.Close()
	user := createTestUser(t, db, "buckets@example.com")
	account := db.Account.Create().SetName("bucket-account").SetPlatform("openai").SetType("apikey").SetCredentials(map[string]string{}).SaveX(t.Context())
	created := time.Date(2026, 6, 20, 1, 15, 0, 0, time.UTC)
	for i := 0; i < 100; i++ {
		db.UsageLog.Create().SetBillingEventID(time.Unix(int64(i), 0).String()).SetUserID(user.ID).SetAccountID(account.ID).SetPlatform("openai").SetModel("gpt-5").SetCreatedAt(created.Add(time.Duration(i) * time.Second)).SetInputTokens(2).SaveX(t.Context())
	}
	entries, err := NewUsageStore(db).TrendEntries(t.Context(), appusage.TrendFilter{StatsFilter: appusage.StatsFilter{StartDate: "2026-06-20", EndDate: "2026-06-20", TZ: "UTC"}, Granularity: "hour"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].InputTokens != 200 || entries[0].CreatedAt == "" {
		t.Fatalf("entries = %+v", entries)
	}
	groups, err := NewAccountStore(db).FindUsageLogs(t.Context(), account.ID, created, created.Add(99*time.Second))
	if err != nil || len(groups) != 1 || groups[0].Count != 100 || groups[0].InputTokens != 200 {
		t.Fatalf("inclusive account aggregate = %+v/%v", groups, err)
	}
}

func TestUsageBucketsMatchLocalBoundariesAndDST(t *testing.T) {
	db := enttestOpen(t)
	defer db.Close()
	user := createTestUser(t, db, "bucket-zones@example.com")
	var raw []appusage.TrendEntry
	for i, timestamp := range []string{"2026-10-31T18:20:00Z", "2026-10-31T18:35:00Z", "2026-11-01T03:59:00Z", "2026-11-01T04:01:00Z", "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z", "2026-11-01T07:01:00Z", "2026-11-02T05:00:00Z"} {
		created, _ := time.Parse(time.RFC3339, timestamp)
		db.UsageLog.Create().SetBillingEventID(timestamp).SetUserID(user.ID).SetPlatform("openai").SetModel("gpt-5").SetCreatedAt(created).SetInputTokens(i + 1).SaveX(t.Context())
		raw = append(raw, appusage.TrendEntry{CreatedAt: timestamp, InputTokens: int64(i + 1)})
	}
	for _, zone := range []string{"UTC", "Asia/Kolkata", "America/New_York"} {
		for _, granularity := range []string{"day", "hour"} {
			t.Run(zone+"/"+granularity, func(t *testing.T) {
				loc, err := time.LoadLocation(zone)
				if err != nil {
					t.Fatal(err)
				}
				start := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
				end := start.AddDate(0, 0, 1)
				var expectedRows []appusage.TrendEntry
				for _, row := range raw {
					at, _ := time.Parse(time.RFC3339, row.CreatedAt)
					if !at.Before(start) && at.Before(end) {
						expectedRows = append(expectedRows, row)
					}
				}
				filter := appusage.TrendFilter{StatsFilter: appusage.StatsFilter{StartDate: "2026-11-01", EndDate: "2026-11-01", TZ: zone}, Granularity: granularity}
				entries, err := NewUsageStore(db).TrendEntries(t.Context(), filter)
				if err != nil {
					t.Fatal(err)
				}
				got, want := appusage.BuildTrendBuckets(entries, granularity, zone), appusage.BuildTrendBuckets(expectedRows, granularity, zone)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("buckets = %+v, want %+v", got, want)
				}
			})
		}
	}
}
