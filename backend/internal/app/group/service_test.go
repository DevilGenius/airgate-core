package group

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
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

func TestCreateRejectsInvalidAccountTypeModelPolicy(t *testing.T) {
	service := NewService(groupStubRepository{
		create: func(_ context.Context, input CreateInput) (Group, error) {
			t.Fatalf("repo.Create should not be called for invalid account type policy: %+v", input)
			return Group{}, nil
		},
	}, stubConcurrencyReader{})

	_, err := service.Create(t.Context(), CreateInput{
		Name:             "默认分组",
		Platform:         "openai",
		SubscriptionType: "standard",
		AccountTypeModelPolicies: map[string]modelpolicy.Policy{
			"plus": {Deny: []string{"o3-["}},
		},
	})
	if !errors.Is(err, ErrInvalidModelPolicy) {
		t.Fatalf("Create() error = %v, want ErrInvalidModelPolicy", err)
	}
}

func TestUpdateRejectsInvalidModelPolicy(t *testing.T) {
	service := NewService(groupStubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Group, error) {
			t.Fatalf("repo.Update should not be called for invalid policy: %+v", input)
			return Group{}, nil
		},
	}, stubConcurrencyReader{})

	policy := modelpolicy.Policy{Allow: []string{"gpt-["}}
	_, err := service.Update(t.Context(), 1, UpdateInput{ModelPolicy: &policy})
	if !errors.Is(err, ErrInvalidModelPolicy) {
		t.Fatalf("Update() error = %v, want ErrInvalidModelPolicy", err)
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

func TestStatsForGroupsMergesConcurrencyCounts(t *testing.T) {
	var capturedIDs []int
	reader := captureConcurrencyReader{
		counts: map[int]int{101: 2, 102: 3, 201: 5},
		seen:   &capturedIDs,
	}
	service := NewService(groupStubRepository{
		statsForGroups: func(_ context.Context, groupIDs []int) (map[int]GroupStats, map[int][]AccountCapacity, error) {
			if len(groupIDs) != 2 || groupIDs[0] != 1 || groupIDs[1] != 2 {
				t.Fatalf("groupIDs = %#v", groupIDs)
			}
			return map[int]GroupStats{
					1: {CapacityTotal: 10},
					2: {CapacityTotal: 20},
				}, map[int][]AccountCapacity{
					1: {{AccountID: 101}, {AccountID: 102}},
					2: {{AccountID: 201}},
				}, nil
		},
	}, reader)

	stats, err := service.StatsForGroups(t.Context(), []int{1, 2}, "UTC")
	if err != nil {
		t.Fatalf("StatsForGroups() error = %v", err)
	}
	if stats[1].CapacityUsed != 5 || stats[2].CapacityUsed != 5 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(capturedIDs) != 3 {
		t.Fatalf("captured account IDs = %#v", capturedIDs)
	}
}

func TestStatsForGroupsPropagatesRepositoryError(t *testing.T) {
	repoErr := errors.New("stats failed")
	service := NewService(groupStubRepository{
		statsForGroups: func(context.Context, []int) (map[int]GroupStats, map[int][]AccountCapacity, error) {
			return nil, nil, repoErr
		},
	}, stubConcurrencyReader{})

	if _, err := service.StatsForGroups(t.Context(), []int{1}, "UTC"); !errors.Is(err, repoErr) {
		t.Fatalf("StatsForGroups() error = %v", err)
	}
}

func TestListAvailableGetAndDelete(t *testing.T) {
	var availableFilter AvailableFilter
	var deletedID int
	service := NewService(groupStubRepository{
		listAvailable: func(_ context.Context, filter AvailableFilter) ([]Group, int64, error) {
			availableFilter = filter
			return []Group{{ID: 1}}, 4, nil
		},
		findByID: func(_ context.Context, id int) (Group, error) {
			return Group{ID: id, Name: "default"}, nil
		},
		delete: func(_ context.Context, id int) error {
			deletedID = id
			return nil
		},
	}, stubConcurrencyReader{})

	available, err := service.ListAvailable(t.Context(), AvailableFilter{UserID: 7})
	if err != nil || available.Total != 4 || available.Page != 1 || available.PageSize != 20 || availableFilter.Page != 1 {
		t.Fatalf("ListAvailable() = %+v/%v filter=%+v", available, err, availableFilter)
	}
	got, err := service.Get(t.Context(), 9)
	if err != nil || got.ID != 9 {
		t.Fatalf("Get() = %+v/%v", got, err)
	}
	if err := service.Delete(t.Context(), 9); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deletedID != 9 {
		t.Fatalf("deletedID = %d, want 9", deletedID)
	}
}

func TestRepositoryErrorsPropagate(t *testing.T) {
	repoErr := errors.New("repo failed")
	service := NewService(groupStubRepository{
		list:          func(context.Context, ListFilter) ([]Group, int64, error) { return nil, 0, repoErr },
		listAvailable: func(context.Context, AvailableFilter) ([]Group, int64, error) { return nil, 0, repoErr },
		findByID:      func(context.Context, int) (Group, error) { return Group{}, repoErr },
		create:        func(context.Context, CreateInput) (Group, error) { return Group{}, repoErr },
		update:        func(context.Context, int, UpdateInput) (Group, error) { return Group{}, repoErr },
		delete:        func(context.Context, int) error { return repoErr },
	}, stubConcurrencyReader{})

	if _, err := service.List(t.Context(), ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("List error = %v", err)
	}
	if _, err := service.ListAvailable(t.Context(), AvailableFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("ListAvailable error = %v", err)
	}
	if _, err := service.Get(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("Get error = %v", err)
	}
	if _, err := service.Create(t.Context(), CreateInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("Create error = %v", err)
	}
	if _, err := service.Update(t.Context(), 1, UpdateInput{}); !errors.Is(err, repoErr) {
		t.Fatalf("Update error = %v", err)
	}
	if err := service.Delete(t.Context(), 1); !errors.Is(err, repoErr) {
		t.Fatalf("Delete error = %v", err)
	}
}

func TestUpdateClonesDispatchAndPluginSettings(t *testing.T) {
	var captured UpdateInput
	service := NewService(groupStubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Group, error) {
			captured = input
			return Group{ID: 1}, nil
		},
	}, stubConcurrencyReader{})

	rate := 1.25
	policy := modelpolicy.Policy{Allow: []string{"gpt-*"}}
	dsl := sdk.DispatchDSL{Rules: []sdk.DispatchRule{{
		ID: "chat",
		When: sdk.DispatchWhen{
			Methods:       []string{"POST"},
			Paths:         []string{"/v1/chat/completions"},
			PathPrefixes:  []string{"/v1/"},
			Models:        []string{"gpt-4.1"},
			ModelPrefixes: []string{"gpt-"},
			ModelSuffixes: []string{"-mini"},
		},
		Candidates: []sdk.DispatchCandidate{{Scheduling: "gpt-4.1", Wire: "gpt-4.1-wire"}},
	}}}
	pluginSettings := map[string]map[string]string{"openai": {"image_enabled": "true"}}
	modelRouting := map[string][]int64{"gpt-*": {1, 2}}

	if _, err := service.Update(t.Context(), 1, UpdateInput{
		RateMultiplier: &rate,
		ModelPolicy:    &policy,
		DispatchDSL:    &dsl,
		ModelRouting:   modelRouting,
		PluginSettings: pluginSettings,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	dsl.Rules[0].When.Methods[0] = "GET"
	dsl.Rules[0].Candidates[0].Scheduling = "mutated"
	pluginSettings["openai"]["image_enabled"] = "false"
	modelRouting["gpt-*"][0] = 99
	if captured.DispatchDSL == nil || captured.DispatchDSL.Rules[0].When.Methods[0] != "POST" || captured.DispatchDSL.Rules[0].Candidates[0].Scheduling != "gpt-4.1" {
		t.Fatalf("DispatchDSL was not cloned: %+v", captured.DispatchDSL)
	}
	if captured.PluginSettings["openai"]["image_enabled"] != "true" {
		t.Fatalf("PluginSettings was not cloned: %+v", captured.PluginSettings)
	}
	if captured.ModelRouting["gpt-*"][0] != 1 {
		t.Fatalf("ModelRouting was not cloned: %+v", captured.ModelRouting)
	}
}

type stubConcurrencyReader struct{}

func (stubConcurrencyReader) GetCurrentCounts(_ context.Context, _ []int) map[int]int {
	return nil
}

type captureConcurrencyReader struct {
	counts map[int]int
	seen   *[]int
}

func (r captureConcurrencyReader) GetCurrentCounts(_ context.Context, ids []int) map[int]int {
	if r.seen != nil {
		*r.seen = append((*r.seen)[:0], ids...)
	}
	return r.counts
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
