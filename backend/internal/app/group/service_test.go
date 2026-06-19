package group

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
)

func TestListNormalizesPagination(t *testing.T) {
	var captured ListFilter

	service := NewService(groupStubRepository{
		list: func(_ context.Context, filter ListFilter) ([]Group, int64, error) {
			captured = filter
			return nil, 0, nil
		},
	}, stubConcurrencyReader{})

	result, err := service.List(t.Context(), ListFilter{})
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if captured.Page != 1 || captured.PageSize != 20 {
		t.Fatalf("List() normalized filter = %+v, want page=1 pageSize=20", captured)
	}
	if result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("List() result pagination = %+v, want page=1 pageSize=20", result)
	}
}

func TestCreateClonesMutableFields(t *testing.T) {
	var captured CreateInput

	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	quotas := map[string]any{"day": float64(100)}
	routing := map[string][]int64{"gpt-*": {1, 2}}

	_, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		Quotas:           quotas,
		ModelRouting:     routing,
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	quotas["day"] = float64(200)
	routing["gpt-*"][0] = 99

	if captured.Quotas["day"] != float64(100) {
		t.Fatalf("captured quotas mutated to %v, want 100", captured.Quotas["day"])
	}
	if captured.ModelRouting["gpt-*"][0] != 1 {
		t.Fatalf("captured model routing mutated to %v, want 1", captured.ModelRouting["gpt-*"][0])
	}
	if captured.RateMultiplier == nil || *captured.RateMultiplier != 1 {
		t.Fatalf("captured RateMultiplier = %v, want default 1", captured.RateMultiplier)
	}
}

func TestCreateNormalizesImagesOperationsAsCombinedSwitch(t *testing.T) {
	var captured CreateInput

	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	_, err := service.Create(t.Context(), CreateInput{
		Name:             "图片分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		OperationPolicies: map[string]bool{
			"images.generate":            true,
			"responses.image_generation": true,
		},
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if !captured.OperationPolicies["images.generate"] || !captured.OperationPolicies["images.edit"] {
		t.Fatalf("images operations = %#v, want generate/edit both true", captured.OperationPolicies)
	}
	if !captured.OperationPolicies["responses.image_generation"] {
		t.Fatalf("responses.image_generation should be preserved")
	}
}

func TestUpdateRemovesDisabledImagesOperations(t *testing.T) {
	var captured UpdateInput

	service := NewService(groupStubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	_, err := service.Update(t.Context(), 1, UpdateInput{
		OperationPolicies: map[string]bool{
			"images.generate": false,
			"images.edit":     false,
		},
	})
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}
	if _, ok := captured.OperationPolicies["images.generate"]; ok {
		t.Fatalf("images.generate should be removed when disabled: %#v", captured.OperationPolicies)
	}
	if _, ok := captured.OperationPolicies["images.edit"]; ok {
		t.Fatalf("images.edit should be removed when disabled: %#v", captured.OperationPolicies)
	}
}

func TestCreateRejectsInvalidRateMultiplier(t *testing.T) {
	service := NewService(groupStubRepository{}, stubConcurrencyReader{})
	rate := -1.0

	_, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		RateMultiplier:   &rate,
	})
	if !errors.Is(err, ErrInvalidRateMultiplier) {
		t.Fatalf("Create() error = %v, want ErrInvalidRateMultiplier", err)
	}
}

func TestCreateRejectsInvalidModelPolicy(t *testing.T) {
	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			t.Fatalf("repo.Create should not be called for invalid policy: %+v", input)
			return Group{}, nil
		},
	}, stubConcurrencyReader{})

	_, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		ModelPolicy:      modelpolicy.Policy{Allow: []string{"gpt-["}},
	})
	if !errors.Is(err, ErrInvalidModelPolicy) {
		t.Fatalf("Create() error = %v, want ErrInvalidModelPolicy", err)
	}
}

