package handler

import (
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func toAPIKeyResp(item appapikey.Key) dto.APIKeyResp {
	resp := dto.APIKeyResp{
		ID:                    int64(item.ID),
		Name:                  item.Name,
		Key:                   item.PlainKey,
		KeyPrefix:             appapikey.DisplayKeyPrefix(item),
		UserID:                int64(item.UserID),
		GroupRate:             item.GroupRate,
		IPWhitelist:           item.IPWhitelist,
		IPBlacklist:           item.IPBlacklist,
		QuotaUSD:              item.QuotaUSD,
		UsedQuota:             item.UsedQuota,
		UsedQuotaActual:       item.UsedQuotaActual,
		SellRate:              item.SellRate,
		MaxConcurrency:        item.MaxConcurrency,
		BalanceAlertEnabled:   item.BalanceAlertEnabled,
		BalanceAlertEmail:     item.BalanceAlertEmail,
		BalanceAlertThreshold: item.BalanceAlertThreshold,
		TodayCost:             item.TodayCost,
		TodayActualCost:       item.TodayActualCost,
		ThirtyDayCost:         item.ThirtyDayCost,
		ThirtyDayActualCost:   item.ThirtyDayActualCost,
		Status:                item.Status,
		TimeMixin: dto.TimeMixin{
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		},
	}
	if item.GroupID != nil {
		groupID := int64(*item.GroupID)
		resp.GroupID = &groupID
	}
	if item.ExpiresAt != nil {
		expiresAt := item.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		resp.ExpiresAt = &expiresAt
	}
	return resp
}
