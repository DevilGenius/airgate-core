package user

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdjustBalanceRejectsInvalidAction(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 10}, nil
		},
	})

	_, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "noop", Amount: 1})
	if err != ErrInvalidBalanceAction {
		t.Fatalf("expected ErrInvalidBalanceAction, got %v", err)
	}
}

func TestAdjustBalanceRejectsInsufficientBalance(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Balance: 5}, nil
		},
	})

	_, err := service.AdjustBalance(t.Context(), 1, BalanceChange{Action: "subtract", Amount: 10})
	if err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestListAPIKeysNormalizesPagination(t *testing.T) {
	service := NewService(stubRepository{
		listAPIKeys: func(_ context.Context, _ int, page, pageSize int) ([]APIKey, int64, error) {
			if page != 1 || pageSize != 20 {
				t.Fatalf("ListAPIKeys received page=%d pageSize=%d, want 1 and 20", page, pageSize)
			}
			return []APIKey{{ID: 1}}, 1, nil
		},
	})

	result, err := service.ListAPIKeys(t.Context(), 7, 0, 0, "")
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if result.Page != 1 || result.PageSize != 20 || result.Total != 1 || len(result.List) != 1 {
		t.Fatalf("unexpected ListAPIKeys result: %+v", result)
	}
}

func TestSetGroupRateAllowsZeroAndMinimumPositiveRate(t *testing.T) {
	var captured Mutation
	service := NewService(stubRepository{
		findByID: func() (User, error) {
			return User{ID: 1, Email: "user@example.com", GroupRates: map[int64]float64{9: 0.5}}, nil
		},
		update: func(_ context.Context, _ int, mutation Mutation) (User, error) {
			captured = mutation
			return User{ID: 1, Email: "user@example.com", GroupRates: mutation.GroupRates}, nil
		},
	})

	if _, err := service.SetGroupRate(t.Context(), 1, 9, 0); err != nil {
		t.Fatalf("SetGroupRate(rate=0) returned error: %v", err)
	}
	if !captured.HasGroupRates || captured.GroupRates[9] != 0 {
		t.Fatalf("captured zero override = %+v, want explicit 0", captured)
	}

	if _, err := service.SetGroupRate(t.Context(), 1, 9, 0.001); err != nil {
		t.Fatalf("SetGroupRate(rate=0.001) returned error: %v", err)
	}
	if captured.GroupRates[9] != 0.001 {
		t.Fatalf("captured rate = %v, want 0.001", captured.GroupRates[9])
	}
}

func TestSetGroupRateRejectsInvalidRate(t *testing.T) {
	service := NewService(stubRepository{})

	_, err := service.SetGroupRate(t.Context(), 1, 9, 0.0001)
	if !errors.Is(err, ErrInvalidRateMultiplier) {
		t.Fatalf("SetGroupRate() error = %v, want ErrInvalidRateMultiplier", err)
	}
}

func TestUpdateRejectsInvalidGroupRate(t *testing.T) {
	service := NewService(stubRepository{})

	_, err := service.Update(t.Context(), 1, UpdateInput{
		GroupRates:    map[int64]float64{9: -1},
		HasGroupRates: true,
	})
	if !errors.Is(err, ErrInvalidRateMultiplier) {
		t.Fatalf("Update() error = %v, want ErrInvalidRateMultiplier", err)
	}
}

type stubRepository struct {
	findByID    func() (User, error)
	listAPIKeys func(context.Context, int, int, int) ([]APIKey, int64, error)
	update      func(context.Context, int, Mutation) (User, error)
}

func (s stubRepository) FindByID(_ context.Context, _ int, _ bool) (User, error) {
	return s.findByID()
}

func (s stubRepository) List(_ context.Context, _ ListFilter) ([]User, int64, error) {
	return nil, 0, nil
}
func (s stubRepository) EmailExists(_ context.Context, _ string) (bool, error) { return false, nil }
func (s stubRepository) ListWithGroupRateOverride(_ context.Context, _ int64) ([]GroupRateOverride, error) {
	return nil, nil
}
func (s stubRepository) Create(_ context.Context, _ Mutation) (User, error) { return User{}, nil }
func (s stubRepository) Update(ctx context.Context, id int, mutation Mutation) (User, error) {
	if s.update != nil {
		return s.update(ctx, id, mutation)
	}
	return User{}, nil
}
func (s stubRepository) UpdateBalance(_ context.Context, _ int, _ BalanceUpdate) (User, error) {
	return User{}, nil
}
func (s stubRepository) Delete(_ context.Context, _ int) error { return nil }
func (s stubRepository) ListBalanceLogs(_ context.Context, _ int, _, _ int) ([]BalanceLog, int64, error) {
	return nil, 0, nil
}
func (s stubRepository) UpdateBalanceAlert(_ context.Context, _ int, _ float64) error { return nil }
func (s stubRepository) SetBalanceAlertNotified(_ context.Context, _ int, _ bool) error {
	return nil
}
func (s stubRepository) ListAPIKeys(ctx context.Context, userID, page, pageSize int, _ time.Time) ([]APIKey, int64, error) {
	if s.listAPIKeys == nil {
		return nil, 0, nil
	}
	return s.listAPIKeys(ctx, userID, page, pageSize)
}
func (s stubRepository) GetAPIKeyName(_ context.Context, _ int) (string, error) {
	return "", nil
}
func (s stubRepository) GetAPIKeyInfo(_ context.Context, _ int) (APIKeyBrief, error) {
	return APIKeyBrief{}, nil
}
