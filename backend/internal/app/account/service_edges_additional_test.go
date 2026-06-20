package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestListAddsConcurrencyAndImageStatsFallback(t *testing.T) {
	now := time.Date(2026, 6, 20, 9, 0, 0, 0, time.Local)
	var capturedFilter ListFilter
	var imageIDs []int
	service := NewService(stubRepository{
		list: func(_ context.Context, filter ListFilter) ([]Account, int64, error) {
			capturedFilter = filter
			return []Account{
				{ID: 1, Name: "openai-with-stats", Platform: "openai"},
				{ID: 2, Name: "openai-zero", Platform: "openai"},
				{ID: 3, Name: "claude", Platform: "claude"},
			}, 3, nil
		},
		batchImageStats: func(_ context.Context, ids []int, _ time.Time) (map[int]AccountImageStats, error) {
			imageIDs = append([]int(nil), ids...)
			return map[int]AccountImageStats{1: {TodayCount: 4, TotalCount: 9}}, nil
		},
	}, nil, &captureConcurrency{counts: map[int]int{1: 2, 3: 7}}, nil)
	service.now = func() time.Time { return now }

	got, err := service.List(t.Context(), ListFilter{Page: -1, PageSize: 999, Platform: "openai"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got.Total != 3 || got.Page != 1 || got.PageSize != 999 {
		t.Fatalf("List() paging result = %+v", got)
	}
	if capturedFilter.Page != 1 || capturedFilter.PageSize != 999 || capturedFilter.Platform != "openai" {
		t.Fatalf("captured filter = %+v", capturedFilter)
	}
	if len(imageIDs) != 2 || imageIDs[0] != 1 || imageIDs[1] != 2 {
		t.Fatalf("BatchImageStats ids = %v, want [1 2]", imageIDs)
	}
	if got.List[0].CurrentConcurrency != 2 || got.List[2].CurrentConcurrency != 7 {
		t.Fatalf("concurrency values = %+v", got.List)
	}
	if got.List[0].ImageStats == nil || got.List[0].ImageStats.TodayCount != 4 || got.List[0].ImageStats.TotalCount != 9 {
		t.Fatalf("first image stats = %+v", got.List[0].ImageStats)
	}
	if got.List[1].ImageStats == nil || got.List[1].ImageStats.TodayCount != 0 || got.List[2].ImageStats != nil {
		t.Fatalf("image stats list = %+v", got.List)
	}

	repoErr := errors.New("list failed")
	service = NewService(stubRepository{
		list: func(context.Context, ListFilter) ([]Account, int64, error) {
			return nil, 0, repoErr
		},
	}, nil, noOpConcurrency{}, nil)
	if _, err := service.List(t.Context(), ListFilter{}); !errors.Is(err, repoErr) {
		t.Fatalf("List() error = %v, want %v", err, repoErr)
	}
}

func TestImportCollectsValidationAndRepositoryFailures(t *testing.T) {
	badRate := 0.001
	goodRate := 1.1
	createCalls := 0
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			createCalls++
			if input.Name == "repo-fail" {
				return Account{}, errors.New("create failed")
			}
			if len(input.GroupIDs) != 0 || input.ProxyID != nil {
				t.Fatalf("Import should clear scoped IDs, got groups=%v proxy=%v", input.GroupIDs, input.ProxyID)
			}
			return Account{ID: 100 + createCalls, Name: input.Name, Platform: input.Platform}, nil
		},
	}, nil, nil, nil)

	proxyID := int64(9)
	summary := service.Import(t.Context(), []CreateInput{
		{Name: "bad-rate", RateMultiplier: &badRate},
		{Name: "bad-policy", RateMultiplier: &goodRate, ModelPolicy: modelpolicy.Policy{Allow: []string{"["}}},
		{Name: "repo-fail", RateMultiplier: &goodRate},
		{Name: "ok", Platform: "openai", RateMultiplier: &goodRate, GroupIDs: []int64{1}, ProxyID: &proxyID},
	})

	if summary.Imported != 1 || summary.Failed != 3 || len(summary.SuccessIDs) != 1 || summary.SuccessIDs[0] == 0 {
		t.Fatalf("Import summary = %+v", summary)
	}
	if len(summary.Errors) != 3 || summary.Errors[0].Index != 0 || summary.Errors[1].Index != 1 || summary.Errors[2].Index != 2 {
		t.Fatalf("Import errors = %+v", summary.Errors)
	}
	if createCalls != 2 {
		t.Fatalf("repo.Create calls = %d, want repo-fail and ok only", createCalls)
	}
}

