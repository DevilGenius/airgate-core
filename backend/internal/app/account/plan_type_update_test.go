package account

import (
	"context"
	"fmt"
	"testing"
)

func TestManualPlanTypeUpdateWhileLocked(t *testing.T) {
	for _, plan := range []string{" team ", ""} {
		var captured UpdateInput
		service := NewService(stubRepository{
			findByID: func(context.Context, int, LoadOptions) (Account, error) {
				return Account{ID: 1, Credentials: map[string]string{"access_token": "secret", "plan_type": "plus"}, Extra: map[string]any{"plan_type_locked": true}}, nil
			},
			update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
				captured = input
				return Account{ID: 1, Credentials: input.Credentials}, nil
			},
		}, nil, nil, nil)
		if _, err := service.Update(t.Context(), 1, UpdateInput{PlanType: &plan}); err != nil {
			t.Fatal(err)
		}
		want := "team"
		if plan == "" {
			want = ""
		}
		if captured.AutomaticCredentials || captured.Credentials["plan_type"] != want || captured.Credentials["access_token"] != "secret" || captured.HasExtra {
			t.Fatalf("manual patch: %+v", captured)
		}
	}
}

func TestBulkPlanTypePreservesEachCredentialAndExtra(t *testing.T) {
	plan, pool := "prolite", false
	patches := map[int]UpdateInput{}
	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, _ LoadOptions) (Account, error) {
			return Account{ID: id, Credentials: map[string]string{"access_token": fmt.Sprint(id)}, Extra: map[string]any{"keep": id, "plan_type_locked": true}}, nil
		},
		update: func(_ context.Context, id int, input UpdateInput) (Account, error) {
			patches[id] = input
			return Account{ID: id}, nil
		},
		listAll: func(context.Context, ListFilter) ([]Account, error) { return nil, nil },
	}, nil, nil, nil)
	result := service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{1, 2}, PlanType: &plan, UpstreamIsPool: &pool, Extra: map[string]any{"plan_type_locked": false}, HasExtra: true})
	if result.Success != 2 {
		t.Fatalf("bulk update: %+v", result)
	}
	for id, patch := range patches {
		if patch.Credentials["plan_type"] != plan || patch.Credentials["access_token"] != fmt.Sprint(id) || patch.Extra["keep"] != id || patch.Extra["plan_type_locked"] != false || patch.UpstreamIsPool == nil || *patch.UpstreamIsPool {
			t.Fatalf("patch %d: %+v", id, patch)
		}
	}
}
