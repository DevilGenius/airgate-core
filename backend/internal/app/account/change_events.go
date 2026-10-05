package account

import (
	"context"
	"time"

	"github.com/DevilGenius/airgate-core/internal/adminevents"
)

type AccountChangePublisher interface {
	PublishAccountChanged(int, adminevents.AccountPatch)
}

func (s *Service) SetAccountChangePublisher(publisher AccountChangePublisher) {
	s.accountEvents = publisher
}

// Importing an existing OAuth account also recovers its persisted state.
func (s *Service) createAccount(ctx context.Context, input CreateInput) (Account, error) {
	updated, err := s.repo.Create(ctx, input)
	if err == nil && s.accountEvents != nil {
		until := ""
		if updated.StateUntil != nil {
			until = updated.StateUntil.UTC().Format(time.RFC3339Nano)
		}
		s.accountEvents.PublishAccountChanged(updated.ID, adminevents.AccountPatch{
			AccountState: updated.State, StateUntil: &until, ErrorMsg: &updated.ErrorMsg,
			MaxConcurrency: &updated.MaxConcurrency, Priority: &updated.Priority,
			ModelDowngradeThreshold: &updated.ModelDowngradeThreshold,
			SchedulingWeight:        &updated.SchedulingWeight,
		})
	}
	return updated, err
}

// All repository updates, including bulk edits and cognition tests, pass here.
// Publish only fields written by this operation, never a stale whole-row snapshot.
func (s *Service) updateAccount(ctx context.Context, id int, input UpdateInput) (Account, error) {
	updated, err := s.repo.Update(ctx, id, input)
	if err != nil {
		return updated, err
	}
	patch := adminevents.AccountPatch{}
	if input.ModelDowngradeThreshold != nil {
		patch.ModelDowngradeThreshold = &updated.ModelDowngradeThreshold
	}
	if input.MaxConcurrency != nil {
		patch.MaxConcurrency = &updated.MaxConcurrency
	}
	if input.Priority != nil {
		patch.Priority = &updated.Priority
	}
	if input.SchedulingWeight != nil {
		patch.SchedulingWeight = &updated.SchedulingWeight
	}
	if input.ClearCognitionTest || input.CognitionDegraded != nil || input.HasExtra {
		patch.Cognition = &adminevents.CognitionResult{}
		if value, ok := updated.Extra["cognition_degraded"].(bool); ok {
			patch.Cognition.Degraded = &value
		}
	}
	if input.State != nil {
		patch.AccountState = updated.State
		until := ""
		if updated.StateUntil != nil {
			until = updated.StateUntil.UTC().Format(time.RFC3339Nano)
		}
		patch.StateUntil = &until
		patch.ErrorMsg = &updated.ErrorMsg
	}
	if s.accountEvents != nil && (patch.ModelDowngradeThreshold != nil || patch.MaxConcurrency != nil || patch.Priority != nil || patch.SchedulingWeight != nil || patch.Cognition != nil || patch.AccountState != "") {
		s.accountEvents.PublishAccountChanged(id, patch)
	}
	return updated, nil
}
