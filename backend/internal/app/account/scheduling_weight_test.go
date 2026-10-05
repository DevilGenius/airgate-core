package account

import (
	"context"
	"errors"
	"testing"
)

func TestSchedulingWeightValidationAcrossWritePaths(t *testing.T) {
	s := NewService(stubRepository{}, nil, nil, nil)
	for _, weight := range []int{-1, 1000001} {
		_, err := s.Create(t.Context(), CreateInput{SchedulingWeight: &weight})
		if !errors.Is(err, ErrInvalidSchedulingWeight) {
			t.Fatalf("create %d: %v", weight, err)
		}
		_, err = s.Update(t.Context(), 1, UpdateInput{SchedulingWeight: &weight})
		if !errors.Is(err, ErrInvalidSchedulingWeight) {
			t.Fatalf("update %d: %v", weight, err)
		}
		result := s.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{1, 2}, SchedulingWeight: &weight})
		if result.Failed != 2 || result.Success != 0 {
			t.Fatalf("bulk %d: %+v", weight, result)
		}
		input := CreateInput{Name: "import", Platform: "openai", Credentials: map[string]string{"api_key": "test"}, SchedulingWeight: &weight}
		if _, err := prepareImportAccount(input, false); !errors.Is(err, ErrInvalidSchedulingWeight) {
			t.Fatalf("import %d: %v", weight, err)
		}
	}
}

func TestSchedulingWeightDefaultsAndExplicitValues(t *testing.T) {
	var captured []int
	s := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			captured = append(captured, *input.SchedulingWeight)
			return Account{}, nil
		},
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			captured = append(captured, *input.SchedulingWeight)
			return Account{}, nil
		},
	}, nil, nil, nil)
	if _, err := s.Create(t.Context(), CreateInput{}); err != nil {
		t.Fatal(err)
	}
	for _, weight := range []int{0, 1, 1000000} {
		if _, err := s.Create(t.Context(), CreateInput{SchedulingWeight: &weight}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Update(t.Context(), 1, UpdateInput{SchedulingWeight: &weight}); err != nil {
			t.Fatal(err)
		}
	}
	want := []int{100, 0, 0, 1, 1, 1000000, 1000000}
	if len(captured) != len(want) {
		t.Fatalf("captured = %v", captured)
	}
	for i := range want {
		if captured[i] != want[i] {
			t.Fatalf("captured = %v", captured)
		}
	}
	input, err := prepareImportAccount(CreateInput{Name: "import", Platform: "openai", Credentials: map[string]string{"api_key": "test"}}, false)
	if err != nil || input.SchedulingWeight == nil || *input.SchedulingWeight != 100 {
		t.Fatalf("import default = %+v, %v", input, err)
	}
}
