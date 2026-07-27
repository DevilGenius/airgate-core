package handler

import (
	"context"
	"strings"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

const (
	workingStateFilter       = "working"
	familyLimitedStateFilter = "family_limited"
)

func parseAccountStateFilters(raw string) []string {
	seen := make(map[string]struct{})
	states := make([]string, 0)
	for _, value := range strings.Split(raw, ",") {
		state := strings.ToLower(strings.TrimSpace(value))
		if state == "" {
			continue
		}
		if _, ok := seen[state]; ok {
			continue
		}
		seen[state] = struct{}{}
		states = append(states, state)
	}
	return states
}

func accountStateFilterSet(states []string) map[string]struct{} {
	set := make(map[string]struct{}, len(states))
	for _, state := range states {
		set[state] = struct{}{}
	}
	return set
}

func usesCompositeAccountStateFilter(states []string) bool {
	if len(states) > 1 {
		return true
	}
	if len(states) == 0 {
		return false
	}
	return states[0] == "active" || states[0] == familyLimitedStateFilter
}

func (h *AccountHandler) listAccountsWithStateFilters(
	ctx context.Context,
	filter appaccount.ListFilter,
) (appaccount.ListResult, map[int][]dto.FamilyCooldownDTO, error) {
	states := parseAccountStateFilters(filter.State)
	if !usesCompositeAccountStateFilter(states) {
		result, err := h.service.List(ctx, filter)
		if err != nil {
			return appaccount.ListResult{}, nil, err
		}
		return result, h.familyCooldownsForAccounts(ctx, accountIDs(result.List)), nil
	}

	page, pageSize := appaccount.NormalizePage(filter.Page, filter.PageSize)
	accounts, familyCooldowns, err := h.listAllAccountsWithStateFilters(ctx, filter, states)
	if err != nil {
		return appaccount.ListResult{}, nil, err
	}

	total := int64(len(accounts))
	start := int64(page-1) * int64(pageSize)
	if start >= total {
		return appaccount.ListResult{
			List:     []appaccount.Account{},
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		}, familyCooldowns, nil
	}
	end := start + int64(pageSize)
	if end > total {
		end = total
	}
	pageAccounts := append([]appaccount.Account(nil), accounts[int(start):int(end)]...)
	h.service.HydrateAccountListRuntimeData(ctx, pageAccounts)
	if familyCooldowns == nil {
		familyCooldowns = h.familyCooldownsForAccounts(ctx, accountIDs(pageAccounts))
	}

	return appaccount.ListResult{
		List:     pageAccounts,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, familyCooldowns, nil
}

func (h *AccountHandler) listAllAccountsWithStateFilters(
	ctx context.Context,
	filter appaccount.ListFilter,
	states []string,
) ([]appaccount.Account, map[int][]dto.FamilyCooldownDTO, error) {
	if !usesCompositeAccountStateFilter(states) {
		accounts, err := h.service.ListAll(ctx, filter)
		return accounts, nil, err
	}

	filter.State = ""
	accounts, err := h.service.ListAll(ctx, filter)
	if err != nil {
		return nil, nil, err
	}
	selected := accountStateFilterSet(states)
	_, needsActive := selected["active"]
	_, needsFamilyLimited := selected[familyLimitedStateFilter]
	needsFamilyCooldowns := needsActive || needsFamilyLimited
	var familyCooldowns map[int][]dto.FamilyCooldownDTO
	if needsFamilyCooldowns {
		familyCooldowns = h.familyCooldownsForAccounts(ctx, accountIDs(accounts))
	}
	workingIDs := make(map[int]struct{})
	if _, ok := selected[workingStateFilter]; ok {
		for _, accountID := range h.service.WorkingAccountIDs(ctx) {
			workingIDs[accountID] = struct{}{}
		}
	}

	filtered := make([]appaccount.Account, 0, len(accounts))
	for _, item := range accounts {
		if accountMatchesStateFilters(item, selected, workingIDs, familyCooldowns) {
			filtered = append(filtered, item)
		}
	}
	return filtered, familyCooldowns, nil
}

func accountMatchesStateFilters(
	item appaccount.Account,
	selected map[string]struct{},
	workingIDs map[int]struct{},
	familyCooldowns map[int][]dto.FamilyCooldownDTO,
) bool {
	if _, ok := selected[workingStateFilter]; ok {
		if _, working := workingIDs[item.ID]; working {
			return true
		}
	}

	if item.State == "active" {
		hasFamilyCooldown := len(familyCooldowns[item.ID]) > 0
		if _, ok := selected[familyLimitedStateFilter]; ok && hasFamilyCooldown {
			return true
		}
		if _, ok := selected["active"]; ok && !hasFamilyCooldown {
			return true
		}
		return false
	}

	_, ok := selected[item.State]
	return ok
}

func accountIDs(accounts []appaccount.Account) []int {
	ids := make([]int, 0, len(accounts))
	for _, item := range accounts {
		ids = append(ids, item.ID)
	}
	return ids
}
