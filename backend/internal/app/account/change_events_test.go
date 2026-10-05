package account

import (
	"context"
	"errors"
	"testing"

	"github.com/DevilGenius/airgate-core/internal/adminevents"
)

type accountChangeRecorder struct{ patches []adminevents.AccountPatch }

func (r *accountChangeRecorder) PublishAccountChanged(_ int, patch adminevents.AccountPatch) {
	r.patches = append(r.patches, patch)
}

func TestAccountChangesPublishOnlyAfterPersistence(t *testing.T) {
	zero := 0
	threshold := 0.0
	no := false
	failure := errors.New("write failed")
	for _, tc := range []struct {
		name  string
		input UpdateInput
		extra map[string]any
		err   error
	}{
		{"zero priority and weight", UpdateInput{Priority: &zero, SchedulingWeight: &zero}, nil, nil},
		{"zero capacity", UpdateInput{MaxConcurrency: &zero}, nil, nil},
		{"disabled demotion threshold", UpdateInput{ModelDowngradeThreshold: &threshold}, nil, nil},
		{"normal cognition", UpdateInput{CognitionDegraded: &no}, map[string]any{"cognition_degraded": false}, nil},
		{"clear cognition", UpdateInput{ClearCognitionTest: true}, map[string]any{"other": "kept"}, nil},
		{"failed cognition", UpdateInput{CognitionDegraded: &no}, nil, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &accountChangeRecorder{}
			service := NewService(stubRepository{update: func(context.Context, int, UpdateInput) (Account, error) {
				if len(recorder.patches) != 0 {
					t.Fatal("published before persistence")
				}
				return Account{ID: 7, Extra: tc.extra}, tc.err
			}}, nil, nil, nil)
			service.SetAccountChangePublisher(recorder)
			_, err := service.updateAccount(t.Context(), 7, tc.input)
			if tc.err != nil {
				if !errors.Is(err, failure) || len(recorder.patches) != 0 {
					t.Fatal("failed writes must not publish")
				}
				return
			}
			if err != nil || len(recorder.patches) != 1 {
				t.Fatalf("patches=%v err=%v", recorder.patches, err)
			}
			patch := recorder.patches[0]
			if tc.input.ModelDowngradeThreshold != nil && (patch.ModelDowngradeThreshold == nil || *patch.ModelDowngradeThreshold != 0) {
				t.Fatal("lost zero threshold")
			}
			if tc.input.MaxConcurrency != nil && (patch.MaxConcurrency == nil || *patch.MaxConcurrency != 0) {
				t.Fatal("lost zero capacity")
			}
			if tc.input.Priority != nil && (patch.Priority == nil || *patch.Priority != 0 || patch.SchedulingWeight == nil || *patch.SchedulingWeight != 0) {
				t.Fatal("lost zero values")
			}
			if tc.input.CognitionDegraded != nil && (patch.Cognition == nil || patch.Cognition.Degraded == nil || *patch.Cognition.Degraded) {
				t.Fatal("lost false result")
			}
			if tc.input.ClearCognitionTest && (patch.Cognition == nil || patch.Cognition.Degraded != nil) {
				t.Fatal("missing explicit clear")
			}
			if patch.AccountState != "" {
				t.Fatal("unrelated state must not be published")
			}
		})
	}
}

func TestBulkPriorityAndClearCognitionUseChangePublisher(t *testing.T) {
	recorder := &accountChangeRecorder{}
	repo := stubRepository{update: func(_ context.Context, id int, input UpdateInput) (Account, error) {
		value := Account{ID: id}
		if input.Priority != nil {
			value.Priority = *input.Priority
		}
		return value, nil
	}}
	service := NewService(repo, nil, nil, nil)
	service.SetAccountChangePublisher(recorder)
	priority := 25
	result := service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{7, 8}, Priority: &priority})
	if len(result.SuccessIDs) != 2 || len(recorder.patches) != 2 {
		t.Fatalf("result=%+v patches=%+v", result, recorder.patches)
	}
	if err := service.ClearCognitionTest(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(recorder.patches) != 3 || recorder.patches[2].Cognition == nil {
		t.Fatal("clear did not publish")
	}
}

func TestImportRecoveryPublishesState(t *testing.T) {
	recorder := &accountChangeRecorder{}
	service := NewService(stubRepository{create: func(context.Context, CreateInput) (Account, error) {
		return Account{ID: 7, State: "active"}, nil
	}}, nil, nil, nil)
	service.SetAccountChangePublisher(recorder)
	if _, err := service.createAccount(t.Context(), CreateInput{}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.patches) != 1 || recorder.patches[0].AccountState != "active" || recorder.patches[0].StateUntil == nil || *recorder.patches[0].StateUntil != "" {
		t.Fatalf("missing recovery event: %+v", recorder.patches)
	}
}
