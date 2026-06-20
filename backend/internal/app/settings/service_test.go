package settings

import (
	"context"
	"errors"
	"testing"
)

func TestListDelegatesToRepository(t *testing.T) {
	service := NewService(settingsStubRepository{
		list: func(_ context.Context, group string) ([]Setting, error) {
			if group != "site" {
				t.Fatalf("List group = %q", group)
			}
			return []Setting{{Key: "site_name", Value: "AirGate", Group: group}}, nil
		},
	})

	items, err := service.List(t.Context(), "site")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].Key != "site_name" {
		t.Fatalf("List() = %+v", items)
	}
}

func TestUpdateClonesInput(t *testing.T) {
	var captured []ItemInput
	service := NewService(settingsStubRepository{
		upsertMany: func(_ context.Context, items []ItemInput) error {
			captured = append(captured, items...)
			return nil
		},
	})

	input := []ItemInput{{Key: "site_name", Value: "Airgate"}}
	if err := service.Update(t.Context(), input); err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	input[0].Value = "Changed"
	if captured[0].Value != "Airgate" {
		t.Fatalf("captured value = %q, want Airgate", captured[0].Value)
	}
}

func TestRepositoryErrorsPropagate(t *testing.T) {
	repoErr := errors.New("repo failed")
	service := NewService(settingsStubRepository{
		list:       func(context.Context, string) ([]Setting, error) { return nil, repoErr },
		upsertMany: func(context.Context, []ItemInput) error { return repoErr },
	})

	if _, err := service.List(t.Context(), "site"); !errors.Is(err, repoErr) {
		t.Fatalf("List error = %v", err)
	}
	if err := service.Update(t.Context(), []ItemInput{{Key: "site_name"}}); !errors.Is(err, repoErr) {
		t.Fatalf("Update error = %v", err)
	}
}

type settingsStubRepository struct {
	list       func(context.Context, string) ([]Setting, error)
	upsertMany func(context.Context, []ItemInput) error
}

func (s settingsStubRepository) List(ctx context.Context, group string) ([]Setting, error) {
	if s.list == nil {
		return nil, nil
	}
	return s.list(ctx, group)
}

func (s settingsStubRepository) UpsertMany(ctx context.Context, items []ItemInput) error {
	if s.upsertMany == nil {
		return nil
	}
	return s.upsertMany(ctx, items)
}
