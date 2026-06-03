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

func TodayStatsKey(day string, accountID int) string {
	return fmt.Sprintf("ag:account:today:%s:%d", day, accountID)
}

func ImageTotalKey(accountID int) string {
	return fmt.Sprintf("ag:account:image:%d", accountID)
}

func ImageTodayKey(day string, accountID int) string {
	return fmt.Sprintf("ag:account:image:%s:%d", day, accountID)
}

func PlatformKey(platform string) string {
	return fmt.Sprintf("ag:account:platform:%s", platform)
}
