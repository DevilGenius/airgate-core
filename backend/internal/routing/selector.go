package routing

import (
	"context"
	"log/slog"
	"sort"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/group"
	"github.com/DevilGenius/airgate-core/ent/user"
	"github.com/DevilGenius/airgate-core/internal/billing"
)

type Candidate struct {
	GroupID                int
	Platform               string
	EffectiveRate          float64
	GroupRateMultiplier    float64
	GroupServiceTier       string
	GroupForceInstructions string
	GroupPluginSettings    map[string]map[string]string
	SortWeight             int
}

func ListEligibleGroups(ctx context.Context, db *ent.Client, userID int, platform string, userGroupRates map[int64]float64, input GroupMatchInput) ([]Candidate, error) {
	groups, err := db.Group.Query().
		Where(group.PlatformEQ(platform)).
		All(ctx)
	if err != nil {
		slog.Error("routing_load_failed",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			sdk.LogFieldError, err)
		return nil, err
	}

	candidates := make([]Candidate, 0, len(groups))
	for _, g := range groups {
		if !GroupMatchesRequest(g, input).OK {
			continue
		}
		if g.IsExclusive {
			allowed, err := g.QueryAllowedUsers().Where(user.IDEQ(userID)).Exist(ctx)
			if err != nil {
				slog.Error("routing_load_failed",
					sdk.LogFieldPlatform, platform,
					sdk.LogFieldUserID, userID,
					sdk.LogFieldGroupID, g.ID,
					"stage", "exclusive_user_check",
					sdk.LogFieldError, err)
				return nil, err
			}
			if !allowed {
				continue
			}
		}
		candidates = append(candidates, Candidate{
			GroupID:                g.ID,
			Platform:               g.Platform,
			EffectiveRate:          billing.ResolveBillingRateForGroup(userGroupRates, g.ID, g.RateMultiplier),
			GroupRateMultiplier:    g.RateMultiplier,
			GroupServiceTier:       g.ServiceTier,
			GroupForceInstructions: g.ForceInstructions,
			GroupPluginSettings:    clonePluginSettings(g.PluginSettings),
			SortWeight:             g.SortWeight,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].EffectiveRate != candidates[j].EffectiveRate {
			return candidates[i].EffectiveRate < candidates[j].EffectiveRate
		}
		if candidates[i].SortWeight != candidates[j].SortWeight {
			return candidates[i].SortWeight > candidates[j].SortWeight
		}
		return candidates[i].GroupID < candidates[j].GroupID
	})

	if len(candidates) == 0 {
		slog.Warn("routing_no_match",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			"needs_image", input.NeedsImage,
			"groups_scanned", len(groups))
	} else {
		slog.Debug("routing_match",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			"candidate_count", len(candidates),
			"top_group_id", candidates[0].GroupID,
			"top_rate", candidates[0].EffectiveRate)
	}
	return candidates, nil
}

func clonePluginSettings(in map[string]map[string]string) map[string]map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(in))
	for plugin, settings := range in {
		if len(settings) == 0 {
			continue
		}
		out[plugin] = make(map[string]string, len(settings))
		for k, v := range settings {
			out[plugin][k] = v
		}
	}
	return out
}
