package handler

import (
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func TestParseAccountStateFiltersNormalizesAndDeduplicates(t *testing.T) {
	states := parseAccountStateFilters(" working,active,working, family_limited ")
	if len(states) != 3 || states[0] != "working" || states[1] != "active" || states[2] != "family_limited" {
		t.Fatalf("states = %#v", states)
	}
}

func TestAccountMatchesOverlappingStateFilters(t *testing.T) {
	active := appaccount.Account{ID: 1, State: "active"}
	disabled := appaccount.Account{ID: 2, State: "disabled"}
	cooldowns := map[int][]dto.FamilyCooldownDTO{
		1: {{Family: "gpt-image"}},
	}
	working := map[int]struct{}{2: {}}

	tests := []struct {
		name      string
		item      appaccount.Account
		states    []string
		working   map[int]struct{}
		cooldowns map[int][]dto.FamilyCooldownDTO
		want      bool
	}{
		{name: "active includes account without cooldown", item: active, states: []string{"active"}, want: true},
		{name: "active excludes family limited", item: active, states: []string{"active"}, cooldowns: cooldowns, want: false},
		{name: "family limited excludes account without cooldown", item: active, states: []string{familyLimitedStateFilter}, want: false},
		{name: "family limited includes active cooldown", item: active, states: []string{familyLimitedStateFilter}, cooldowns: cooldowns, want: true},
		{name: "active plus family includes all active", item: active, states: []string{"active", familyLimitedStateFilter}, cooldowns: cooldowns, want: true},
		{name: "working overlaps disabled", item: disabled, states: []string{workingStateFilter, "active"}, working: working, want: true},
		{name: "disabled without working does not match active", item: disabled, states: []string{workingStateFilter, "active"}, want: false},
		{name: "disabled exact state", item: disabled, states: []string{"disabled", "degraded"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := accountMatchesStateFilters(
				tt.item,
				accountStateFilterSet(tt.states),
				tt.working,
				tt.cooldowns,
			)
			if got != tt.want {
				t.Fatalf("accountMatchesStateFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}