func TestGetSingleAccountUsageFetchesAndCachesGatewayResult(t *testing.T) {
	runtime := newAccountGatewayRuntime(t, &accountFakeGatewayPlugin{
		platform: "openai",
		handle: func(context.Context, string, string, string, http.Header, []byte) (int, http.Header, []byte, error) {
			return http.StatusOK, nil, []byte(`{"credits":{"balance":6.25},"windows":[{"key":"5h","used_percent":40,"reset_after_seconds":1800}]}`), nil
		},
	})
	defer runtime.cleanup()

	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, opts LoadOptions) (Account, error) {
			if opts.WithGroups || opts.WithProxy {
				t.Fatalf("FindByID opts = %+v, want empty", opts)
			}
			return Account{ID: id, Platform: "openai", Type: "oauth", State: "active"}, nil
		},
	}, accountGatewayCatalog{instances: map[string]*plugin.PluginInstance{"openai": runtime.instance}}, nil, nil)
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	got, err := service.GetSingleAccountUsage(t.Context(), 44)
	if err != nil {
		t.Fatalf("GetSingleAccountUsage() error = %v", err)
	}
	credits, ok := got["credits"].(map[string]any)
	if !ok || credits["balance"] != 6.25 {
		t.Fatalf("usage result = %#v", got)
	}
	if info, ok := service.getUsageInfoForAccount(t.Context(), 44); !ok || info.Credits == nil || info.Credits.Balance != 6.25 {
		t.Fatalf("cached usage info=%+v ok=%v", info, ok)
	}

	repoErr := errors.New("missing account")
	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{}, repoErr
		},
	}, nil, nil, nil)
	if _, err := service.GetSingleAccountUsage(t.Context(), 45); !errors.Is(err, repoErr) {
		t.Fatalf("GetSingleAccountUsage() error = %v, want %v", err, repoErr)
	}
}

func TestCapacityCreateDeleteAndStatsErrorBranches(t *testing.T) {
	repoErr := errors.New("repo failed")
	service := NewService(stubRepository{
		create: func(context.Context, CreateInput) (Account, error) {
			return Account{}, repoErr
		},
		delete: func(context.Context, int) error {
			return repoErr
		},
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{}, repoErr
		},
	}, nil, nil, nil)

	capacity := service.GetCapacity(t.Context(), []int{2, 2, 0, -1})
	if len(capacity) != 1 || capacity[2] != 0 {
		t.Fatalf("GetCapacity without reader = %+v", capacity)
	}
	if _, err := service.Create(t.Context(), CreateInput{Name: "bad"}); !errors.Is(err, repoErr) {
		t.Fatalf("Create() error = %v, want %v", err, repoErr)
	}
	if err := service.Delete(t.Context(), 12); !errors.Is(err, repoErr) {
		t.Fatalf("Delete() error = %v, want %v", err, repoErr)
	}
	if _, err := service.GetStats(t.Context(), 12, StatsQuery{}); !errors.Is(err, repoErr) {
		t.Fatalf("GetStats find error = %v, want %v", err, repoErr)
	}

	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 12, Platform: "openai"}, nil
		},
		findUsageLogs: func(context.Context, int, time.Time, time.Time) ([]UsageLog, error) {
			return nil, repoErr
		},
	}, nil, nil, nil)
	if _, err := service.GetStats(t.Context(), 12, StatsQuery{StartDate: "2026-06-20", EndDate: "2026-06-21"}); !errors.Is(err, repoErr) {
		t.Fatalf("GetStats usage log error = %v, want %v", err, repoErr)
	}
	if _, err := service.GetStats(t.Context(), 12, StatsQuery{StartDate: "bad"}); !errors.Is(err, ErrInvalidDateRange) {
		t.Fatalf("GetStats date error = %v, want ErrInvalidDateRange", err)
	}
}

