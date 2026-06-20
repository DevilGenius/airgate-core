package subscription

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUserSubscriptionsNormalizesPagination(t *testing.T) {
	var captured UserListFilter
	service := NewService(subscriptionStubRepository{
		listByUser: func(_ context.Context, filter UserListFilter) ([]Subscription, int64, error) {
			captured = filter
			return nil, 0, nil
		},
	})

	result, err := service.UserSubscriptions(t.Context(), UserListFilter{UserID: 1})
	if err != nil {
		t.Fatalf("UserSubscriptions() returned error: %v", err)
	}
	if captured.Page != 1 || captured.PageSize != 20 {
		t.Fatalf("normalized filter = %+v, want page=1 pageSize=20", captured)
	}
	if result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("result pagination = %+v, want page=1 pageSize=20", result)
	}
}

func TestAdminAssignValidatesRFC3339(t *testing.T) {
	service := NewService(subscriptionStubRepository{})
	_, err := service.AdminAssign(t.Context(), AssignInput{
		UserID:    1,
		GroupID:   2,
		ExpiresAt: "2026-01-01",
	})
	if err != ErrInvalidExpiresAt {
		t.Fatalf("expected ErrInvalidExpiresAt, got %v", err)
	}
}

func TestAdminAdjustValidatesRFC3339(t *testing.T) {
	service := NewService(subscriptionStubRepository{})
	value := "2026-01-01"
	_, err := service.AdminAdjust(t.Context(), 1, AdjustInput{ExpiresAt: &value})
	if err != ErrInvalidAdjustExpiresAt {
		t.Fatalf("expected ErrInvalidAdjustExpiresAt, got %v", err)
	}
}

func TestSubscriptionServiceDelegates(t *testing.T) {
	now := time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339)
	var createInput CreateInput
	var bulkInput BulkCreateInput
	var updateInput UpdateInput
	service := NewService(subscriptionStubRepository{
		listActiveByUser: func(_ context.Context, userID int) ([]Subscription, error) {
			if userID != 7 {
				t.Fatalf("ListActiveByUser userID=%d", userID)
			}
			return []Subscription{{ID: 1, UserID: userID}}, nil
		},
		listAdmin: func(_ context.Context, filter AdminListFilter) ([]Subscription, int64, error) {
			if filter.Page != 1 || filter.PageSize != 20 || filter.Status != "active" {
				t.Fatalf("AdminList filter=%+v", filter)
			}
			return []Subscription{{ID: 2}}, 5, nil
		},
		create: func(_ context.Context, input CreateInput) (Subscription, error) {
			createInput = input
			return Subscription{ID: 3, UserID: input.UserID, GroupID: input.GroupID, Status: input.Status}, nil
		},
		bulkCreate: func(_ context.Context, input BulkCreateInput) (int, error) {
			bulkInput = input
			return len(input.UserIDs), nil
		},
		update: func(_ context.Context, id int, input UpdateInput) (Subscription, error) {
			updateInput = input
			return Subscription{ID: id, Status: *input.Status, ExpiresAt: *input.ExpiresAt}, nil
		},
	})
	service.now = func() time.Time { return now }

	active, err := service.ActiveSubscriptions(t.Context(), 7)
	if err != nil || len(active) != 1 || active[0].UserID != 7 {
		t.Fatalf("ActiveSubscriptions() = %+v/%v", active, err)
	}
	progress, err := service.SubscriptionProgress(t.Context(), 7)
	if err != nil || len(progress) != 0 {
		t.Fatalf("SubscriptionProgress() = %+v/%v", progress, err)
	}
	admin, err := service.AdminListSubscriptions(t.Context(), AdminListFilter{Status: "active"})
	if err != nil || admin.Total != 5 || admin.Page != 1 || admin.PageSize != 20 {
		t.Fatalf("AdminListSubscriptions() = %+v/%v", admin, err)
	}
	assigned, err := service.AdminAssign(t.Context(), AssignInput{UserID: 7, GroupID: 9, ExpiresAt: expires})
	if err != nil || assigned.ID != 3 || createInput.EffectiveAt != now || createInput.Status != "active" {
		t.Fatalf("AdminAssign() = %+v/%v input=%+v", assigned, err, createInput)
	}
	count, err := service.AdminBulkAssign(t.Context(), BulkAssignInput{UserIDs: []int{1, 2}, GroupID: 9, ExpiresAt: expires})
	if err != nil || count != 2 || bulkInput.EffectiveAt != now || bulkInput.Status != "active" {
		t.Fatalf("AdminBulkAssign() = %d/%v input=%+v", count, err, bulkInput)
	}
	bulkInput.UserIDs[0] = 99
	status := "cancelled"
	nextExpires := now.Add(48 * time.Hour).Format(time.RFC3339)
	adjusted, err := service.AdminAdjust(t.Context(), 3, AdjustInput{Status: &status, ExpiresAt: &nextExpires})
	if err != nil || adjusted.Status != "cancelled" || updateInput.ExpiresAt == nil || !updateInput.ExpiresAt.Equal(now.Add(48*time.Hour)) {
		t.Fatalf("AdminAdjust() = %+v/%v input=%+v", adjusted, err, updateInput)
	}
}

