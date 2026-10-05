package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/account"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestZeroWeightDoesNotBlockNewAssignmentLayers(t *testing.T) {
	for _, scenario := range []string{"priority", "negative reserve", "model quality", "sticky only fallback", "type preference"} {
		t.Run(scenario, func(t *testing.T) {
			s := newSelectionTestScheduler(Normal)
			zero, positive := newSelectionTestAccount(1), newSelectionTestAccount(2)
			zero.SchedulingWeight, zero.Priority = 0, 100
			opts := AccountSelectionOptions{}
			switch scenario {
			case "negative reserve":
				positive.Priority = -10
			case "model quality":
				positive.ModelDowngradeThreshold = 0.9
				s.modelSuccessRate = NewModelSuccessRateTracker(nil)
				for range modelSuccessRateMinBucketValidRequests {
					s.modelSuccessRate.Record(positive.ID, "gpt-5", sdk.ForwardOutcome{Kind: sdk.OutcomeUpstreamTransient})
				}
			case "sticky only fallback":
				s.currentLoad = func(_ context.Context, id int) int {
					if id == positive.ID {
						return 8
					}
					return 0
				}
			case "type preference":
				zero.Type, positive.Type = "oauth", "apikey"
				opts.PreferDifferentAccountType = "apikey"
			}
			seedSelectionTestGroup(t, 7, "openai", []*ent.Account{zero, positive}, nil)
			for _, session := range []string{"", "new-session", "stale-session"} {
				if session == "stale-session" {
					s.sticky.Set(t.Context(), 1, "openai", session, 999)
				}
				got, err := s.SelectAccountWithOptions(t.Context(), "openai", "gpt-5", 1, 7, session, opts)
				if err != nil || got == nil || got.ID != positive.ID {
					t.Fatalf("session %q: got %+v, err %v", session, got, err)
				}
			}
		})
	}
}

func TestZeroWeightAffinityStillUsesOriginalEligibility(t *testing.T) {
	for _, mode := range []string{"soft session", "hard session", "soft response", "hard response"} {
		t.Run(mode, func(t *testing.T) {
			s := newSelectionTestScheduler(Normal)
			bound := newSelectionTestAccount(1)
			bound.SchedulingWeight = 0
			seedSelectionTestGroup(t, 7, "openai", []*ent.Account{bound}, nil)
			opts := AccountSelectionOptions{APIKeyID: 11}
			if mode == "hard session" || mode == "hard response" {
				opts.RequireContinuationAffinity = true
			}
			if mode == "soft response" || mode == "hard response" {
				opts.PreviousResponseID = "response"
				s.responseAffinity.Bind(t.Context(), 7, "openai", "response", bound.ID, 1, 11)
			} else {
				s.sticky.Set(t.Context(), 1, "openai", "session", bound.ID)
			}
			got, err := s.SelectAccountWithOptions(t.Context(), "openai", "gpt-5", 1, 7, "session", opts)
			if err != nil || got == nil || got.ID != bound.ID {
				t.Fatalf("got %+v, err %v", got, err)
			}
			s.currentLoad = func(context.Context, int) int { return bound.MaxConcurrency }
			if got, err := s.SelectAccountWithOptions(t.Context(), "openai", "gpt-5", 1, 7, "session", opts); got != nil || err == nil {
				t.Fatalf("full bound account accepted: %+v, %v", got, err)
			}
		})
	}
}

func TestZeroWeightSoftStickyDoesNotBypassPriorityOrDisabledState(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		s := newSelectionTestScheduler(Normal)
		bound, other := newSelectionTestAccount(1), newSelectionTestAccount(2)
		bound.SchedulingWeight = 0
		other.Priority = 10
		if disabled {
			bound.State, bound.Priority = account.StateDisabled, 100
		}
		seedSelectionTestGroup(t, 7, "openai", []*ent.Account{bound, other}, nil)
		s.sticky.Set(t.Context(), 1, "openai", "session", bound.ID)
		got, err := s.SelectAccount(t.Context(), "openai", "gpt-5", 1, 7, "session")
		if err != nil || got == nil || got.ID != other.ID {
			t.Fatalf("disabled=%v: %+v, %v", disabled, got, err)
		}
	}
}

func TestAllZeroWeightsHaveNoNewAssignment(t *testing.T) {
	for _, full := range []bool{false, true} {
		s := newSelectionTestScheduler(Normal)
		pool := []*ent.Account{newSelectionTestAccount(1), newSelectionTestAccount(2)}
		for _, acc := range pool {
			acc.SchedulingWeight = 0
		}
		if full {
			s.currentLoad = func(context.Context, int) int { return DefaultAccountMaxConcurrency }
		}
		seedSelectionTestGroup(t, 7, "openai", pool, nil)
		if got := s.selectByWeight(t.Context(), pool, nil); got != nil {
			t.Fatalf("zero weight selected: %+v", got)
		}
		got, err := s.SelectAccount(t.Context(), "openai", "gpt-5", 1, 7, "new-session")
		if got != nil || !errors.Is(err, ErrNoAvailableAccount) {
			t.Fatalf("full=%v: %+v, %v", full, got, err)
		}
		if _, found := s.sticky.Get(t.Context(), 1, "openai", "new-session"); found {
			t.Fatal("created a zero-weight binding")
		}
	}
}