func TestGetModelsAPIKeyFallbackEdges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{
				ID:          1,
				Platform:    "openai",
				Type:        "apikey",
				Credentials: map[string]string{"api_key": "sk-test", "base_url": server.URL},
			}, nil
		},
	}, stubPluginCatalog{models: []sdk.ModelInfo{{ID: "fallback", Name: "Fallback"}}}, nil, nil)
	models, err := service.GetModels(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetModels fallback error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "fallback" {
		t.Fatalf("fallback models = %+v", models)
	}

	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{
				ID:          2,
				Platform:    "openai",
				Type:        "apikey",
				Credentials: map[string]string{},
			}, nil
		},
	}, stubPluginCatalog{models: []sdk.ModelInfo{{ID: "missing-key-fallback"}}}, nil, nil)
	models, err = service.GetModels(t.Context(), 2)
	if err != nil || len(models) != 1 || models[0].ID != "missing-key-fallback" {
		t.Fatalf("GetModels missing key fallback models=%+v err=%v", models, err)
	}

	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{
				ID:          3,
				Platform:    "openai",
				Type:        "apikey",
				Credentials: map[string]string{"api_key": "sk-test"},
				Proxy:       &Proxy{Protocol: "http", Address: "%zz", Port: 8080},
			}, nil
		},
	}, stubPluginCatalog{models: []sdk.ModelInfo{{ID: "bad-proxy-fallback"}}}, nil, nil)
	models, err = service.GetModels(t.Context(), 3)
	if err != nil || len(models) != 1 || models[0].ID != "bad-proxy-fallback" {
		t.Fatalf("GetModels bad proxy fallback models=%+v err=%v", models, err)
	}
}

func TestUpdateAndBulkUpdateAdditionalErrorBranches(t *testing.T) {
	manualErr := errors.New("manual state failed")
	active := "active"
	service := NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{ID: 7, Platform: "openai", State: "disabled"}, nil
		},
	}, nil, nil, &failingManualStateWriter{stubStateWriter: newStubStateWriter(), recoverErr: manualErr})
	if _, err := service.Update(t.Context(), 7, UpdateInput{State: &active}); !errors.Is(err, manualErr) {
		t.Fatalf("Update manual state error = %v, want %v", err, manualErr)
	}

	call := 0
	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			call++
			if call == 2 {
				return Account{}, errors.New("reload failed")
			}
			return Account{ID: 7, Platform: "openai", State: "disabled"}, nil
		},
	}, nil, nil, newStubStateWriter())
	updated, err := service.Update(t.Context(), 7, UpdateInput{State: &active})
	if err != nil {
		t.Fatalf("Update reload warning path error = %v", err)
	}
	if updated.State != "active" {
		t.Fatalf("Update reload failure state = %q, want active", updated.State)
	}

	badRate := 0.001
	service = NewService(stubRepository{}, nil, nil, nil)
	result := service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{1, 2}, RateMultiplier: &badRate})
	if result.Failed != 2 || result.Success != 0 {
		t.Fatalf("BulkUpdate invalid rate result = %+v", result)
	}

	repoErr := errors.New("find extra failed")
	service = NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{}, repoErr
		},
	}, nil, nil, nil)
	result = service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{3}, Extra: map[string]any{"a": "b"}, HasExtra: true})
	if result.Failed != 1 || result.FailedIDs[0] != 3 || result.Results[0].Error == "" {
		t.Fatalf("BulkUpdate extra find error result = %+v", result)
	}

	updateErr := errors.New("update failed")
	priority := 9
	service = NewService(stubRepository{
		update: func(context.Context, int, UpdateInput) (Account, error) {
			return Account{}, updateErr
		},
	}, nil, nil, nil)
	result = service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{4}, Priority: &priority})
	if result.Failed != 1 || result.FailedIDs[0] != 4 || result.Results[0].Error == "" {
		t.Fatalf("BulkUpdate update error result = %+v", result)
	}
}
