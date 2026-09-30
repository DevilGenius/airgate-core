package routegraph

import (
	"github.com/DevilGenius/airgate-core/internal/plantype"
	"sync"
	"time"
)

var platformPlanFilters sync.Map // platform -> immutable []plantype.Filter
var defaultPlanFilters = plantype.ParseFilters("")

func accountPlanFilters(platform string) []plantype.Filter {
	if filters, ok := platformPlanFilters.Load(platform); ok {
		return filters.([]plantype.Filter)
	}
	return defaultPlanFilters
}

// SetPlatformPlanMetadata runs at plugin publication/removal, not per request.
// Reclassify cached account nodes so hot updates take effect immediately.
func SetPlatformPlanMetadata(platform, raw string) {
	if platform == "" {
		return
	}
	filters := plantype.ParseFilters(raw)
	updateMu.Lock()
	defer updateMu.Unlock()
	platformPlanFilters.Store(platform, filters)
	base := Current()
	if base == nil {
		return
	}
	next := cloneSnapshot(base)
	for _, group := range base.groupsByPlatform[platform] {
		accounts := make([]*AccountNode, 0, len(group.Accounts))
		for _, account := range group.Accounts {
			accounts = append(accounts, buildAccountNode(account.Account))
		}
		putGroupNode(next, withAccountNodes(group, accounts))
	}
	next.refreshedAt = time.Now()
	snapshotValue.Store(next)
}
