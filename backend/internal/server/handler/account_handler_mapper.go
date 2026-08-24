package handler

import (
	"time"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func toAccountResp(account appaccount.Account) dto.AccountResp {
	resp := dto.AccountResp{
		ID:                      int64(account.ID),
		Name:                    account.Name,
		Email:                   account.Email,
		Platform:                account.Platform,
		Type:                    account.Type,
		Credentials:             account.Credentials,
		ModelPolicy:             account.ModelPolicy,
		State:                   account.State,
		Priority:                account.Priority,
		MaxConcurrency:          account.MaxConcurrency,
		CurrentConcurrency:      account.CurrentConcurrency,
		RateMultiplier:          account.RateMultiplier,
		ModelDowngradeThreshold: account.ModelDowngradeThreshold,
		ErrorMsg:                account.ErrorMsg,
		UpstreamIsPool:          account.UpstreamIsPool,
		Usage5hGrowthDate:       account.UsageEstimateMeta.FiveHour.GrowthDate,
		Usage5hDailyGrowth:      account.UsageEstimateMeta.FiveHour.DailyGrowth,
		Usage7dGrowthDate:       account.UsageEstimateMeta.SevenDay.GrowthDate,
		Usage7dDailyGrowth:      account.UsageEstimateMeta.SevenDay.DailyGrowth,
		Extra:                   account.Extra,
		GroupIDs:                account.GroupIDs,
		TimeMixin: dto.TimeMixin{
			CreatedAt: account.CreatedAt,
			UpdatedAt: account.UpdatedAt,
		},
	}
	if resp.GroupIDs == nil {
		resp.GroupIDs = []int64{}
	}

	if account.LastUsedAt != nil {
		lastUsedAt := account.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.LastUsedAt = &lastUsedAt
	}
	if account.LastProbeAt != nil {
		lastProbeAt := account.LastProbeAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.LastProbeAt = &lastProbeAt
	}
	if account.UsageEstimateMeta.FiveHour.ObservedAt != nil {
		observedAt := account.UsageEstimateMeta.FiveHour.ObservedAt.UTC().Format(time.RFC3339)
		resp.Usage5hObservedAt = &observedAt
	}
	if account.UsageEstimateMeta.SevenDay.ObservedAt != nil {
		observedAt := account.UsageEstimateMeta.SevenDay.ObservedAt.UTC().Format(time.RFC3339)
		resp.Usage7dObservedAt = &observedAt
	}
	if account.DeletedAt != nil {
		deletedAt := account.DeletedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.DeletedAt = &deletedAt
	}
	if account.StateUntil != nil {
		until := account.StateUntil.UTC().Format("2006-01-02T15:04:05Z")
		resp.StateUntil = &until
	}
	if account.Proxy != nil {
		proxyID := int64(account.Proxy.ID)
		resp.ProxyID = &proxyID
	}
	// 生图计数仅 OpenAI 平台在列表路径上填充；其它平台 / 详情路径 ImageStats=nil → 字段缺省。
	if account.ImageStats != nil {
		today := account.ImageStats.TodayCount
		total := account.ImageStats.TotalCount
		resp.TodayImageCount = &today
		resp.TotalImageCount = &total
	}

	return resp
}

func toAccountExportItem(account appaccount.Account) dto.AccountExportItem {
	return dto.AccountExportItem{
		Name:                    account.Name,
		Email:                   account.Email,
		Platform:                account.Platform,
		Type:                    account.Type,
		Credentials:             account.Credentials,
		ModelPolicy:             account.ModelPolicy,
		Priority:                account.Priority,
		MaxConcurrency:          account.MaxConcurrency,
		RateMultiplier:          dto.NewOptionalFloat(account.RateMultiplier),
		ModelDowngradeThreshold: account.ModelDowngradeThreshold,
	}
}

func toCredentialSchemaResp(schema appaccount.CredentialSchema) dto.CredentialSchemaResp {
	resp := dto.CredentialSchemaResp{
		Fields:       make([]dto.CredentialFieldResp, 0, len(schema.Fields)),
		AccountTypes: make([]dto.AccountTypeResp, 0, len(schema.AccountTypes)),
	}

	for _, field := range schema.Fields {
		resp.Fields = append(resp.Fields, dto.CredentialFieldResp{
			Key:          field.Key,
			Label:        field.Label,
			Type:         field.Type,
			Required:     field.Required,
			Placeholder:  field.Placeholder,
			EditDisabled: field.EditDisabled,
		})
	}

	for _, accountType := range schema.AccountTypes {
		item := dto.AccountTypeResp{
			Key:         accountType.Key,
			Label:       accountType.Label,
			Description: accountType.Description,
		}
		for _, field := range accountType.Fields {
			item.Fields = append(item.Fields, dto.CredentialFieldResp{
				Key:          field.Key,
				Label:        field.Label,
				Type:         field.Type,
				Required:     field.Required,
				Placeholder:  field.Placeholder,
				EditDisabled: field.EditDisabled,
			})
		}
		resp.AccountTypes = append(resp.AccountTypes, item)
	}

	return resp
}
