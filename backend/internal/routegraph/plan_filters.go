package routegraph

import (
	"sync"
	"time"

	"github.com/DevilGenius/airgate-core/internal/plantype"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

var platformPlanFilters sync.Map // platform -> immutable []plantype.Filter
var defaultPlanFilters = plantype.ResolveFilters(nil)

func accountPlanFilters(platform string) []plantype.Filter {
	if filters, ok := platformPlanFilters.Load(platform); ok {
		return filters.([]plantype.Filter)
	}
	return defaultPlanFilters
}

// SetPlatformAccountPlans runs at plugin publication/removal, not per request.
// Reclassify cached account nodes so hot updates take effect immediately.
func SetPlatformAccountPlans(platform string, plans []sdk.AccountPlan) {
	if platform == "" {
		return
	}
	filters := plantype.ResolveFilters(plans)
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
