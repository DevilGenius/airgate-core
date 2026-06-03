package accountcache

import (
	"fmt"
	"time"
)

const (
	TodayStatsTTL = 48 * time.Hour
	ImageTotalTTL = 30 * 24 * time.Hour
	ProfileTTL    = 24 * time.Hour
)

func Day(t time.Time) string {
	return t.In(time.Local).Format("20060102")
}

func ProfileKey(accountID int) string {
	return fmt.Sprintf("ag:account:profile:%d", accountID)
}

func UsageKey(accountID int) string {
	return fmt.Sprintf("ag:account:usage:%d", accountID)
}

func UsagePattern() string {
	return "ag:account:usage:*"
}

func TodayStatsKey(day string) string {
	return fmt.Sprintf("ag:account:stats:today:%s", day)
}

func TodayStatsField(accountID int, field string) string {
	return fmt.Sprintf("%d:%s", accountID, field)
}

func TodayStatsFields(accountID int) []string {
	return []string{
		TodayStatsField(accountID, "requests"),
		TodayStatsField(accountID, "tokens"),
		TodayStatsField(accountID, "account_cost"),
		TodayStatsField(accountID, "user_cost"),
		TodayStatsField(accountID, "updated_at"),
	}
}

func ImageTotalKey(accountID int) string {
	return fmt.Sprintf("ag:account:image:total:%d", accountID)
}

func ImageTodayKey(day string, accountID int) string {
	return fmt.Sprintf("ag:account:image:today:%s:%d", day, accountID)
}

func PlatformKey(platform string) string {
	return fmt.Sprintf("ag:account:platform:%s", platform)
}
