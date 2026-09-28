package bootstrap

import (
	"strings"
	"testing"
)

func TestRequestTraceUpgradeRetainsPayloads(t *testing.T) {
	for _, upgrade := range loadSystemUpgrades() {
		if upgrade.ID != "20260928190000_persist_request_traces" {
			continue
		}
		sql := strings.ToLower(upgrade.SQL)
		if !strings.Contains(sql, "alter table if exists public.monitor_request_trace drop column if exists expires_at") {
			t.Fatal("trace expiration metadata not removed")
		}
		if strings.Contains(sql, "delete from") || strings.Contains(sql, "drop table") || strings.Contains(sql, "truncate ") {
			t.Fatal("upgrade deletes retained traces")
		}
		return
	}
	t.Fatal("trace persistence upgrade not embedded")
}
