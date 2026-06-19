package routing

import (
	"context"
	"log/slog"
	"net/http"
	"sort"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/dispatchresolver"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

type RequestInput struct {
	Method      string
	Path        string
	ClientModel string
}

type Candidate struct {
	GroupID                int
	Platform               string
	EffectiveRate          float64
	GroupRateMultiplier    float64
	GroupServiceTier       string
	GroupForceInstructions string
	GroupPluginSettings    map[string]map[string]string
	GroupOperationPolicies map[string]bool
	DispatchPlans          []sdk.DispatchPlan
	SortWeight             int
}

func ListEligibleGroups(ctx context.Context, _ *ent.Client, userID int, platform string, userGroupRates map[int64]float64, input RequestInput) ([]Candidate, error) {
	groups := routegraph.GroupsByPlatform(platform)

	candidates := make([]Candidate, 0, len(groups))
	for _, g := range groups {
		plans := dispatchresolver.ResolveDispatchPlans(
			platform,
			g.DispatchResolver,
			input.Method,
			input.Path,
			input.ClientModel,
		)
		requirements := RequirementsFromDispatchPlans(plans)
		entGroup := &ent.Group{
			ID:                g.ID,
			Platform:          g.Platform,
			OperationPolicies: g.OperationPolicies,
		}
		if !GroupMatchesRequirements(entGroup, requirements).OK {
			continue
		}
		if g.IsExclusive {
			if _, ok := g.AllowedUsers[userID]; !ok {
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
			GroupOperationPolicies: cloneOperationPolicies(g.OperationPolicies),
			DispatchPlans:          cloneDispatchPlans(plans),
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

	requiredOperation := RequirementsFromDispatchPlans(firstDispatchPlans(candidates)).RequiredOperation
	if len(candidates) == 0 {
		slog.Warn("routing_no_match",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			"required_operation", requiredOperation,
			"groups_scanned", len(groups))
	} else {
		slog.Debug("routing_match",
			sdk.LogFieldPlatform, platform,
			sdk.LogFieldUserID, userID,
			"candidate_count", len(candidates),
			"top_group_id", candidates[0].GroupID,
			"top_rate", candidates[0].EffectiveRate,
			"required_operation", requiredOperation)
	}
	return candidates, nil
}

func firstDispatchPlans(candidates []Candidate) []sdk.DispatchPlan {
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0].DispatchPlans
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

func cloneOperationPolicies(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneDispatchPlans(in []sdk.DispatchPlan) []sdk.DispatchPlan {
	if len(in) == 0 {
		return nil
	}
	return append([]sdk.DispatchPlan(nil), in...)
}

func DefaultDenyGate(requiredOperation, code, message string) sdk.DispatchGate {
	return sdk.DispatchGate{
		RequiredOperation: requiredOperation,
		Status:            http.StatusForbidden,
		ErrorType:         "invalid_request_error",
		Code:              code,
		Message:           message,
	}
}
