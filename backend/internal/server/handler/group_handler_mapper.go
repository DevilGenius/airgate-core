package handler

import (
	appgroup "github.com/DevilGenius/airgate-core/internal/app/group"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func toGroupRespFromDomain(item appgroup.Group) dto.GroupResp {
	return dto.GroupResp{
		ID:                       int64(item.ID),
		Name:                     item.Name,
		Platform:                 item.Platform,
		RateMultiplier:           item.RateMultiplier,
		IsExclusive:              item.IsExclusive,
		StatusVisible:            item.StatusVisible,
		SubscriptionType:         item.SubscriptionType,
		Quotas:                   item.Quotas,
		ModelRouting:             item.ModelRouting,
		ModelPolicy:              item.ModelPolicy,
		AccountTypeModelPolicies: item.AccountTypeModelPolicies,
		DispatchDSL:              item.DispatchDSL,
		OperationPolicies:        item.OperationPolicies,
		PluginSettings:           item.PluginSettings,
		ServiceTier:              item.ServiceTier,
		ForceInstructions:        item.ForceInstructions,
		Note:                     item.Note,
		SortWeight:               item.SortWeight,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
}

func toGroupStatsResp(stats appgroup.GroupStats) dto.GroupStatsResp {
	return dto.GroupStatsResp{
		AccountActive:   stats.AccountActive,
		AccountError:    stats.AccountError,
		AccountDisabled: stats.AccountDisabled,
		AccountTotal:    stats.AccountTotal,
		CapacityUsed:    stats.CapacityUsed,
		CapacityTotal:   stats.CapacityTotal,
		TodayCost:       stats.TodayCost,
		TotalCost:       stats.TotalCost,
	}
}

func toGroupOverviewResp(item appgroup.Group, stats appgroup.GroupStats) dto.GroupOverviewResp {
	return dto.GroupOverviewResp{
		GroupResp:      toGroupRespFromDomain(item),
		GroupStatsResp: toGroupStatsResp(stats),
	}
}