func TestUpdateRejectsInvalidAccountTypeModelPolicy(t *testing.T) {
	service := NewService(groupStubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Group, error) {
			t.Fatalf("repo.Update should not be called for invalid policy: %+v", input)
			return Group{}, nil
		},
	}, stubConcurrencyReader{})

	_, err := service.Update(t.Context(), 1, UpdateInput{
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{
			"plus": {Deny: []string{"o3-["}},
		},
	})
	if !errors.Is(err, ErrInvalidModelPolicy) {
		t.Fatalf("Update() error = %v, want ErrInvalidModelPolicy", err)
	}
}

func TestCreateAllowsMinimumPositiveRateMultiplier(t *testing.T) {
	captured := make([]float64, 0, 2)
	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			if input.RateMultiplier == nil {
				t.Fatalf("RateMultiplier should be normalized before repository create")
			}
			captured = append(captured, *input.RateMultiplier)
			return Group{ID: len(captured)}, nil
		},
	}, stubConcurrencyReader{})

	rate := 0.01
	if _, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		RateMultiplier:   &rate,
	}); err != nil {
		t.Fatalf("Create(rate=%v) returned error: %v", rate, err)
	}
	if len(captured) != 1 || captured[0] != 0.01 {
		t.Fatalf("captured rates = %v, want [0.01]", captured)
	}
}

func TestUpdateRejectsTooSmallPositiveRateMultiplier(t *testing.T) {
	service := NewService(groupStubRepository{}, stubConcurrencyReader{})

	for _, rate := range []float64{0, 0.001} {
		_, err := service.Update(t.Context(), 1, UpdateInput{RateMultiplier: &rate})
		if !errors.Is(err, ErrInvalidRateMultiplier) {
			t.Fatalf("Update(rate=%v) error = %v, want ErrInvalidRateMultiplier", rate, err)
		}
	}
}

type stubConcurrencyReader struct{}

func (stubConcurrencyReader) GetCurrentCounts(_ context.Context, _ []int) map[int]int {
	return nil
}

type groupStubRepository struct {
	list           func(context.Context, ListFilter) ([]Group, int64, error)
	listAvailable  func(context.Context, AvailableFilter) ([]Group, int64, error)
	findByID       func(context.Context, int) (Group, error)
	create         func(context.Context, CreateInput) (Group, error)
	update         func(context.Context, int, UpdateInput) (Group, error)
	delete         func(context.Context, int) error
	statsForGroups func(context.Context, []int) (map[int]GroupStats, map[int][]AccountCapacity, error)
}

func (s groupStubRepository) List(ctx context.Context, filter ListFilter) ([]Group, int64, error) {
	if s.list == nil {
		return nil, 0, nil
	}
	return s.list(ctx, filter)
}

func (s groupStubRepository) ListAvailable(ctx context.Context, filter AvailableFilter) ([]Group, int64, error) {
	if s.listAvailable == nil {
		return nil, 0, nil
	}
	return s.listAvailable(ctx, filter)
}

func (s groupStubRepository) FindByID(ctx context.Context, id int) (Group, error) {
	if s.findByID == nil {
		return Group{}, nil
	}
	return s.findByID(ctx, id)
}

func (s groupStubRepository) Create(ctx context.Context, input CreateInput) (Group, error) {
	if s.create == nil {
		return Group{}, nil
	}
	return s.create(ctx, input)
}

func (s groupStubRepository) Update(ctx context.Context, id int, input UpdateInput) (Group, error) {
	if s.update == nil {
		return Group{}, nil
	}
	return s.update(ctx, id, input)
}

func (s groupStubRepository) Delete(ctx context.Context, id int) error {
	if s.delete == nil {
		return nil
	}
	return s.delete(ctx, id)
}

func (s groupStubRepository) StatsForGroups(ctx context.Context, groupIDs []int, _ time.Time) (map[int]GroupStats, map[int][]AccountCapacity, error) {
	if s.statsForGroups == nil {
		return nil, nil, nil
	}
	return s.statsForGroups(ctx, groupIDs)
}