func TestAdminAdjustLogsPlanChangeWhenNotCancelled(t *testing.T) {
	status := "active"
	var captured UpdateInput
	service := NewService(subscriptionStubRepository{
		update: func(_ context.Context, id int, input UpdateInput) (Subscription, error) {
			captured = input
			return Subscription{ID: id, Status: *input.Status}, nil
		},
	})

	adjusted, err := service.AdminAdjust(t.Context(), 4, AdjustInput{Status: &status})
	if err != nil {
		t.Fatalf("AdminAdjust() returned error: %v", err)
	}
	if adjusted.Status != "active" || captured.Status == nil || *captured.Status != "active" {
		t.Fatalf("AdminAdjust() = %+v captured=%+v", adjusted, captured)
	}
}

func TestSubscriptionServiceErrors(t *testing.T) {
	repoErr := errors.New("repo failed")
	service := NewService(subscriptionStubRepository{
		listByUser:       func(context.Context, UserListFilter) ([]Subscription, int64, error) { return nil, 0, repoErr },
		listActiveByUser: func(context.Context, int) ([]Subscription, error) { return nil, repoErr },
		listAdmin:        func(context.Context, AdminListFilter) ([]Subscription, int64, error) { return nil, 0, repoErr },
		create:           func(context.Context, CreateInput) (Subscription, error) { return Subscription{}, repoErr },
		bulkCreate:       func(context.Context, BulkCreateInput) (int, error) { return 1, repoErr },
		update:           func(context.Context, int, UpdateInput) (Subscription, error) { return Subscription{}, repoErr },
	})
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	if _, err := service.UserSubscriptions(t.Context(), UserListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("UserSubscriptions error = %v", err)
	}
	if _, err := service.ActiveSubscriptions(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("ActiveSubscriptions error = %v", err)
	}
	if _, err := service.AdminListSubscriptions(t.Context(), AdminListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("AdminListSubscriptions error = %v", err)
	}
	if _, err := service.AdminAssign(t.Context(), AssignInput{ExpiresAt: expires}); !errors.Is(err, repoErr) {
		t.Fatalf("AdminAssign error = %v", err)
	}
	if count, err := service.AdminBulkAssign(t.Context(), BulkAssignInput{ExpiresAt: expires}); !errors.Is(err, repoErr) || count != 1 {
		t.Fatalf("AdminBulkAssign error = %d/%v", count, err)
	}
	if _, err := service.AdminAdjust(t.Context(), 1, AdjustInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("AdminAdjust error = %v", err)
	}

	_, err := service.AdminBulkAssign(t.Context(), BulkAssignInput{ExpiresAt: "bad-date"})
	if err != ErrInvalidExpiresAt {
		t.Fatalf("AdminBulkAssign invalid date error = %v", err)
	}
}

type subscriptionStubRepository struct {
	listByUser       func(context.Context, UserListFilter) ([]Subscription, int64, error)
	listActiveByUser func(context.Context, int) ([]Subscription, error)
	listAdmin        func(context.Context, AdminListFilter) ([]Subscription, int64, error)
	create           func(context.Context, CreateInput) (Subscription, error)
	bulkCreate       func(context.Context, BulkCreateInput) (int, error)
	update           func(context.Context, int, UpdateInput) (Subscription, error)
}

func (s subscriptionStubRepository) ListByUser(ctx context.Context, filter UserListFilter) ([]Subscription, int64, error) {
	if s.listByUser == nil {
		return nil, 0, nil
	}
	return s.listByUser(ctx, filter)
}

func (s subscriptionStubRepository) ListActiveByUser(ctx context.Context, userID int) ([]Subscription, error) {
	if s.listActiveByUser == nil {
		return nil, nil
	}
	return s.listActiveByUser(ctx, userID)
}

func (s subscriptionStubRepository) ListAdmin(ctx context.Context, filter AdminListFilter) ([]Subscription, int64, error) {
	if s.listAdmin == nil {
		return nil, 0, nil
	}
	return s.listAdmin(ctx, filter)
}

func (s subscriptionStubRepository) Create(ctx context.Context, input CreateInput) (Subscription, error) {
	if s.create == nil {
		return Subscription{
			ID:          1,
			UserID:      input.UserID,
			GroupID:     input.GroupID,
			EffectiveAt: input.EffectiveAt,
			ExpiresAt:   input.ExpiresAt,
			Status:      input.Status,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}, nil
	}
	return s.create(ctx, input)
}

func (s subscriptionStubRepository) BulkCreate(ctx context.Context, input BulkCreateInput) (int, error) {
	if s.bulkCreate == nil {
		return len(input.UserIDs), nil
	}
	return s.bulkCreate(ctx, input)
}

func (s subscriptionStubRepository) Update(ctx context.Context, id int, input UpdateInput) (Subscription, error) {
	if s.update == nil {
		return Subscription{ID: id}, nil
	}
	return s.update(ctx, id, input)
}
