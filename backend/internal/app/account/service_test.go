package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/plugin"
)

func TestImportIgnoresEnvironmentScopedIDs(t *testing.T) {
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			if len(input.GroupIDs) != 0 {
				t.Fatalf("expected import to clear group IDs, got %v", input.GroupIDs)
			}
			if input.ProxyID != nil {
				t.Fatalf("expected import to clear proxy ID, got %v", *input.ProxyID)
			}
			return Account{ID: 1, Name: input.Name}, nil
		},
	}, nil, nil, nil)

	proxyID := int64(99)
	rateMultiplier := 1.2
	summary := service.Import(t.Context(), []CreateInput{{
		Name:           "demo",
		Platform:       "openai",
		Type:           "apikey",
		Credentials:    map[string]string{"api_key": "secret"},
		Priority:       3,
		MaxConcurrency: 5,
		RateMultiplier: &rateMultiplier,
		GroupIDs:       []int64{2, 1},
		ProxyID:        &proxyID,
	}})

	if summary.Imported != 1 || summary.Failed != 0 {
		t.Fatalf("unexpected import summary: %+v", summary)
	}
}

func TestCreateDefaultsMissingRateMultiplierToOne(t *testing.T) {
	var captured *float64
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			captured = input.RateMultiplier
			return Account{ID: 1, Platform: input.Platform}, nil
		},
	}, nil, nil, nil)

	if _, err := service.Create(t.Context(), CreateInput{Platform: "openai"}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if captured == nil || *captured != 1 {
		t.Fatalf("captured RateMultiplier = %v, want default 1", captured)
	}
}

func TestCreateAllowsZeroAndMinimumPositiveRateMultiplier(t *testing.T) {
	captured := make([]float64, 0, 2)
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			if input.RateMultiplier == nil {
				t.Fatalf("RateMultiplier should be normalized before repository create")
			}
			captured = append(captured, *input.RateMultiplier)
			return Account{ID: len(captured), Platform: input.Platform}, nil
		},
	}, nil, nil, nil)

	rate := 0.01
	if _, err := service.Create(t.Context(), CreateInput{
		Platform:       "openai",
		RateMultiplier: &rate,
	}); err != nil {
		t.Fatalf("Create(rate=%v) returned error: %v", rate, err)
	}
	if len(captured) != 1 || captured[0] != 0.01 {
		t.Fatalf("captured rates = %v, want [0.01]", captured)
	}
}

func TestCreateRejectsInvalidRateMultiplier(t *testing.T) {
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			t.Fatalf("repo.Create should not be called for invalid rate: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, nil)

	for _, rate := range []float64{-1, 0, 0.001} {
		_, err := service.Create(t.Context(), CreateInput{
			Platform:       "openai",
			RateMultiplier: &rate,
		})
		if !errors.Is(err, ErrInvalidRateMultiplier) {
			t.Fatalf("Create(rate=%v) error = %v, want ErrInvalidRateMultiplier", rate, err)
		}
	}
}

func TestCreateRejectsInvalidModelPolicy(t *testing.T) {
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			t.Fatalf("repo.Create should not be called for invalid policy: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, nil)

	_, err := service.Create(t.Context(), CreateInput{
		Platform:    "openai",
		ModelPolicy: modelpolicy.Policy{Allow: []string{"gpt-["}},
	})
	if !errors.Is(err, ErrInvalidModelPolicy) {
		t.Fatalf("Create() error = %v, want ErrInvalidModelPolicy", err)
	}
}

func TestCreateNormalizesModelPolicyBeforePersist(t *testing.T) {
	var captured CreateInput
	service := NewService(stubRepository{
		create: func(_ context.Context, input CreateInput) (Account, error) {
			captured = input
			return Account{ID: 1, Platform: input.Platform}, nil
		},
	}, nil, nil, nil)

	if _, err := service.Create(t.Context(), CreateInput{
		Platform:    "openai",
		ModelPolicy: modelpolicy.Policy{Allow: []string{" GPT-5* ", ""}},
	}); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if len(captured.ModelPolicy.Allow) != 1 || captured.ModelPolicy.Allow[0] != "GPT-5*" {
		t.Fatalf("captured policy = %#v, want trimmed single allow", captured.ModelPolicy)
	}
}

func TestUpdateRejectsInvalidRateMultiplier(t *testing.T) {
	rate := 0.001
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for invalid rate: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, nil)

	_, err := service.Update(t.Context(), 1, UpdateInput{RateMultiplier: &rate})
	if !errors.Is(err, ErrInvalidRateMultiplier) {
		t.Fatalf("Update error = %v, want ErrInvalidRateMultiplier", err)
	}
}

func TestUpdateRejectsInvalidModelPolicy(t *testing.T) {
	policy := modelpolicy.Policy{Deny: []string{"o3-["}}
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for invalid policy: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, nil)

	_, err := service.Update(t.Context(), 1, UpdateInput{ModelPolicy: &policy})
	if !errors.Is(err, ErrInvalidModelPolicy) {
		t.Fatalf("Update() error = %v, want ErrInvalidModelPolicy", err)
	}
}

func TestBulkUpdateRejectsInvalidRateMultiplier(t *testing.T) {
	rate := -1.0
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for invalid rate: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, nil)

	result := service.BulkUpdate(t.Context(), BulkUpdateInput{
		IDs:            []int{1, 2},
		RateMultiplier: &rate,
	})
	if result.Success != 0 || result.Failed != 2 {
		t.Fatalf("BulkUpdate result = %+v, want 2 failures", result)
	}
	for _, item := range result.Results {
		if item.Success || item.Error == "" {
			t.Fatalf("result item = %+v, want invalid rate failure", item)
		}
	}
}

func TestBulkUpdateRejectsInvalidModelPolicy(t *testing.T) {
	policy := modelpolicy.Policy{Allow: []string{"gpt-["}}
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for invalid policy: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, nil)

	result := service.BulkUpdate(t.Context(), BulkUpdateInput{
		IDs:         []int{1, 2},
		ModelPolicy: &policy,
	})
	if result.Success != 0 || result.Failed != 2 {
		t.Fatalf("BulkUpdate result = %+v, want 2 failures", result)
	}
	for _, item := range result.Results {
		if item.Success || item.Error == "" {
			t.Fatalf("result item = %+v, want invalid policy failure", item)
		}
	}
}

func TestGetModelsUsesUpstreamForAPIKeyAccount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"upstream-a"},{"id":"upstream-b","name":"Upstream B"}]}`))
	}))
	defer upstream.Close()

	service := NewService(stubRepository{
		findByID: func(_ context.Context, _ int, opts LoadOptions) (Account, error) {
			if !opts.WithProxy {
				t.Fatalf("expected WithProxy to be true")
			}
			return Account{
				ID:          1,
				Platform:    "openai",
				Type:        "apikey",
				Credentials: map[string]string{"api_key": "sk-test", "base_url": upstream.URL},
			}, nil
		},
	}, stubPluginCatalog{models: []sdk.ModelInfo{{ID: "fallback"}}}, nil, nil)

	models, err := service.GetModels(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetModels returned error: %v", err)
	}
	if len(models) != 2 || models[0].ID != "upstream-a" || models[0].Name != "upstream-a" || models[1].ID != "upstream-b" || models[1].Name != "Upstream B" {
		t.Fatalf("models = %+v", models)
	}
}

func TestGetModelsUsesPluginModelsForOAuthAccount(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	service := NewService(stubRepository{
		findByID: func(context.Context, int, LoadOptions) (Account, error) {
			return Account{
				ID:          1,
				Platform:    "openai",
				Type:        "oauth",
				Credentials: map[string]string{"access_token": "token", "base_url": upstream.URL},
			}, nil
		},
	}, stubPluginCatalog{models: []sdk.ModelInfo{{ID: "plugin-model", Name: "Plugin Model"}}}, nil, nil)

	models, err := service.GetModels(t.Context(), 1)
	if err != nil {
		t.Fatalf("GetModels returned error: %v", err)
	}
	if upstreamCalled {
		t.Fatalf("OAuth account should not request upstream models")
	}
	if len(models) != 1 || models[0].ID != "plugin-model" || models[0].Name != "Plugin Model" {
		t.Fatalf("models = %+v", models)
	}
}

func TestShouldPersistQuotaExtraAllowsClearingPlanMetadata(t *testing.T) {
	if !shouldPersistQuotaExtra("plan_type", "") {
		t.Fatalf("empty plan_type should be persisted to clear stale subscription data")
	}
	if !shouldPersistQuotaExtra("subscription_active_until", "") {
		t.Fatalf("empty subscription_active_until should be persisted to clear stale subscription data")
	}
	if shouldPersistQuotaExtra("email", "") {
		t.Fatalf("empty non-plan metadata should not be persisted")
	}
}

func TestQuotaRefreshCredentialsInjectsProxyURL(t *testing.T) {
	proxy := &Proxy{Protocol: "http", Address: "10.0.0.1", Port: 7890}
	proxyWithAuth := &Proxy{Protocol: "socks5", Address: "10.0.0.2", Port: 1080, Username: "u", Password: "p"}

	cases := []struct {
		name string
		item Account
		want map[string]string
	}{
		{
			name: "no proxy, no credentials",
			item: Account{},
			want: map[string]string{},
		},
		{
			name: "no proxy, with credentials",
			item: Account{Credentials: map[string]string{"refresh_token": "rt"}},
			want: map[string]string{"refresh_token": "rt"},
		},
		{
			name: "proxy bound, empty credentials",
			item: Account{Proxy: proxy},
			want: map[string]string{"proxy_url": "http://10.0.0.1:7890"},
		},
		{
			name: "proxy bound, credentials without proxy_url",
			item: Account{
				Credentials: map[string]string{"refresh_token": "rt", "session_token": "st"},
				Proxy:       proxy,
			},
			want: map[string]string{
				"refresh_token": "rt",
				"session_token": "st",
				"proxy_url":     "http://10.0.0.1:7890",
			},
		},
		{
			name: "proxy bound with auth",
			item: Account{Credentials: map[string]string{"access_token": "at"}, Proxy: proxyWithAuth},
			want: map[string]string{
				"access_token": "at",
				"proxy_url":    "socks5://u:p@10.0.0.2:1080",
			},
		},
		{
			name: "user-supplied proxy_url overrides bound proxy",
			item: Account{
				Credentials: map[string]string{"proxy_url": "http://override:8080", "refresh_token": "rt"},
				Proxy:       proxy,
			},
			want: map[string]string{
				"proxy_url":     "http://override:8080",
				"refresh_token": "rt",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := quotaRefreshCredentials(tc.item)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestQuotaRefreshCredentialsDoesNotMutateInput(t *testing.T) {
	original := map[string]string{"refresh_token": "rt"}
	item := Account{
		Credentials: original,
		Proxy:       &Proxy{Protocol: "http", Address: "10.0.0.1", Port: 7890},
	}
	_ = quotaRefreshCredentials(item)
	if _, exists := original["proxy_url"]; exists {
		t.Fatalf("quotaRefreshCredentials mutated input credentials: %v", original)
	}
}

func TestShouldAutoRefreshQuotaSkipsPureAPIKeyAccounts(t *testing.T) {
	cases := []struct {
		name string
		item Account
		want bool
	}{
		{
			name: "apikey type",
			item: Account{Type: "apikey", Credentials: map[string]string{"api_key": "sk-test"}},
			want: false,
		},
		{
			name: "legacy api key credentials without type",
			item: Account{Credentials: map[string]string{"api_key": "sk-test"}},
			want: false,
		},
		{
			name: "oauth access token",
			item: Account{Type: "oauth", Credentials: map[string]string{"access_token": "at"}},
			want: true,
		},
		{
			name: "oauth refresh token",
			item: Account{Type: "oauth", Credentials: map[string]string{"refresh_token": "rt"}},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoRefreshQuota(tc.item); got != tc.want {
				t.Fatalf("shouldAutoRefreshQuota() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListResolvesPluginOAuthPlanFilter(t *testing.T) {
	var captured ListFilter
	service := NewService(stubRepository{
		list: func(_ context.Context, filter ListFilter) ([]Account, int64, error) {
			captured = filter
			return nil, 0, nil
		},
	}, stubPluginCatalog{
		metas: []plugin.PluginMeta{{
			Platform: "kiro",
			Metadata: map[string]string{
				oauthPlanMetadataKey: `[{"key":"pro","label":"Pro","credential_key":"plan_type","match":"contains","matches":["Builder Id Pro"]}]`,
			},
		}},
	}, noOpConcurrency{}, nil)

	_, err := service.List(t.Context(), ListFilter{
		Page:        1,
		PageSize:    20,
		Platform:    "kiro",
		AccountType: oauthPlanFilterID("kiro", "pro"),
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if captured.AccountType != "" {
		t.Fatalf("captured AccountType = %q, want empty after virtual filter resolution", captured.AccountType)
	}
	if captured.Credential == nil {
		t.Fatal("captured Credential is nil")
	}
	if captured.Credential.Platform != "kiro" ||
		captured.Credential.AccountType != "oauth" ||
		captured.Credential.Key != "plan_type" ||
		captured.Credential.MatchMode != "contains" ||
		len(captured.Credential.Values) != 1 ||
		captured.Credential.Values[0] != "Builder Id Pro" {
		t.Fatalf("captured Credential = %+v", captured.Credential)
	}
}

func TestListKeepsUnknownOAuthPlanFilterExact(t *testing.T) {
	var captured ListFilter
	service := NewService(stubRepository{
		list: func(_ context.Context, filter ListFilter) ([]Account, int64, error) {
			captured = filter
			return nil, 0, nil
		},
	}, stubPluginCatalog{}, noOpConcurrency{}, nil)

	_, err := service.List(t.Context(), ListFilter{Page: 1, PageSize: 20, AccountType: oauthPlanFilterID("openai", "plus")})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if captured.AccountType != oauthPlanFilterID("openai", "plus") {
		t.Fatalf("captured AccountType = %q, want unresolved virtual filter to remain exact", captured.AccountType)
	}
	if captured.Credential != nil {
		t.Fatalf("captured Credential = %+v, want nil", captured.Credential)
	}
}

type stubRepository struct {
	create           func(context.Context, CreateInput) (Account, error)
	update           func(context.Context, int, UpdateInput) (Account, error)
	delete           func(context.Context, int) error
	findByID         func(context.Context, int, LoadOptions) (Account, error)
	list             func(context.Context, ListFilter) ([]Account, int64, error)
	listAll          func(context.Context, ListFilter) ([]Account, error)
	listByPlatform   func(context.Context, string) ([]Account, error)
	findUsageLogs    func(context.Context, int, time.Time, time.Time) ([]UsageLog, error)
	batchWindowStats func(context.Context, []int, time.Time) (map[int]AccountWindowStats, error)
	batchImageStats  func(context.Context, []int, time.Time) (map[int]AccountImageStats, error)
	saveCredentials  func(context.Context, int, map[string]string) error
}

type noOpConcurrency struct{}

func (noOpConcurrency) GetCurrentCounts(context.Context, []int) map[int]int {
	return map[int]int{}
}

func (noOpConcurrency) GetWorkingCounts(context.Context) map[int]int {
	return map[int]int{}
}

func (s stubRepository) List(ctx context.Context, filter ListFilter) ([]Account, int64, error) {
	if s.list != nil {
		return s.list(ctx, filter)
	}
	return nil, 0, nil
}

func (s stubRepository) ListAll(ctx context.Context, filter ListFilter) ([]Account, error) {
	if s.listAll != nil {
		return s.listAll(ctx, filter)
	}
	return nil, nil
}

func (s stubRepository) Create(ctx context.Context, input CreateInput) (Account, error) {
	if s.create == nil {
		return Account{}, nil
	}
	return s.create(ctx, input)
}

func (s stubRepository) Update(ctx context.Context, id int, input UpdateInput) (Account, error) {
	if s.update != nil {
		return s.update(ctx, id, input)
	}
	return Account{ID: id}, nil
}

func (s stubRepository) Delete(ctx context.Context, id int) error {
	if s.delete != nil {
		return s.delete(ctx, id)
	}
	return nil
}

func (s stubRepository) FindByID(ctx context.Context, id int, opts LoadOptions) (Account, error) {
	if s.findByID == nil {
		return Account{}, nil
	}
	return s.findByID(ctx, id, opts)
}

func (s stubRepository) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	if s.listByPlatform != nil {
		return s.listByPlatform(ctx, platform)
	}
	return nil, nil
}

func (s stubRepository) FindUsageLogs(ctx context.Context, id int, start, end time.Time) ([]UsageLog, error) {
	if s.findUsageLogs != nil {
		return s.findUsageLogs(ctx, id, start, end)
	}
	return nil, nil
}

func (s stubRepository) BatchWindowStats(ctx context.Context, ids []int, start time.Time) (map[int]AccountWindowStats, error) {
	if s.batchWindowStats != nil {
		return s.batchWindowStats(ctx, ids, start)
	}
	return nil, nil
}

func (s stubRepository) BatchImageStats(ctx context.Context, ids []int, start time.Time) (map[int]AccountImageStats, error) {
	if s.batchImageStats != nil {
		return s.batchImageStats(ctx, ids, start)
	}
	return nil, nil
}

func (s stubRepository) SaveCredentials(ctx context.Context, id int, credentials map[string]string) error {
	if s.saveCredentials != nil {
		return s.saveCredentials(ctx, id, credentials)
	}
	return nil
}

// stubStateWriter 捕获 StateWriter 调用。
type stubStateWriter struct {
	rateLimited    map[int]*time.Time
	cleared        map[int]bool
	markersCleared map[int]int
	disabled       map[int]string
	degraded       map[int]string
	recovered      map[int]bool
	routeRefreshed map[int]bool
}

func newStubStateWriter() *stubStateWriter {
	return &stubStateWriter{
		rateLimited:    map[int]*time.Time{},
		cleared:        map[int]bool{},
		markersCleared: map[int]int{},
		disabled:       map[int]string{},
		degraded:       map[int]string{},
		recovered:      map[int]bool{},
		routeRefreshed: map[int]bool{},
	}
}

func (s *stubStateWriter) MarkRateLimited(_ context.Context, accountID int, until time.Time, _ string) {
	cp := until
	s.rateLimited[accountID] = &cp
}

func (s *stubStateWriter) ClearRateLimited(_ context.Context, accountID int) {
	s.cleared[accountID] = true
}

func (s *stubStateWriter) ClearRateLimitMarkers(_ context.Context, accountID int) int {
	s.markersCleared[accountID]++
	return 0
}

func (s *stubStateWriter) MarkDisabled(_ context.Context, accountID int, reason string) {
	s.disabled[accountID] = reason
}

func (s *stubStateWriter) MarkDegraded(_ context.Context, accountID int, reason string) {
	s.degraded[accountID] = reason
}

func (s *stubStateWriter) ManualRecover(_ context.Context, accountID int) error {
	s.recovered[accountID] = true
	return nil
}

func (s *stubStateWriter) ManualDisable(_ context.Context, accountID int, reason string) error {
	s.disabled[accountID] = reason
	return nil
}

func (s *stubStateWriter) RefreshRouteGraphAccount(_ context.Context, accountID int) {
	s.routeRefreshed[accountID] = true
}

func TestMarkAccountUsageErrorDegradesForbidden(t *testing.T) {
	writer := newStubStateWriter()
	service := NewService(stubRepository{}, nil, nil, writer)

	service.markAccountUsageError(context.Background(), 42, "HTTP 403: 访问被拒绝")

	if got := writer.degraded[42]; got != "HTTP 403: 访问被拒绝" {
		t.Fatalf("MarkDegraded reason = %q, want forbidden reason", got)
	}
	if _, ok := writer.disabled[42]; ok {
		t.Fatalf("403 usage error should not MarkDisabled")
	}
}

func TestMarkAccountUsageErrorDisablesNonForbidden(t *testing.T) {
	writer := newStubStateWriter()
	service := NewService(stubRepository{}, nil, nil, writer)

	service.markAccountUsageError(context.Background(), 42, "HTTP 401: invalid token")

	if got := writer.disabled[42]; got != "HTTP 401: invalid token" {
		t.Fatalf("MarkDisabled reason = %q, want auth reason", got)
	}
	if _, ok := writer.degraded[42]; ok {
		t.Fatalf("non-403 usage error should not MarkDegraded")
	}
}

type stubPluginCatalog struct {
	models           []sdk.ModelInfo
	metas            []plugin.PluginMeta
	accountTypes     []sdk.AccountType
	credentialFields []sdk.CredentialField
}

func (s stubPluginCatalog) GetPluginByPlatform(string) *plugin.PluginInstance { return nil }
func (s stubPluginCatalog) GetModels(string) []sdk.ModelInfo                  { return s.models }
func (s stubPluginCatalog) GetAccountTypes(string) []sdk.AccountType          { return s.accountTypes }
func (s stubPluginCatalog) GetCredentialFields(string) []sdk.CredentialField {
	return s.credentialFields
}
func (s stubPluginCatalog) GetAllPluginMeta() []plugin.PluginMeta { return s.metas }

func TestUpdateRoutesManualStateThroughStateWriter(t *testing.T) {
	state := " Disabled "
	name := "renamed"
	updateCalled := false
	writer := newStubStateWriter()
	service := NewService(stubRepository{
		update: func(_ context.Context, id int, input UpdateInput) (Account, error) {
			updateCalled = true
			if id != 42 {
				t.Fatalf("id = %d, want 42", id)
			}
			if input.State != nil {
				t.Fatalf("repo.Update received State = %q, want nil", *input.State)
			}
			if input.Name == nil || *input.Name != name {
				t.Fatalf("repo.Update Name = %v, want %q", input.Name, name)
			}
			return Account{ID: id, Platform: "openai", State: "active"}, nil
		},
		findByID: func(_ context.Context, id int, opts LoadOptions) (Account, error) {
			if id != 42 {
				t.Fatalf("id = %d, want 42", id)
			}
			if !opts.WithGroups || !opts.WithProxy {
				t.Fatalf("reload opts = %+v, want groups and proxy", opts)
			}
			return Account{ID: id, Platform: "openai", State: "disabled"}, nil
		},
	}, nil, nil, writer)

	updated, err := service.Update(t.Context(), 42, UpdateInput{Name: &name, State: &state})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if !updateCalled {
		t.Fatalf("repo.Update should be called for non-state fields")
	}
	if got := writer.disabled[42]; got != "手动关闭" {
		t.Fatalf("ManualDisable reason = %q, want 手动关闭", got)
	}
	if updated.State != "disabled" {
		t.Fatalf("updated.State = %q, want disabled", updated.State)
	}
}

func TestUpdateRejectsBlankState(t *testing.T) {
	state := "  "
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for blank state: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, newStubStateWriter())

	_, err := service.Update(t.Context(), 7, UpdateInput{State: &state})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("Update error = %v, want ErrInvalidState", err)
	}
}

func TestUpdateStateOnlyUsesManualRecover(t *testing.T) {
	state := "active"
	writer := newStubStateWriter()
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for state-only manual update: %+v", input)
			return Account{}, nil
		},
		findByID: func(_ context.Context, id int, _ LoadOptions) (Account, error) {
			return Account{ID: id, Platform: "openai", State: "active"}, nil
		},
	}, nil, nil, writer)

	if _, err := service.Update(t.Context(), 7, UpdateInput{State: &state}); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if !writer.recovered[7] {
		t.Fatalf("ManualRecover should be called for account 7")
	}
}

func TestBulkUpdateRoutesManualStateThroughStateWriter(t *testing.T) {
	state := "disabled"
	priority := 3
	var updateIDs []int
	writer := newStubStateWriter()
	service := NewService(stubRepository{
		update: func(_ context.Context, id int, input UpdateInput) (Account, error) {
			updateIDs = append(updateIDs, id)
			if input.State != nil {
				t.Fatalf("repo.Update received State = %q, want nil", *input.State)
			}
			if input.Priority == nil || *input.Priority != priority {
				t.Fatalf("repo.Update Priority = %v, want %d", input.Priority, priority)
			}
			return Account{ID: id, Platform: "openai"}, nil
		},
		listAll: func(_ context.Context, filter ListFilter) ([]Account, error) {
			return []Account{
				{ID: filter.IDs[0], Platform: "openai", State: "disabled"},
				{ID: filter.IDs[1], Platform: "openai", State: "disabled"},
			}, nil
		},
	}, nil, nil, writer)

	result := service.BulkUpdate(t.Context(), BulkUpdateInput{
		IDs:      []int{11, 12},
		State:    &state,
		Priority: &priority,
	})

	if result.Success != 2 || result.Failed != 0 {
		t.Fatalf("BulkUpdate result = %+v, want 2 success", result)
	}
	if len(updateIDs) != 2 || updateIDs[0] != 11 || updateIDs[1] != 12 {
		t.Fatalf("repo.Update IDs = %v, want [11 12]", updateIDs)
	}
	if got := writer.disabled[11]; got != "手动关闭" {
		t.Fatalf("ManualDisable account 11 reason = %q, want 手动关闭", got)
	}
	if got := writer.disabled[12]; got != "手动关闭" {
		t.Fatalf("ManualDisable account 12 reason = %q, want 手动关闭", got)
	}
}

func TestBulkUpdateNoopValidatesWithoutUpdate(t *testing.T) {
	var findIDs []int
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for noop bulk update: %+v", input)
			return Account{}, nil
		},
		findByID: func(_ context.Context, id int, _ LoadOptions) (Account, error) {
			findIDs = append(findIDs, id)
			return Account{ID: id, Platform: "openai"}, nil
		},
		listAll: func(_ context.Context, filter ListFilter) ([]Account, error) {
			t.Fatalf("repo.ListAll should not be called for noop bulk update: %+v", filter)
			return nil, nil
		},
	}, nil, nil, nil)

	result := service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{21, 22}})

	if result.Success != 2 || result.Failed != 0 {
		t.Fatalf("BulkUpdate result = %+v, want 2 success", result)
	}
	if len(findIDs) != 2 || findIDs[0] != 21 || findIDs[1] != 22 {
		t.Fatalf("FindByID IDs = %v, want [21 22]", findIDs)
	}
}

func TestBulkUpdateRejectsInvalidState(t *testing.T) {
	state := "degraded"
	service := NewService(stubRepository{
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			t.Fatalf("repo.Update should not be called for invalid state: %+v", input)
			return Account{}, nil
		},
	}, nil, nil, newStubStateWriter())

	result := service.BulkUpdate(t.Context(), BulkUpdateInput{IDs: []int{31, 32}, State: &state})

	if result.Success != 0 || result.Failed != 2 {
		t.Fatalf("BulkUpdate result = %+v, want 2 failures", result)
	}
	for _, item := range result.Results {
		if item.Success || item.Error != ErrInvalidState.Error() {
			t.Fatalf("result item = %+v, want ErrInvalidState failure", item)
		}
	}
}

type windowStatsStub struct {
	stubRepository
	captured [][]int
	byStart  map[int64]map[int]AccountWindowStats
}

func (s *windowStatsStub) BatchWindowStats(_ context.Context, ids []int, startTime time.Time) (map[int]AccountWindowStats, error) {
	cp := append([]int(nil), ids...)
	s.captured = append(s.captured, cp)
	if s.byStart == nil {
		return nil, nil
	}
	return s.byStart[startTime.Unix()], nil
}

func TestEnrichTodayStats_AttachesAccountLevelStats(t *testing.T) {
	// 2026-04-14 15:30 本地时间 → 今日 00:00 = 2026-04-14 00:00
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	todayStart := time.Date(2026, 4, 14, 0, 0, 0, 0, time.Local).Unix()

	repo := &windowStatsStub{
		byStart: map[int64]map[int]AccountWindowStats{
			todayStart: {
				42: {Requests: 9, Tokens: 242_500, AccountCost: 0.22, UserCost: 0.13},
			},
		},
	}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	// 上游 quota 窗口不影响 today_stats，今日统计是账号级的
	merged := map[string]any{
		"42": map[string]any{
			"windows": []any{
				map[string]any{"key": "5h", "label": "5h", "used_percent": 19.0},
				map[string]any{"key": "7d", "label": "7d", "used_percent": 100.0},
				map[string]any{"key": "5h_spark", "label": "5h Spark", "used_percent": 0.0},
				map[string]any{"key": "7d_spark", "label": "7d Spark", "used_percent": 14.0},
			},
		},
	}
	svc.enrichTodayStats(t.Context(), merged)

	acct := merged["42"].(map[string]any)
	stats, ok := acct["today_stats"].(map[string]any)
	if !ok {
		t.Fatalf("account should have today_stats attached at top level")
	}
	if stats["requests"].(int64) != 9 {
		t.Errorf("requests = %v, want 9", stats["requests"])
	}
	if stats["tokens"].(int64) != 242_500 {
		t.Errorf("tokens = %v, want 242500", stats["tokens"])
	}
	if stats["account_cost"].(float64) != 0.22 {
		t.Errorf("account_cost = %v, want 0.22", stats["account_cost"])
	}
	if stats["user_cost"].(float64) != 0.13 {
		t.Errorf("user_cost = %v, want 0.13", stats["user_cost"])
	}

	// windows 不应该被打上 stats 字段
	windows := acct["windows"].([]any)
	for i, wAny := range windows {
		w := wAny.(map[string]any)
		if _, hasStats := w["stats"]; hasStats {
			t.Errorf("window %d should NOT have stats attached (today_stats lives at account level)", i)
		}
	}
}

func TestEnrichTodayStats_ApikeyPlaceholderGetsStats(t *testing.T) {
	// 回归：apikey 账号在 merged 里只有一个空 map 占位（getUpstreamUsage 里 seed 的），
	// enrichTodayStats 应该能给它填上 today_stats——不能因为没有 windows 就跳过
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	todayStart := time.Date(2026, 4, 14, 0, 0, 0, 0, time.Local).Unix()

	repo := &windowStatsStub{
		byStart: map[int64]map[int]AccountWindowStats{
			todayStart: {
				55: {Requests: 3, Tokens: 1200, AccountCost: 0.05, UserCost: 0.02},
			},
		},
	}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	// 模拟 getUpstreamUsage seed 之后的状态：apikey 账号只有一个空 map
	merged := map[string]any{
		"55": map[string]any{}, // apikey 占位
	}
	svc.enrichTodayStats(t.Context(), merged)

	acct := merged["55"].(map[string]any)
	stats, ok := acct["today_stats"].(map[string]any)
	if !ok {
		t.Fatalf("apikey placeholder account should get today_stats attached")
	}
	if stats["requests"].(int64) != 3 {
		t.Errorf("requests = %v, want 3", stats["requests"])
	}
	if stats["user_cost"].(float64) != 0.02 {
		t.Errorf("user_cost = %v, want 0.02", stats["user_cost"])
	}
}

func TestEnrichTodayStats_ZeroWhenNoRecords(t *testing.T) {
	// 账号今天完全没有请求 → 仍然注入 0 值，前端据此稳定展示
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)

	repo := &windowStatsStub{byStart: map[int64]map[int]AccountWindowStats{}}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	merged := map[string]any{
		"99": map[string]any{
			"windows": []any{
				map[string]any{"key": "5h", "label": "5h", "used_percent": 0.0},
			},
		},
	}
	svc.enrichTodayStats(t.Context(), merged)

	stats := merged["99"].(map[string]any)["today_stats"].(map[string]any)
	if stats["requests"].(int64) != 0 {
		t.Errorf("requests = %v, want 0", stats["requests"])
	}
	if stats["account_cost"].(float64) != 0 {
		t.Errorf("account_cost = %v, want 0", stats["account_cost"])
	}
}

func TestCloneMergedShallow_IsolatesCachedEntry(t *testing.T) {
	// 回归测试：克隆体写 today_stats 不能污染缓存里的原始 map
	cached := map[string]any{
		"42": map[string]any{
			"windows": []any{map[string]any{"key": "5h"}},
		},
	}
	clone := cloneMergedShallow(cached)
	cloneAcct := clone["42"].(map[string]any)
	cloneAcct["today_stats"] = map[string]any{"requests": int64(99)}

	// 缓存里的 account map 不应该出现 today_stats
	origAcct := cached["42"].(map[string]any)
	if _, leaked := origAcct["today_stats"]; leaked {
		t.Fatalf("today_stats leaked into cached map — cloneMergedShallow is not deep enough")
	}
}

func TestEnrichTodayStats_BatchesAllAccountsInOneQuery(t *testing.T) {
	// 多个账号应该在一次 BatchWindowStats 调用里一起查
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	todayStart := time.Date(2026, 4, 14, 0, 0, 0, 0, time.Local).Unix()

	repo := &windowStatsStub{
		byStart: map[int64]map[int]AccountWindowStats{
			todayStart: {
				1: {Requests: 3},
				2: {Requests: 5},
			},
		},
	}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	merged := map[string]any{
		"1": map[string]any{"windows": []any{}},
		"2": map[string]any{"windows": []any{}},
		"3": map[string]any{"windows": []any{}},
	}
	svc.enrichTodayStats(t.Context(), merged)

	if len(repo.captured) != 1 {
		t.Fatalf("expected exactly 1 BatchWindowStats call, got %d", len(repo.captured))
	}
	if len(repo.captured[0]) != 3 {
		t.Errorf("expected all 3 account IDs in one call, got %v", repo.captured[0])
	}
}

func TestGetAccountUsage_NoCacheReturnsSeededStatsWhileRefreshing(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	todayStart := time.Date(2026, 4, 14, 0, 0, 0, 0, time.Local).Unix()

	repo := &windowStatsStub{
		stubRepository: stubRepository{
			listAll: func(_ context.Context, filter ListFilter) ([]Account, error) {
				if filter.Platform != "openai" {
					t.Fatalf("Platform = %q, want openai", filter.Platform)
				}
				if len(filter.IDs) != 1 || filter.IDs[0] != 55 {
					t.Fatalf("IDs = %v, want [55]", filter.IDs)
				}
				return []Account{{ID: 55, Platform: "openai", Type: "oauth"}}, nil
			},
		},
		byStart: map[int64]map[int]AccountWindowStats{
			todayStart: {
				55: {Requests: 3, Tokens: 1200, AccountCost: 0.05, UserCost: 0.02},
			},
		},
	}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }

	usage, refreshing, err := svc.GetAccountUsage(t.Context(), "openai", []int{55}, false)
	if err != nil {
		t.Fatalf("GetAccountUsage returned error: %v", err)
	}
	if !refreshing {
		t.Fatalf("expected cache miss to report refreshing")
	}
	acct, ok := usage["55"].(map[string]any)
	if !ok {
		t.Fatalf("expected seeded account 55, got %#v", usage["55"])
	}
	stats, ok := acct["today_stats"].(map[string]any)
	if !ok {
		t.Fatalf("expected today_stats on seeded account")
	}
	if stats["requests"].(int64) != 3 {
		t.Fatalf("requests = %v, want 3", stats["requests"])
	}
}

func TestGetAccountUsage_ExpiredMemoryCacheReturnsStaleWindowsAndRefreshes(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	repo := &windowStatsStub{
		stubRepository: stubRepository{
			listAll: func(context.Context, ListFilter) ([]Account, error) {
				return []Account{{ID: 42, Platform: "openai", Type: "oauth"}}, nil
			},
		},
		byStart: map[int64]map[int]AccountWindowStats{},
	}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	svc.setUsageInfoMemoryCache(42, "openai", AccountUsageInfo{
		Windows: []AccountUsageWindow{{
			Key:          "5h",
			Label:        "5h",
			DisplayLabel: "5h",
			Slot:         "5h",
			Group:        "base",
			UsedPercent:  27,
			ResetAt:      now.Add(time.Hour).Format(time.RFC3339),
		}},
	}, now.Add(-time.Hour), now.Add(-time.Second))

	usage, refreshing, err := svc.GetAccountUsage(t.Context(), "openai", []int{42}, false)
	if err != nil {
		t.Fatalf("GetAccountUsage returned error: %v", err)
	}
	if !refreshing {
		t.Fatalf("expected stale cache to report refreshing")
	}
	acct := usage["42"].(map[string]any)
	windows := acct["windows"].([]any)
	if len(windows) != 1 {
		t.Fatalf("expected stale window to be returned, got %d", len(windows))
	}
	if got := windows[0].(map[string]any)["used_percent"]; got != float64(27) {
		t.Fatalf("used_percent = %v, want 27", got)
	}
}

func TestGetAccountUsage_EmptyMemoryCacheRefreshes(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	repo := &windowStatsStub{
		stubRepository: stubRepository{
			listAll: func(_ context.Context, filter ListFilter) ([]Account, error) {
				if len(filter.IDs) != 1 || filter.IDs[0] != 77 {
					t.Fatalf("IDs = %v, want [77]", filter.IDs)
				}
				return []Account{{ID: 77, Platform: "openai", Type: "oauth"}}, nil
			},
		},
		byStart: map[int64]map[int]AccountWindowStats{},
	}
	svc := NewService(repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	svc.setUsageInfoMemoryCache(77, "openai", AccountUsageInfo{}, now, now.Add(time.Hour))

	usage, refreshing, err := svc.GetAccountUsage(t.Context(), "openai", []int{77}, false)
	if err != nil {
		t.Fatalf("GetAccountUsage returned error: %v", err)
	}
	if !refreshing {
		t.Fatalf("expected empty cache to report refreshing")
	}
	if _, ok := usage["77"].(map[string]any); !ok {
		t.Fatalf("expected seeded account 77, got %#v", usage["77"])
	}
}

func TestInvalidateUsageCacheClearsMemoryEntriesForPlatform(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	svc := NewService(stubRepository{}, nil, nil, nil)
	svc.now = func() time.Time { return now }
	svc.setUsageInfoMemoryCache(42, "openai", AccountUsageInfo{
		Windows: []AccountUsageWindow{{Key: "5h", Label: "5h", UsedPercent: 10}},
	}, now, now.Add(time.Hour))
	svc.setUsageInfoMemoryCache(7, "claude", AccountUsageInfo{
		Windows: []AccountUsageWindow{{Key: "5h", Label: "5h", UsedPercent: 20}},
	}, now, now.Add(time.Hour))

	svc.InvalidateUsageCache("openai")

	if _, _, ok := svc.getUsageInfoMemoryCache(42); ok {
		t.Fatalf("openai memory usage entry should be cleared")
	}
	if _, _, ok := svc.getUsageInfoMemoryCache(7); !ok {
		t.Fatalf("unrelated platform memory usage entry should be kept")
	}
}

func TestUpdateAccountUsageCacheUpdatesMemoryEntry(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.Local)
	svc := NewService(stubRepository{}, nil, nil, nil)
	svc.now = func() time.Time { return now }
	svc.updateAccountUsageCache(t.Context(), "openai", 42, AccountUsageInfo{
		Windows: []AccountUsageWindow{{
			Key:          "5h",
			Label:        "5h",
			UsedPercent:  66,
			ResetSeconds: int64(time.Hour.Seconds()),
		}},
	})

	info, _, ok := svc.getUsageInfoMemoryCache(42)
	if !ok {
		t.Fatalf("memory usage entry should exist")
	}
	if got := info.Windows[0].UsedPercent; got != 66 {
		t.Fatalf("memory usage percent = %v, want 66", got)
	}
}

func TestExtractBodyError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "Anthropic standard nested error",
			body: `{"error":{"type":"authentication_error","message":"Invalid x-api-key"}}`,
			want: "authentication_error: Invalid x-api-key",
		},
		{
			name: "nested error with only message",
			body: `{"error":{"message":"rate limited"}}`,
			want: "rate limited",
		},
		{
			name: "nested error with only type",
			body: `{"error":{"type":"overloaded"}}`,
			want: "overloaded",
		},
		{
			name: "error as plain string",
			body: `{"error":"upstream gone"}`,
			want: "upstream gone",
		},
		{
			name: "top-level code + message (pool format)",
			body: `{"code":"INVALID_API_KEY","message":"Invalid API key"}`,
			want: "INVALID_API_KEY: Invalid API key",
		},
		{
			name: "top-level only message",
			body: `{"message":"something broke"}`,
			want: "something broke",
		},
		{
			name: "top-level only code",
			body: `{"code":"BAD_REQUEST"}`,
			want: "BAD_REQUEST",
		},
		{
			name: "empty body",
			body: ``,
			want: "",
		},
		{
			name: "non-JSON body",
			body: `<html>500 Internal Server Error</html>`,
			want: "",
		},
		{
			name: "unrelated JSON",
			body: `{"foo":"bar"}`,
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractBodyError([]byte(c.body))
			if got != c.want {
				t.Errorf("extractBodyError(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

func TestConnectivityTestErrorMessage(t *testing.T) {
	cases := []struct {
		name    string
		outcome sdk.ForwardOutcome
		want    string
	}{
		{
			name: "优先透传上游错误体",
			outcome: sdk.ForwardOutcome{
				Kind: sdk.OutcomeClientError,
				Upstream: sdk.UpstreamResponse{
					StatusCode: http.StatusBadRequest,
					Body:       []byte(`{"error":{"message":"model not supported"}}`),
				},
				Reason: "HTTP 400: fallback reason",
			},
			want: "HTTP 400: model not supported",
		},
		{
			name: "客户端错误可用 reason 兜底",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeClientError,
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadRequest},
				Reason:   "The model is not supported.",
			},
			want: "HTTP 400: The model is not supported.",
		},
		{
			name: "空流诊断不直接展示给用户",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeUpstreamTransient,
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
				Reason:   "上游流式响应为空：已收到完成事件但没有文本、工具调用或响应输出",
			},
			want: "上游未返回有效响应，请检查测试模型是否被该上游账号支持或查看上游日志",
		},
		{
			name: "上游暂时错误保留原因",
			outcome: sdk.ForwardOutcome{
				Kind:     sdk.OutcomeUpstreamTransient,
				Upstream: sdk.UpstreamResponse{StatusCode: http.StatusBadGateway},
				Reason:   "HTTP 502: upstream secret request ID 349f8894",
			},
			want: "上游服务暂不可用: HTTP 502: upstream secret request ID 349f8894",
		},
		{
			name: "账号限流使用统一提示",
			outcome: sdk.ForwardOutcome{
				Kind:       sdk.OutcomeAccountRateLimited,
				RetryAfter: 3 * time.Minute,
				Reason:     "HTTP 429: The usage limit has been reached",
			},
			want: "上游账号当前被限流，请在 3m0s 后重试",
		},
		{
			name: "账号失效使用统一提示",
			outcome: sdk.ForwardOutcome{
				Kind:   sdk.OutcomeAccountDead,
				Reason: "HTTP 401: Your authentication token has been invalidated",
			},
			want: "上游账号不可用: HTTP 401: Your authentication token has been invalidated",
		},
		{
			name: "账号暂时不可用使用统一提示",
			outcome: sdk.ForwardOutcome{
				Kind:   sdk.OutcomeAccountUnavailable,
				Reason: "HTTP 403: 访问被拒绝，账号暂不可用或无权限",
			},
			want: "上游账号403暂不可用: HTTP 403: 访问被拒绝，账号暂不可用或无权限",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := connectivityTestErrorMessage(c.outcome); got != c.want {
				t.Fatalf("connectivityTestErrorMessage() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestPersistRateLimitFromWindows(t *testing.T) {
	writer := newStubStateWriter()
	svc := NewService(stubRepository{}, nil, nil, writer)

	accounts := map[string]any{
		// 7d 100% + 另一个 5h 99%：取 7d 的 reset_seconds 做恢复时间
		"42": map[string]any{
			"windows": []any{
				map[string]any{"key": "5h", "used_percent": 99.0, "reset_seconds": float64(300)},
				map[string]any{"key": "7d", "used_percent": 100.0, "reset_seconds": float64(34800)}, // 9h 40m
			},
		},
		// 两个窗口都 100%：取两者中较晚的 reset
		"7": map[string]any{
			"windows": []any{
				map[string]any{"key": "5h", "used_percent": 100.0, "reset_seconds": float64(1200)},
				map[string]any{"key": "7d", "used_percent": 100.0, "reset_seconds": float64(3600)},
			},
		},
		// 全部 <100%：清空
		"3": map[string]any{
			"windows": []any{
				map[string]any{"key": "5h", "used_percent": 42.0, "reset_seconds": float64(600)},
			},
		},
		// 插件显式声明忽略限流：即使用量超过 100%，也不写 rate_limited
		"9": map[string]any{
			"windows": []any{
				map[string]any{"key": "monthly", "used_percent": 180.0, "reset_seconds": float64(3600), "ignore_limit": true},
			},
		},
		// 无 windows：跳过
		"1": map[string]any{},
	}

	svc.persistRateLimitFromWindows(t.Context(), accounts)

	if got, ok := writer.rateLimited[42]; !ok || got == nil {
		t.Fatalf("expected account 42 to be MarkRateLimited, got %+v", got)
	} else if until := time.Until(*got); until < 9*time.Hour+30*time.Minute || until > 9*time.Hour+50*time.Minute {
		t.Errorf("account 42 reset expected ~9h40m, got %s", until)
	}

	if got, ok := writer.rateLimited[7]; !ok || got == nil {
		t.Fatalf("expected account 7 to be MarkRateLimited, got %+v", got)
	} else if until := time.Until(*got); until < 55*time.Minute || until > 65*time.Minute {
		t.Errorf("account 7 should take LATER of two resets (~1h), got %s", until)
	}

	if !writer.cleared[3] {
		t.Errorf("account 3 should have ClearRateLimited called")
	}
	if _, ok := writer.rateLimited[9]; ok {
		t.Errorf("account 9 uses ignore_limit, should not call MarkRateLimited")
	}
	if !writer.cleared[9] {
		t.Errorf("account 9 uses ignore_limit, should have ClearRateLimited called")
	}
	if _, ok := writer.rateLimited[1]; ok {
		t.Errorf("account 1 has no windows, should not call MarkRateLimited")
	}
	if writer.cleared[1] {
		t.Errorf("account 1 has no windows, should not call ClearRateLimited")
	}
}

func TestAccountServiceSimpleOperationsAndCapacity(t *testing.T) {
	concurrency := &captureConcurrency{counts: map[int]int{2: 5}}
	service := NewService(stubRepository{}, nil, concurrency, nil)

	capacity := service.GetCapacity(t.Context(), []int{0, 2, 2, 3})
	if len(capacity) != 2 || capacity[2] != 5 || capacity[3] != 0 {
		t.Fatalf("capacity = %+v, want account 2=5 and account 3=0", capacity)
	}
	if len(concurrency.captured) != 2 || concurrency.captured[0] != 2 || concurrency.captured[1] != 3 {
		t.Fatalf("captured capacity IDs = %v, want [2 3]", concurrency.captured)
	}
	if got := NewService(stubRepository{}, nil, nil, nil).GetCapacity(t.Context(), []int{4, 4}); got[4] != 0 {
		t.Fatalf("nil concurrency capacity = %+v, want 4=0", got)
	}

	var exportFilter ListFilter
	exported := []Account{{ID: 9, Name: "exported"}}
	service = NewService(stubRepository{
		listAll: func(_ context.Context, filter ListFilter) ([]Account, error) {
			exportFilter = filter
			return exported, nil
		},
	}, stubPluginCatalog{}, nil, nil)
	gotExported, err := service.ExportAll(t.Context(), ListFilter{Platform: "openai"})
	if err != nil {
		t.Fatalf("ExportAll returned error: %v", err)
	}
	if len(gotExported) != 1 || gotExported[0].ID != 9 || exportFilter.Platform != "openai" {
		t.Fatalf("ExportAll result = %+v filter=%+v", gotExported, exportFilter)
	}

	var deleted []int
	service = NewService(stubRepository{
		delete: func(_ context.Context, id int) error {
			deleted = append(deleted, id)
			if id == 2 {
				return errors.New("delete failed")
			}
			return nil
		},
	}, nil, nil, nil)
	if err := service.Delete(t.Context(), 1); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	result := service.BulkDelete(t.Context(), []int{1, 2, 3})
	if result.Success != 2 || result.Failed != 1 || !sameIDs(result.SuccessIDs, []int{1, 3}) || !sameIDs(result.FailedIDs, []int{2}) {
		t.Fatalf("BulkDelete result = %+v", result)
	}
	if !sameIDs(deleted, []int{1, 1, 2, 3}) {
		t.Fatalf("deleted IDs = %v", deleted)
	}
}

func TestBulkUpdateMergesExtraPatch(t *testing.T) {
	var captured UpdateInput
	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, _ LoadOptions) (Account, error) {
			return Account{ID: id, Extra: map[string]any{"keep": "old", "replace": "old"}}, nil
		},
		update: func(_ context.Context, _ int, input UpdateInput) (Account, error) {
			captured = input
			return Account{ID: 7, Platform: "openai", Extra: input.Extra}, nil
		},
		listAll: func(context.Context, ListFilter) ([]Account, error) {
			return []Account{{ID: 7, Platform: "openai"}}, nil
		},
	}, nil, nil, nil)

	result := service.BulkUpdate(t.Context(), BulkUpdateInput{
		IDs:      []int{7},
		Extra:    map[string]any{"replace": "new", "added": float64(3)},
		HasExtra: true,
	})
	if result.Success != 1 || result.Failed != 0 {
		t.Fatalf("BulkUpdate result = %+v, want success", result)
	}
	if !captured.HasExtra || captured.Extra["keep"] != "old" || captured.Extra["replace"] != "new" || captured.Extra["added"] != float64(3) {
		t.Fatalf("captured Extra = %+v", captured.Extra)
	}

	merged := mergeAnyMap(map[string]any{"a": 1, "b": 2}, map[string]any{"b": 3})
	if merged["a"] != 1 || merged["b"] != 3 {
		t.Fatalf("mergeAnyMap = %+v", merged)
	}
}

func TestGetStatsUsesResolvedRangeAndRepositoryLogs(t *testing.T) {
	now := time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC)
	var capturedStart, capturedEnd time.Time
	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, _ LoadOptions) (Account, error) {
			return Account{ID: id, Name: "stats", Platform: "openai", State: "active"}, nil
		},
		findUsageLogs: func(_ context.Context, id int, start, end time.Time) ([]UsageLog, error) {
			if id != 42 {
				t.Fatalf("FindUsageLogs id = %d, want 42", id)
			}
			capturedStart, capturedEnd = start, end
			return []UsageLog{{Model: "gpt-5", InputTokens: 10, OutputTokens: 5, CreatedAt: now}}, nil
		},
	}, nil, nil, nil)
	service.now = func() time.Time { return now }

	stats, err := service.GetStats(t.Context(), 42, StatsQuery{StartDate: "2026-06-19", EndDate: "2026-06-20", TZ: "UTC"})
	if err != nil {
		t.Fatalf("GetStats returned error: %v", err)
	}
	if stats.AccountID != 42 || stats.Range.Count != 1 || capturedStart.Format("2006-01-02") != "2026-06-19" || capturedEnd.Format("2006-01-02") != "2026-06-20" {
		t.Fatalf("stats=%+v start=%v end=%v", stats, capturedStart, capturedEnd)
	}
}

func TestParseSingleAccountUsagePluginResponse(t *testing.T) {
	info, usageErrors, ok := parseSingleAccountUsagePluginResponse(7, []byte(`{
		"accounts":{"7":{"updated_at":"2026-06-20T00:00:00Z","windows":[{"key":"5h","label":"5h","used_percent":12,"reset_seconds":60}]}},
		"errors":[{"id":9,"message":"ignored"}]
	}`))
	if !ok || len(info.Windows) != 1 || info.Windows[0].Key != "5h" || len(usageErrors) != 1 {
		t.Fatalf("accounts response info=%+v errors=%+v ok=%v", info, usageErrors, ok)
	}

	_, usageErrors, ok = parseSingleAccountUsagePluginResponse(7, []byte(`{"errors":[{"id":7,"message":"bad token"}]}`))
	if ok || len(usageErrors) != 1 || usageErrors[0].Message != "bad token" {
		t.Fatalf("errors-only response errors=%+v ok=%v", usageErrors, ok)
	}

	info, usageErrors, ok = parseSingleAccountUsagePluginResponse(7, []byte(`{"credits":{"balance":4.5,"unlimited":true}}`))
	if !ok || info.Credits == nil || !info.Credits.Unlimited || len(usageErrors) != 0 {
		t.Fatalf("direct response info=%+v errors=%+v ok=%v", info, usageErrors, ok)
	}

	for _, body := range [][]byte{[]byte(`{bad json`), []byte(`{}`)} {
		if info, usageErrors, ok := parseSingleAccountUsagePluginResponse(7, body); ok || len(usageErrors) != 0 || accountUsageInfoHasData(info) {
			t.Fatalf("invalid/empty body %q parsed as info=%+v errors=%+v ok=%v", string(body), info, usageErrors, ok)
		}
	}
}

func TestHandleSingleAccountUsageErrors(t *testing.T) {
	writer := newStubStateWriter()
	service := NewService(stubRepository{}, nil, nil, writer)

	service.handleSingleAccountUsageErrors(t.Context(), Account{ID: 7, State: "active"}, []accountUsageError{
		{ID: 8, Message: "wrong account"},
		{ID: 7},
	})
	if len(writer.disabled) != 0 || len(writer.degraded) != 0 {
		t.Fatalf("unexpected state changes for ignored errors: disabled=%+v degraded=%+v", writer.disabled, writer.degraded)
	}

	service.handleSingleAccountUsageErrors(t.Context(), Account{ID: 7, State: "active"}, []accountUsageError{{ID: 7, Message: "HTTP 403: forbidden"}})
	if writer.degraded[7] != "HTTP 403: forbidden" {
		t.Fatalf("forbidden usage error should degrade account, got %+v", writer.degraded)
	}

	service.handleSingleAccountUsageErrors(t.Context(), Account{ID: 9, UpstreamIsPool: true}, []accountUsageError{{ID: 9, Message: "HTTP 401"}})
	service.handleSingleAccountUsageErrors(t.Context(), Account{ID: 10, State: "disabled"}, []accountUsageError{{ID: 10, Message: "HTTP 401"}})
	if _, ok := writer.disabled[9]; ok {
		t.Fatal("pool account should not be disabled")
	}
	if _, ok := writer.disabled[10]; ok {
		t.Fatal("disabled account should not be disabled again")
	}
}

func TestUsageCacheMemoryHelpers(t *testing.T) {
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	service := NewService(stubRepository{}, nil, nil, nil)
	service.now = func() time.Time { return now }

	if got := usageCachePlatformKey("  "); got != "__all__" {
		t.Fatalf("blank platform key = %q", got)
	}
	if got := normalizeAccountIDs([]int{3, 0, 3, 2}); !sameIDs(got, []int{3, 2}) {
		t.Fatalf("normalizeAccountIDs = %v", got)
	}
	if got := usageCacheAccountIDsRefreshKey(" openai ", []int{5, 2, 5}); got != "openai:accounts:2,5" {
		t.Fatalf("usageCacheAccountIDsRefreshKey = %q", got)
	}

	info := AccountUsageInfo{Windows: []AccountUsageWindow{{Key: "5h", Label: "5h", ResetAt: now.Add(time.Hour).Format(time.RFC3339)}}}
	service.setUsageInfoMemoryCache(1, "openai", info, now, now.Add(time.Hour))
	writes := []accountUsageCacheWrite{{account: Account{ID: 1}, info: info}, {account: Account{ID: 2}, info: info}}
	existing := service.getUsageInfosForCacheWrites(t.Context(), writes, now)
	if len(existing) != 1 || len(existing[1].Windows) != 1 {
		t.Fatalf("existing cache writes = %+v", existing)
	}
	if empty := service.getUsageInfosForCacheWrites(t.Context(), nil, now); len(empty) != 0 {
		t.Fatalf("empty cache writes = %+v", empty)
	}

	service.updateAccountUsageCaches(t.Context(),
		[]Account{{ID: 1, Platform: "openai", Type: "oauth"}, {ID: 2, Platform: "openai", Type: "apikey"}, {ID: 3, Platform: "openai", Type: "oauth", State: "disabled"}},
		map[string]AccountUsageInfo{"1": {Credits: &AccountUsageCredits{Balance: 2}}, "2": info, "3": info},
	)
	cached, _, ok := service.getUsageInfoMemoryCache(1)
	if !ok || cached.Credits == nil || cached.Credits.Balance != 2 || len(cached.Windows) != 1 {
		t.Fatalf("merged cached account 1 = %+v ok=%v", cached, ok)
	}
	if _, _, ok := service.getUsageInfoMemoryCache(2); ok {
		t.Fatal("apikey account should not be cached by updateAccountUsageCaches")
	}

	service.writeUsageInfoCache(t.Context(), "openai", 1, AccountUsageInfo{}, now)
	if _, _, ok := service.getUsageInfoMemoryCache(1); ok {
		t.Fatal("empty usage info should delete memory cache")
	}
	service.updateAccountUsageCache(t.Context(), "openai", 0, info)
	if _, _, ok := service.getUsageInfoMemoryCache(0); ok {
		t.Fatal("accountID <= 0 should not create memory cache")
	}

	service.setUsageInfoMemoryCache(4, "openai", info, now.Add(-2*time.Hour), now.Add(-time.Hour))
	byAccount, missing := service.getUsageInfosForAccounts(t.Context(), "openai", []Account{
		{ID: 4, Platform: "openai", Type: "oauth"},
		{ID: 5, Platform: "openai", Type: "oauth"},
		{ID: 6, Platform: "openai", Type: "apikey"},
	})
	if len(byAccount) != 1 || len(byAccount[4].Windows) != 1 || len(missing) != 2 || missing[0].ID != 4 || missing[1].ID != 5 {
		t.Fatalf("getUsageInfosForAccounts byAccount=%+v missing=%+v", byAccount, missing)
	}
}

func TestUsageWindowNumberAndResetHelpers(t *testing.T) {
	now := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour).Format(time.RFC3339)
	if got := parseWindowReset(map[string]any{"reset_at": resetAt}, now); got == nil || got.UTC().Format(time.RFC3339) != resetAt {
		t.Fatalf("reset_at parse = %v", got)
	}
	if got := parseWindowReset(map[string]any{"reset_seconds": int64(30)}, now); got == nil || got.Sub(now) != 30*time.Second {
		t.Fatalf("reset_seconds parse = %v", got)
	}
	if got := parseWindowReset(map[string]any{"reset_after_seconds": int32(45)}, now); got == nil || got.Sub(now) != 45*time.Second {
		t.Fatalf("reset_after_seconds parse = %v", got)
	}
	if got := parseWindowReset(map[string]any{"reset_at": "bad"}, now); got != nil {
		t.Fatalf("bad reset parse = %v", got)
	}
	if !usageWindowIgnoresLimit(map[string]any{"enforce_limit": false}) {
		t.Fatal("enforce_limit=false should ignore limit")
	}
	for _, value := range []any{float64(1.5), float32(2.5), 3, int64(4), int32(5), json.Number("6.5")} {
		if _, ok := usageNumber(value); !ok {
			t.Fatalf("usageNumber(%T) returned !ok", value)
		}
	}
	if _, ok := usageNumber("7"); ok {
		t.Fatal("usageNumber string should not be accepted")
	}
	if _, ok := usageNumber(json.Number("bad")); ok {
		t.Fatal("usageNumber bad json.Number should not be accepted")
	}
}

func TestAccountProfileCacheAndRedisValueHelpers(t *testing.T) {
	stateUntil := time.Date(2026, 6, 20, 11, 0, 0, 0, time.FixedZone("CST", 8*3600))
	lastUsed := stateUntil.Add(-time.Hour)
	item := Account{
		ID: 8, Name: "profile", Platform: "openai", Type: "oauth", State: "rate_limited",
		StateUntil: &stateUntil, LastUsedAt: &lastUsed, Priority: 9, MaxConcurrency: 10,
		RateMultiplier: 1.5, ErrorMsg: "limited", UpstreamIsPool: true, GroupIDs: []int64{1, 2},
		Proxy: &Proxy{ID: 77}, CreatedAt: lastUsed, UpdatedAt: stateUntil,
	}
	payload := accountProfileCacheFromAccount(item)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal profile payload: %v", err)
	}
	decoded, ok := decodeAccountProfileCache(body, item.ID)
	if !ok || decoded.ID != item.ID {
		t.Fatalf("decodeAccountProfileCache = %+v ok=%v", decoded, ok)
	}
	account, ok := accountProfileCacheToAccount(decoded)
	if !ok || account.ID != item.ID || account.Proxy == nil || account.Proxy.ID != 77 ||
		account.StateUntil == nil || account.LastUsedAt == nil || account.Credentials == nil {
		t.Fatalf("accountProfileCacheToAccount = %+v ok=%v", account, ok)
	}
	if _, ok := decodeAccountProfileCache([]byte(`{bad`), item.ID); ok {
		t.Fatal("bad JSON profile cache decoded successfully")
	}
	if _, ok := decodeAccountProfileCache(body, 99); ok {
		t.Fatal("wrong account ID profile cache decoded successfully")
	}
	if _, ok := accountProfileCacheToAccount(accountProfileCachePayload{}); ok {
		t.Fatal("zero ID profile payload should be invalid")
	}
	if !parseAccountProfileCacheTime("bad").IsZero() {
		t.Fatal("bad profile time should parse to zero time")
	}

	if got, ok := redisValueBytes("abc"); !ok || string(got) != "abc" {
		t.Fatalf("redisValueBytes string = %q ok=%v", string(got), ok)
	}
	if got, ok := redisValueBytes([]byte("def")); !ok || string(got) != "def" {
		t.Fatalf("redisValueBytes bytes = %q ok=%v", string(got), ok)
	}
	if _, ok := redisValueBytes(123); ok {
		t.Fatal("redisValueBytes int should fail")
	}
	for _, value := range []any{int64(1), 2, "3", []byte("4")} {
		if _, ok := redisValueInt64(value); !ok {
			t.Fatalf("redisValueInt64(%T) returned !ok", value)
		}
	}
	if _, ok := redisValueInt64("bad"); ok {
		t.Fatal("redisValueInt64 bad string should fail")
	}
	for _, value := range []any{float64(1.2), float32(2.3), int64(3), 4, "5.5", []byte("6.5")} {
		if _, ok := redisValueFloat64(value); !ok {
			t.Fatalf("redisValueFloat64(%T) returned !ok", value)
		}
	}
	if _, ok := redisValueFloat64("bad"); ok {
		t.Fatal("redisValueFloat64 bad string should fail")
	}

	stats, ok := parseCachedTodayStats([]any{"3", []byte("100"), "1.25", float64(2.5), "ignored"})
	if !ok || stats.Requests != 3 || stats.Tokens != 100 || stats.AccountCost != 1.25 || stats.UserCost != 2.5 {
		t.Fatalf("parseCachedTodayStats = %+v ok=%v", stats, ok)
	}
	if _, ok := parseCachedTodayStats([]any{nil, "1", "2", "3"}); ok {
		t.Fatal("nil cached today stats should fail")
	}
	if _, ok := parseCachedTodayStats([]any{"1", "2", "3"}); ok {
		t.Fatal("short cached today stats should fail")
	}

	source := map[string]any{"1": map[string]any{"windows": []any{"same"}}, "raw": "value"}
	cloned := cloneMergedShallow(source)
	cloned["1"].(map[string]any)["today_stats"] = map[string]any{"requests": int64(1)}
	if _, leaked := source["1"].(map[string]any)["today_stats"]; leaked || cloned["raw"] != "value" {
		t.Fatalf("cloneMergedShallow source=%+v clone=%+v", source, cloned)
	}
}

func TestGetCredentialsSchemaUsesPluginAndFallbacks(t *testing.T) {
	service := NewService(stubRepository{}, stubPluginCatalog{
		accountTypes: []sdk.AccountType{{
			Key: "oauth", Label: "OAuth", Description: "OAuth account",
			Fields: []sdk.CredentialField{{Key: "refresh_token", Label: "Refresh Token", Type: "password", Required: true, Placeholder: "rt", EditDisabled: true}},
		}},
	}, nil, nil)
	schema := service.GetCredentialsSchema("custom")
	if len(schema.AccountTypes) != 1 || len(schema.Fields) != 1 || schema.Fields[0].Key != "refresh_token" || !schema.Fields[0].EditDisabled {
		t.Fatalf("account type schema = %+v", schema)
	}

	service = NewService(stubRepository{}, stubPluginCatalog{
		credentialFields: []sdk.CredentialField{{Key: "api_key", Label: "API Key", Type: "password", Required: true}},
	}, nil, nil)
	schema = service.GetCredentialsSchema("flat")
	if len(schema.AccountTypes) != 0 || len(schema.Fields) != 1 || schema.Fields[0].Key != "api_key" {
		t.Fatalf("flat credential schema = %+v", schema)
	}

	service = NewService(stubRepository{}, stubPluginCatalog{}, nil, nil)
	if schema := service.GetCredentialsSchema("openai"); len(schema.Fields) != 2 || schema.Fields[0].Key != "api_key" {
		t.Fatalf("openai fallback schema = %+v", schema)
	}
	if schema := service.GetCredentialsSchema("unknown"); len(schema.Fields) != 2 || schema.Fields[1].Key != "base_url" {
		t.Fatalf("generic fallback schema = %+v", schema)
	}
}

type captureConcurrency struct {
	counts   map[int]int
	captured []int
}

func (c *captureConcurrency) GetCurrentCounts(_ context.Context, ids []int) map[int]int {
	c.captured = append([]int(nil), ids...)
	return c.counts
}

func (c *captureConcurrency) GetWorkingCounts(context.Context) map[int]int {
	return c.counts
}

func sameIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
