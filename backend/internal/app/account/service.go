package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
	"github.com/DevilGenius/airgate-core/internal/pkg/httperrors"
	"github.com/DevilGenius/airgate-core/internal/pkg/ratevalue"
	"github.com/DevilGenius/airgate-core/internal/pkg/timezone"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/safego"
)

// PluginCatalog 账号域需要的插件能力集合。
type PluginCatalog interface {
	GetPluginByPlatform(string) *plugin.PluginInstance
	GetModels(string) []sdk.ModelInfo
	GetAccountTypes(string) []sdk.AccountType
	GetCredentialFields(string) []sdk.CredentialField
	GetAllPluginMeta() []plugin.PluginMeta
}

// ConcurrencyReader 并发读接口。
type ConcurrencyReader interface {
	GetCurrentCounts(context.Context, []int) map[int]int
	GetWorkingCounts(context.Context) map[int]int
}

// Service 提供账号域用例编排。
// usageCacheEntry 用量缓存条目
type usageCacheEntry struct {
	platform  string
	info      AccountUsageInfo
	fetchedAt time.Time
	expiresAt time.Time
}

const usageCacheMaxTTL = 5 * time.Hour

const usageCacheMinimumTTL = time.Second

const usageAccountsProbeBatchSize = 10

const usageCacheWriteTimeout = 5 * time.Second

const autoQuotaRefreshInterval = 6 * time.Hour

type accountProfileCachePayload struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Platform       string  `json:"platform"`
	Type           string  `json:"type"`
	State          string  `json:"state"`
	StateUntil     string  `json:"state_until,omitempty"`
	Priority       int     `json:"priority"`
	MaxConcurrency int     `json:"max_concurrency"`
	RateMultiplier float64 `json:"rate_multiplier"`
	ErrorMsg       string  `json:"error_msg,omitempty"`
	UpstreamIsPool bool    `json:"upstream_is_pool"`
	LastUsedAt     string  `json:"last_used_at,omitempty"`
	GroupIDs       []int64 `json:"group_ids,omitempty"`
	ProxyID        *int    `json:"proxy_id,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// StateWriter 管理员巡检场景下对账号状态的写入口。
// 由 scheduler 包实现；让 account service 不直接依赖 scheduler。
type StateWriter interface {
	// MarkRateLimited 把账号打入 rate_limited 状态直到 until。
	MarkRateLimited(ctx context.Context, accountID int, until time.Time, reason string)
	// ClearRateLimited 账号已从限流中恢复，回到 active。
	ClearRateLimited(ctx context.Context, accountID int)
	// MarkDisabled 永久禁用（凭证失效等，需要人工重新验证）。
	MarkDisabled(ctx context.Context, accountID int, reason string)
	// MarkDegraded 临时降级（如上游 403 暂不可用），不会永久禁用账号。
	MarkDegraded(ctx context.Context, accountID int, reason string)
	// ManualRecover 手动恢复到 active 并刷新调度 RouteGraph。
	ManualRecover(ctx context.Context, accountID int) error
	// ManualDisable 手动禁用并刷新调度 RouteGraph。
	ManualDisable(ctx context.Context, accountID int, reason string) error
	// RefreshRouteGraphAccount 刷新账号在调度 RouteGraph 中的静态快照。
	RefreshRouteGraphAccount(ctx context.Context, accountID int)
}

type Service struct {
	repo        Repository
	plugins     PluginCatalog
	concurrency ConcurrencyReader
	stateWriter StateWriter
	monitor     monitoring.Recorder
	now         func() time.Time

	usageMu    sync.RWMutex
	usageCache map[int]*usageCacheEntry
	usageRedis *redis.Client

	usageRefreshMu      sync.Mutex
	usageRefreshRunning map[string]struct{}

	// usageFlight 把同账号的并发 probe 合并为一次上游调用，避免重复打插件。
	usageFlight singleflight.Group
}

// NewService 创建账号服务。
// stateWriter 可传 nil（测试场景）；nil 时额度巡检不会主动标记账号状态。
func NewService(repo Repository, plugins PluginCatalog, concurrency ConcurrencyReader, stateWriter StateWriter) *Service {
	return &Service{
		repo:                repo,
		plugins:             plugins,
		concurrency:         concurrency,
		stateWriter:         stateWriter,
		now:                 time.Now,
		usageCache:          make(map[int]*usageCacheEntry),
		usageRefreshRunning: make(map[string]struct{}),
	}
}

// SetUsageCacheRedis enables the cross-process account usage cache.
func (s *Service) SetUsageCacheRedis(rdb *redis.Client) {
	s.usageRedis = rdb
}

// SetMonitorRecorder injects the best-effort monitor event recorder.
func (s *Service) SetMonitorRecorder(recorder monitoring.Recorder) {
	if s == nil {
		return
	}
	s.monitor = recorder
}

// StartQuotaRefreshLoop periodically refreshes OAuth account plan metadata written into credentials.
func (s *Service) StartQuotaRefreshLoop(ctx context.Context) {
	safego.Go("account_quota_refresh_loop", func() { s.runQuotaRefreshLoop(ctx) })
}

func (s *Service) runQuotaRefreshLoop(ctx context.Context) {
	s.refreshAllOAuthQuotas(ctx)

	ticker := time.NewTicker(autoQuotaRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAllOAuthQuotas(ctx)
		}
	}
}

func (s *Service) refreshAllOAuthQuotas(ctx context.Context) {
	logger := sdk.LoggerFromContext(ctx)
	accounts, err := s.repo.ListAll(ctx, ListFilter{})
	if err != nil {
		logger.Warn("account_quota_auto_refresh_list_failed", sdk.LogFieldError, err)
		return
	}

	success, failed, skipped := 0, 0, 0
	for _, item := range accounts {
		if ctx.Err() != nil {
			return
		}
		if !shouldAutoRefreshQuota(item) {
			skipped++
			continue
		}
		if _, err := s.refreshQuota(ctx, item, false); err != nil {
			failed++
			logger.Warn("account_quota_auto_refresh_failed",
				sdk.LogFieldAccountID, item.ID,
				sdk.LogFieldPlatform, item.Platform,
				sdk.LogFieldError, err)
			continue
		}
		success++
	}

	logger.Info("account_quota_auto_refresh_complete", "success", success, "failed", failed, "skipped", skipped)
}

func shouldAutoRefreshQuota(item Account) bool {
	if len(item.Credentials) == 0 {
		return false
	}
	accountType := strings.ToLower(strings.TrimSpace(item.Type))
	if accountType == "apikey" || accountType == "api_key" {
		return false
	}
	return strings.TrimSpace(item.Credentials["access_token"]) != "" ||
		strings.TrimSpace(item.Credentials["refresh_token"]) != ""
}

// List 查询账号列表。
func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	page, pageSize := NormalizePage(filter.Page, filter.PageSize)
	filter.Page = page
	filter.PageSize = pageSize
	filter = s.normalizeListFilter(filter)

	workingCounts := map[int]int(nil)
	if isWorkingStateFilter(filter.State) {
		nextFilter, counts, empty := s.applyWorkingStateFilter(ctx, filter)
		if empty {
			return ListResult{List: []Account{}, Total: 0, Page: page, PageSize: pageSize}, nil
		}
		filter = nextFilter
		workingCounts = counts
	}

	accounts, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return ListResult{}, err
	}
	s.hydrateAccountListRuntimeData(ctx, accounts, workingCounts)

	s.cacheAccountProfiles(ctx, accounts)

	return ListResult{
		List:     accounts,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *Service) hydrateAccountListRuntimeData(ctx context.Context, accounts []Account, workingCounts map[int]int) {
	ids := make([]int, 0, len(accounts))
	openaiIDs := make([]int, 0, len(accounts))
	for _, item := range accounts {
		ids = append(ids, item.ID)
		// 生图统计仅 OpenAI 平台账号需要：其它平台没有 image endpoint，跑 SQL 也是 0 行白浪费。
		if item.Platform == "openai" {
			openaiIDs = append(openaiIDs, item.ID)
		}
	}
	counts := workingCounts
	if counts == nil {
		counts = s.currentConcurrencyCounts(ctx, ids)
	}
	for index := range accounts {
		accounts[index].CurrentConcurrency = counts[accounts[index].ID]
	}

	// 生图请求计数：今日 + 累计。BatchImageStats 失败不阻断主响应（运维路径优先稳定）。
	if len(openaiIDs) > 0 {
		day := accountcache.Day(s.now())
		imageStats, missingIDs := s.loadImageStatsCache(ctx, day, openaiIDs)
		if len(missingIDs) > 0 {
			todayStart := timezone.StartOfDay(s.now().In(time.Local))
			if fallback, err := s.repo.BatchImageStats(ctx, missingIDs, todayStart); err == nil {
				for _, id := range missingIDs {
					stats := fallback[id]
					imageStats[id] = stats
					s.writeImageStatsCache(ctx, day, id, stats)
				}
			}
		}
		for index := range accounts {
			if accounts[index].Platform != "openai" {
				continue
			}
			if entry, ok := imageStats[accounts[index].ID]; ok {
				stats := entry
				accounts[index].ImageStats = &stats
			} else {
				// 没记录：显式给个零值结构，让前端拿到 today=0/total=0 而不是 nil（区分"没数据"和"非 openai"）
				accounts[index].ImageStats = &AccountImageStats{}
			}
		}
	}
}

// GetCapacity 查询当前账号并发容量。只读取调度器 Redis 运行态，不访问账号 DB。
func (s *Service) GetCapacity(ctx context.Context, accountIDs []int) map[int]int {
	accountIDs = normalizeAccountIDs(accountIDs)
	result := make(map[int]int, len(accountIDs))
	if len(accountIDs) == 0 {
		return result
	}
	if s.concurrency == nil {
		for _, id := range accountIDs {
			result[id] = 0
		}
		return result
	}
	counts := s.concurrency.GetCurrentCounts(ctx, accountIDs)
	for _, id := range accountIDs {
		result[id] = counts[id]
	}
	return result
}

func isWorkingStateFilter(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "working")
}

func (s *Service) applyWorkingStateFilter(ctx context.Context, filter ListFilter) (ListFilter, map[int]int, bool) {
	counts := s.workingConcurrencyCounts(ctx)
	if len(counts) == 0 {
		return filter, counts, true
	}
	filter.State = ""
	filter.IDs = intersectAccountIDs(filter.IDs, mapKeys(counts))
	if len(filter.IDs) == 0 {
		return filter, counts, true
	}
	return filter, counts, false
}

func (s *Service) currentConcurrencyCounts(ctx context.Context, ids []int) map[int]int {
	if s.concurrency == nil {
		return map[int]int{}
	}
	return s.concurrency.GetCurrentCounts(ctx, ids)
}

func (s *Service) workingConcurrencyCounts(ctx context.Context) map[int]int {
	if s.concurrency == nil {
		return map[int]int{}
	}
	return s.concurrency.GetWorkingCounts(ctx)
}

func mapKeys(values map[int]int) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func intersectAccountIDs(left []int, right []int) []int {
	if len(left) == 0 {
		return normalizeAccountIDs(right)
	}
	if len(right) == 0 {
		return nil
	}
	rightSet := make(map[int]struct{}, len(right))
	for _, id := range right {
		if id > 0 {
			rightSet[id] = struct{}{}
		}
	}
	result := make([]int, 0, len(left))
	seen := make(map[int]struct{}, len(left))
	for _, id := range left {
		if id <= 0 {
			continue
		}
		if _, ok := rightSet[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// Create 创建账号。
func (s *Service) Create(ctx context.Context, input CreateInput) (Account, error) {
	logger := sdk.LoggerFromContext(ctx)
	rateMultiplier, err := normalizeCreateRateMultiplier(input.RateMultiplier)
	if err != nil {
		return Account{}, err
	}
	input.RateMultiplier = &rateMultiplier
	input.ModelPolicy = modelpolicy.Normalize(input.ModelPolicy)
	if err := validateModelPolicy(input.ModelPolicy); err != nil {
		return Account{}, err
	}

	account, err := s.repo.Create(ctx, input)
	if err != nil {
		logger.Error("account_credential_persist_failed",
			sdk.LogFieldPlatform, input.Platform,
			"type", input.Type,
			"name", input.Name,
			sdk.LogFieldError, err)
		return account, err
	}
	logger.Info("account_created",
		sdk.LogFieldAccountID, account.ID,
		sdk.LogFieldPlatform, account.Platform,
		"type", account.Type,
		"name", account.Name)
	s.cacheAccountProfiles(ctx, []Account{account})
	s.InvalidateUsageCache("") // 新账号创建后清除用量缓存
	return account, err
}

// ExportAll 查询符合筛选条件的全部账号（用于导出，不分页、不带并发计数）。
func (s *Service) ExportAll(ctx context.Context, filter ListFilter) ([]Account, error) {
	filter = s.normalizeListFilter(filter)
	if isWorkingStateFilter(filter.State) {
		nextFilter, _, empty := s.applyWorkingStateFilter(ctx, filter)
		if empty {
			return []Account{}, nil
		}
		filter = nextFilter
	}
	return s.repo.ListAll(ctx, filter)
}

// Import 批量导入账号，逐条创建并收集失败信息（不使用事务，允许部分成功）。
func (s *Service) Import(ctx context.Context, items []CreateInput) ImportSummary {
	summary := ImportSummary{}
	for index, input := range items {
		rateMultiplier, err := normalizeCreateRateMultiplier(input.RateMultiplier)
		if err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, ImportItemError{
				Index:   index,
				Name:    input.Name,
				Message: err.Error(),
			})
			continue
		}
		input.RateMultiplier = &rateMultiplier
		input.ModelPolicy = modelpolicy.Normalize(input.ModelPolicy)
		if err := validateModelPolicy(input.ModelPolicy); err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, ImportItemError{
				Index:   index,
				Name:    input.Name,
				Message: err.Error(),
			})
			continue
		}
		input.GroupIDs = nil
		input.ProxyID = nil
		created, err := s.repo.Create(ctx, input)
		if err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, ImportItemError{
				Index:   index,
				Name:    input.Name,
				Message: err.Error(),
			})
			continue
		}
		summary.Imported++
		summary.SuccessIDs = append(summary.SuccessIDs, created.ID)
	}
	if summary.Imported > 0 {
		s.InvalidateUsageCache("")
	}
	return summary
}

// Update 更新账号。
func (s *Service) Update(ctx context.Context, id int, input UpdateInput) (Account, error) {
	logger := sdk.LoggerFromContext(ctx)
	if input.RateMultiplier != nil {
		if err := validateRateMultiplier(*input.RateMultiplier); err != nil {
			return Account{}, err
		}
	}
	if input.ModelPolicy != nil {
		policy := modelpolicy.Normalize(*input.ModelPolicy)
		if err := validateModelPolicy(policy); err != nil {
			return Account{}, err
		}
		input.ModelPolicy = &policy
	}
	repoInput := input
	manualState, routeManualState, err := s.routedManualState(input.State)
	if err != nil {
		return Account{}, err
	}
	if routeManualState {
		repoInput.State = nil
	}

	var updated Account
	if hasUpdateInputChanges(repoInput) {
		updated, err = s.repo.Update(ctx, id, repoInput)
	} else {
		updated, err = s.repo.FindByID(ctx, id, LoadOptions{WithGroups: true, WithProxy: true})
	}
	if err != nil {
		logger.Error("account_credential_persist_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldError, err)
		return updated, err
	}

	if routeManualState {
		if err := s.applyManualState(ctx, id, manualState); err != nil {
			logger.Error("account_manual_state_failed",
				sdk.LogFieldAccountID, id,
				"state", manualState,
				sdk.LogFieldError, err)
			return updated, err
		}
		if reloaded, reloadErr := s.repo.FindByID(ctx, id, LoadOptions{WithGroups: true, WithProxy: true}); reloadErr == nil {
			updated = reloaded
		} else {
			logger.Warn("account_reload_after_manual_state_failed",
				sdk.LogFieldAccountID, id,
				sdk.LogFieldError, reloadErr)
			updated.State = manualState
		}
	}

	switch {
	case input.State != nil:
		state := strings.TrimSpace(*input.State)
		if routeManualState {
			state = manualState
		}
		logger.Info("account_status_changed",
			sdk.LogFieldAccountID, id,
			"state", state)
	case input.MaxConcurrency != nil || input.RateMultiplier != nil:
		logger.Info("account_quota_updated",
			sdk.LogFieldAccountID, id)
	}
	if input.Type != nil || input.Credentials != nil || input.State != nil {
		s.InvalidateUsageCache(updated.Platform)
	}
	s.cacheAccountProfiles(ctx, []Account{updated})
	return updated, err
}

// Delete 删除账号。
func (s *Service) Delete(ctx context.Context, id int) error {
	logger := sdk.LoggerFromContext(ctx)
	err := s.repo.Delete(ctx, id)
	if err != nil {
		logger.Error("account_credential_persist_failed",
			sdk.LogFieldAccountID, id,
			"op", "delete",
			sdk.LogFieldError, err)
		return err
	}
	logger.Info("account_deleted", sdk.LogFieldAccountID, id)
	s.deleteAccountCacheKeys([]int{id})
	s.InvalidateUsageCache("")
	return err
}

// BulkUpdate 批量更新账号。逐条执行并收集每个账号的成功/失败信息，允许部分成功。
// group_ids 为整体替换：若提供则覆盖账号原有分组，未提供则不触碰。
func (s *Service) BulkUpdate(ctx context.Context, input BulkUpdateInput) BulkResult {
	result := BulkResult{Results: make([]BulkResultItem, 0, len(input.IDs))}
	if input.RateMultiplier != nil {
		if err := validateRateMultiplier(*input.RateMultiplier); err != nil {
			for _, id := range input.IDs {
				result.appendFailure(id, err)
			}
			return result
		}
	}
	if input.ModelPolicy != nil {
		policy := modelpolicy.Normalize(*input.ModelPolicy)
		if err := validateModelPolicy(policy); err != nil {
			for _, id := range input.IDs {
				result.appendFailure(id, err)
			}
			return result
		}
		input.ModelPolicy = &policy
	}
	mutated := false
	for _, id := range input.IDs {
		patch := UpdateInput{
			State:          input.State,
			Priority:       input.Priority,
			MaxConcurrency: input.MaxConcurrency,
			RateMultiplier: input.RateMultiplier,
			ModelPolicy:    input.ModelPolicy,
		}
		if input.HasProxyID {
			patch.ProxyID = input.ProxyID
			patch.HasProxyID = true
		}
		if input.HasGroupIDs {
			patch.GroupIDs = input.GroupIDs
			patch.HasGroupIDs = true
		}
		if input.HasExtra {
			existing, err := s.repo.FindByID(ctx, id, LoadOptions{})
			if err != nil {
				result.appendFailure(id, err)
				continue
			}
			patch.Extra = mergeAnyMap(existing.Extra, input.Extra)
			patch.HasExtra = true
		}

		manualState, routeManualState, err := s.routedManualState(patch.State)
		if err != nil {
			result.appendFailure(id, err)
			continue
		}
		if routeManualState {
			patch.State = nil
		}
		patchHasChanges := hasUpdateInputChanges(patch)
		if !routeManualState && !patchHasChanges {
			if _, err := s.repo.FindByID(ctx, id, LoadOptions{}); err != nil {
				result.appendFailure(id, err)
				continue
			}
			result.appendSuccess(id)
			continue
		}

		if patchHasChanges {
			if _, err := s.repo.Update(ctx, id, patch); err != nil {
				result.appendFailure(id, err)
				continue
			}
			mutated = true
		}
		if routeManualState {
			if err := s.applyManualState(ctx, id, manualState); err != nil {
				result.appendFailure(id, err)
				continue
			}
			mutated = true
		}
		result.appendSuccess(id)
	}
	if mutated && len(result.SuccessIDs) > 0 {
		accounts, err := s.repo.ListAll(ctx, ListFilter{IDs: result.SuccessIDs})
		if err == nil {
			s.cacheAccountProfiles(ctx, accounts)
		}
	}
	if result.Success > 0 && input.State != nil {
		s.InvalidateUsageCache("")
	}
	return result
}

// BulkDelete 批量删除账号。
func (s *Service) BulkDelete(ctx context.Context, ids []int) BulkResult {
	result := BulkResult{Results: make([]BulkResultItem, 0, len(ids))}
	for _, id := range ids {
		if err := s.repo.Delete(ctx, id); err != nil {
			result.appendFailure(id, err)
			continue
		}
		result.appendSuccess(id)
	}
	if result.Success > 0 {
		s.deleteAccountCacheKeys(result.SuccessIDs)
		s.InvalidateUsageCache("")
	}
	return result
}

func (r *BulkResult) appendSuccess(id int) {
	r.Success++
	r.SuccessIDs = append(r.SuccessIDs, id)
	r.Results = append(r.Results, BulkResultItem{ID: id, Success: true})
}

func (r *BulkResult) appendFailure(id int, err error) {
	r.Failed++
	r.FailedIDs = append(r.FailedIDs, id)
	r.Results = append(r.Results, BulkResultItem{ID: id, Success: false, Error: err.Error()})
}

func hasUpdateInputChanges(input UpdateInput) bool {
	return input.Name != nil ||
		input.Type != nil ||
		input.Credentials != nil ||
		input.State != nil ||
		input.Priority != nil ||
		input.MaxConcurrency != nil ||
		input.RateMultiplier != nil ||
		input.ModelPolicy != nil ||
		input.UpstreamIsPool != nil ||
		input.HasGroupIDs ||
		input.HasProxyID ||
		input.HasExtra
}

func normalizeCreateRateMultiplier(value *float64) (float64, error) {
	rateMultiplier := 1.0
	if value != nil {
		rateMultiplier = *value
	}
	if err := validateRateMultiplier(rateMultiplier); err != nil {
		return 0, err
	}
	return rateMultiplier, nil
}

func validateRateMultiplier(value float64) error {
	if err := ratevalue.ValidateMultiplier(value); err != nil {
		return errors.Join(ErrInvalidRateMultiplier, err)
	}
	return nil
}

func validateModelPolicy(policy modelpolicy.Policy) error {
	if err := modelpolicy.Validate(policy); err != nil {
		return errors.Join(ErrInvalidModelPolicy, err)
	}
	return nil
}

func mergeAnyMap(base, patch map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(patch))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range patch {
		merged[key] = value
	}
	return merged
}

func (s *Service) routedManualState(state *string) (string, bool, error) {
	value, ok, err := normalizeManualState(state)
	if err != nil || !ok || s.stateWriter == nil {
		return value, false, err
	}
	return value, true, nil
}

func normalizeManualState(state *string) (string, bool, error) {
	if state == nil {
		return "", false, nil
	}
	value := strings.ToLower(strings.TrimSpace(*state))
	if value == "active" || value == "disabled" {
		return value, true, nil
	}
	return value, false, ErrInvalidState
}

func (s *Service) applyManualState(ctx context.Context, id int, state string) error {
	switch state {
	case "active":
		return s.stateWriter.ManualRecover(ctx, id)
	case "disabled":
		return s.stateWriter.ManualDisable(ctx, id, "手动关闭")
	default:
		return nil
	}
}

// ToggleScheduling 快速切换账号调度状态。active ↔ disabled。
// 其它中间态（rate_limited / degraded）一律视为"非 disabled"，切换后目标 = disabled。
//
// 通过 StateWriter 走状态机路径，确保 RouteGraph 立即刷新。
func (s *Service) ToggleScheduling(ctx context.Context, id int) (ToggleResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	item, err := s.repo.FindByID(ctx, id, LoadOptions{})
	if err != nil {
		logger.Error("account_lookup_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldError, err)
		return ToggleResult{}, err
	}

	var newState string
	if item.State == "disabled" {
		newState = "active"
		if s.stateWriter != nil {
			if err := s.stateWriter.ManualRecover(ctx, id); err != nil {
				logger.Error("account_manual_recover_failed",
					sdk.LogFieldAccountID, id, sdk.LogFieldError, err)
				return ToggleResult{}, err
			}
		}
	} else {
		newState = "disabled"
		if s.stateWriter != nil {
			if err := s.stateWriter.ManualDisable(ctx, id, "手动关闭"); err != nil {
				logger.Error("account_manual_disable_failed",
					sdk.LogFieldAccountID, id, sdk.LogFieldError, err)
				return ToggleResult{}, err
			}
		}
	}

	logger.Info("account_status_changed",
		sdk.LogFieldAccountID, id,
		"state", newState)
	return ToggleResult{ID: id, State: newState}, nil
}

// PrepareConnectivityTest 准备账号连通性测试。
func (s *Service) PrepareConnectivityTest(ctx context.Context, id int, modelID string) (*ConnectivityTest, error) {
	logger := sdk.LoggerFromContext(ctx)
	item, err := s.repo.FindByID(ctx, id, LoadOptions{WithProxy: true})
	if err != nil {
		logger.Error("account_lookup_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldError, err)
		return nil, err
	}

	inst := s.plugins.GetPluginByPlatform(item.Platform)
	if inst == nil || inst.Gateway == nil {
		logger.Warn("account_credential_validation_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldPlatform, item.Platform,
			sdk.LogFieldReason, "plugin_not_found")
		return nil, ErrPluginNotFound
	}

	if modelID == "" {
		models := s.plugins.GetModels(item.Platform)
		if len(models) > 0 {
			modelID = models[0].ID
		}
	}
	if modelID == "" {
		return nil, ErrModelRequired
	}

	testBody, _ := json.Marshal(map[string]any{
		"model":    modelID,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
		"stream":   true,
	})

	// X-Airgate-Internal 让下游网关（如 gateway-claude 的 claude_code_only 开关）
	// 能识别这是管理后台自家的探测流量，跳过面向外部客户端的身份闸。
	forwardReq := &sdk.ForwardRequest{
		Account: &sdk.Account{
			ID:          int64(item.ID),
			Name:        item.Name,
			Platform:    item.Platform,
			Type:        item.Type,
			Credentials: cloneStringMap(item.Credentials),
			ProxyURL:    buildProxyURL(item.Proxy),
		},
		Body: testBody,
		Headers: http.Header{
			"Content-Type":       {"application/json"},
			"X-Airgate-Internal": {"test"},
		},
		Model:  modelID,
		Stream: true,
	}

	return &ConnectivityTest{
		AccountName: item.Name,
		AccountType: item.Type,
		ModelID:     modelID,
		run: func(runCtx context.Context, writer http.ResponseWriter) error {
			req := *forwardReq
			req.Writer = writer
			outcome, forwardErr := inst.Gateway.Forward(runCtx, &req)
			if forwardErr != nil {
				s.recordConnectivityTestFailure(runCtx, item, modelID, "plugin_forward_error", forwardErr)
				return forwardErr
			}
			// 测试路径严格判定：只有 OutcomeSuccess 算通过；任何其它 Kind 都报告失败。
			// 这是管理员工具，失败原因要保留真实上游诊断，方便直接排查账号态 / 上游态问题。
			if outcome.Kind == sdk.OutcomeSuccess {
				s.resolveAccountMonitorEvents(runCtx, item.ID)
				return nil
			}
			msg := connectivityTestErrorMessage(outcome)
			if msg == "" && outcome.Upstream.StatusCode > 0 {
				msg = fmt.Sprintf("upstream returned HTTP %d", outcome.Upstream.StatusCode)
			}
			if msg == "" {
				msg = fmt.Sprintf("plugin returned %s", outcome.Kind)
			}
			err := errors.New(msg)
			s.recordConnectivityTestFailure(runCtx, item, modelID, outcome.Kind.String(), err)
			return err
		},
	}, nil
}

func connectivityTestErrorMessage(outcome sdk.ForwardOutcome) string {
	if msg := extractBodyError(outcome.Upstream.Body); msg != "" {
		return formatConnectivityHTTPMessage(outcome.Upstream.StatusCode, msg)
	}

	reason := strings.TrimSpace(outcome.Reason)
	if isConnectivityInternalDiagnostic(reason) {
		reason = ""
	}

	switch outcome.Kind {
	case sdk.OutcomeClientError:
		if reason == "" {
			reason = "请求参数或测试模型不被上游接受"
		}
		return formatConnectivityHTTPMessage(outcome.Upstream.StatusCode, reason)
	case sdk.OutcomeAccountRateLimited:
		if outcome.RetryAfter > 0 {
			return fmt.Sprintf("上游账号当前被限流，请在 %s 后重试", outcome.RetryAfter)
		}
		return "上游账号当前被限流，请稍后重试"
	case sdk.OutcomeAccountDead:
		if reason != "" {
			return "上游账号不可用: " + reason
		}
		return "上游账号不可用，请检查凭证或账号状态"
	case sdk.OutcomeAccountUnavailable:
		if reason != "" {
			return "上游账号403暂不可用: " + reason
		}
		return "上游账号403暂不可用，请稍后重试"
	case sdk.OutcomeStreamAborted:
		return "上游响应流中断，请稍后重试或查看上游日志"
	case sdk.OutcomeUpstreamTransient:
		if reason != "" {
			return "上游服务暂不可用: " + reason
		}
		return "上游未返回有效响应，请检查测试模型是否被该上游账号支持或查看上游日志"
	default:
		return reason
	}
}

func formatConnectivityHTTPMessage(statusCode int, msg string) string {
	if statusCode >= 400 && !strings.HasPrefix(strings.ToUpper(msg), "HTTP ") {
		return fmt.Sprintf("HTTP %d: %s", statusCode, msg)
	}
	return msg
}

func isConnectivityInternalDiagnostic(reason string) bool {
	return strings.Contains(reason, "上游流式响应为空") ||
		strings.Contains(reason, "未收到上游流式完成事件")
}

// extractBodyError 从上游错误响应 body 中提取人类可读的错误消息。
//
// Claude 等插件的 extractErrorMessage 只认 Anthropic 标准嵌套格式
// {"error":{"type":"...","message":"..."}}，对于以下变体会失败：
//   - 顶层 code+message：{"code":"INVALID_API_KEY","message":"Invalid API key"}
//     （某些池子转发器 / 反代会用这种格式）
//   - 顶层只有 message：{"message":"..."}
//   - error 是字符串：{"error":"some plain text"}
//   - error.message 但没有 error.type
//
// 这里把这些格式都覆盖一遍。返回空字符串表示无法提取。
func extractBodyError(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}

	asString := func(v any) string {
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}

	// 1. {"error": {"type": "...", "message": "..."}} (Anthropic 标准)
	if errObj, ok := raw["error"].(map[string]any); ok {
		t := asString(errObj["type"])
		m := asString(errObj["message"])
		switch {
		case t != "" && m != "":
			return t + ": " + m
		case m != "":
			return m
		case t != "":
			return t
		}
	}

	// 2. {"error": "plain text"}
	if s := asString(raw["error"]); s != "" {
		return s
	}

	// 3. 顶层 {"code": "...", "message": "..."}（池子转发器常见格式）
	code := asString(raw["code"])
	msg := asString(raw["message"])
	switch {
	case code != "" && msg != "":
		return code + ": " + msg
	case msg != "":
		return msg
	case code != "":
		return code
	}

	return ""
}

// GetModels 获取账号平台的模型列表。
func (s *Service) GetModels(ctx context.Context, id int) ([]Model, error) {
	item, err := s.repo.FindByID(ctx, id, LoadOptions{WithProxy: true})
	if err != nil {
		return nil, err
	}

	if item.Platform == "openai" && item.Type == "apikey" {
		if models, err := getAPIKeyUpstreamModels(ctx, item); err == nil && len(models) > 0 {
			return models, nil
		}
	}

	rawModels := s.plugins.GetModels(item.Platform)
	models := make([]Model, 0, len(rawModels))
	for _, raw := range rawModels {
		models = append(models, Model{ID: raw.ID, Name: raw.Name})
	}
	return models, nil
}

func getAPIKeyUpstreamModels(ctx context.Context, item Account) ([]Model, error) {
	apiKey := strings.TrimSpace(item.Credentials["api_key"])
	if apiKey == "" {
		return nil, errors.New("missing api_key")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildAPIKeyModelsURL(item.Credentials["base_url"]), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client, err := accountHTTPClient(item.Proxy)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("/v1/models returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseOpenAIModelsResponse(body), nil
}

func buildAPIKeyModelsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/models"
	}
	return baseURL + "/v1/models"
}

func accountHTTPClient(proxyInfo *Proxy) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL := buildProxyURL(proxyInfo); proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

func parseOpenAIModelsResponse(body []byte) []Model {
	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	models := make([]Model, 0, len(payload.Data))
	seen := make(map[string]bool, len(payload.Data))
	for _, raw := range payload.Data {
		id := strings.TrimSpace(raw.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(raw.Name)
		if name == "" {
			name = id
		}
		models = append(models, Model{ID: id, Name: name})
	}
	return models
}

// InvalidateUsageCache 清除指定平台的用量缓存（创建/删除账号后调用）。
// platform 为空时清理所有账号用量缓存；platform 非空时清理该平台账号缓存。
func (s *Service) InvalidateUsageCache(platform string) {
	platform = strings.TrimSpace(platform)
	s.usageMu.Lock()
	if platform == "" {
		s.usageCache = make(map[int]*usageCacheEntry)
	} else {
		for accountID, entry := range s.usageCache {
			if entry != nil && entry.platform == platform {
				delete(s.usageCache, accountID)
			}
		}
	}
	s.usageMu.Unlock()

	if s.usageRedis == nil {
		return
	}
	if platform == "" {
		s.deleteAllUsageCacheKeys()
		return
	}
	s.deleteUsageCacheKeysForPlatform(platform)
}

type accountUsageRequest struct {
	ID          int               `json:"id"`
	Credentials map[string]string `json:"credentials"`
}

// GetAccountUsage 查询当前页账号的用量视图。
//
// 账号页必须传入当前页 ids；这里不再按平台全量扫描账号。读取路径只批量取这些
// ids 的 Redis usage/today 统计，缓存缺失时后台批量刷新缺失账号。
func (s *Service) GetAccountUsage(ctx context.Context, platform string, accountIDs []int, refresh bool) (map[string]any, bool, error) {
	accountIDs = normalizeAccountIDs(accountIDs)
	if len(accountIDs) == 0 {
		return map[string]any{}, false, nil
	}

	accounts, missingProfileIDs := s.loadAccountProfilesForUsage(ctx, platform, accountIDs)
	if len(missingProfileIDs) > 0 {
		fallback, err := s.repo.ListAll(ctx, ListFilter{Platform: platform, IDs: missingProfileIDs})
		if err != nil {
			return nil, false, err
		}
		s.cacheAccountProfiles(ctx, fallback)
		accounts = append(accounts, fallback...)
	}

	result := make(map[string]any, len(accounts))
	infos, missingAccounts := s.getUsageInfosForAccounts(ctx, platform, accounts)
	for _, item := range accounts {
		key := strconv.Itoa(item.ID)
		if info, ok := infos[item.ID]; ok {
			result[key] = accountUsageInfoToMap(info)
			continue
		}
		result[key] = map[string]any{}
	}

	s.enrichTodayStats(ctx, result)
	missingRefreshKey := usageCacheAccountsRefreshKey(platform, missingAccounts)
	pageRefreshAccounts := filterRefreshableUsageAccounts(accounts)
	pageRefreshKey := usageCacheAccountsRefreshKey(platform, pageRefreshAccounts)
	missingRefreshRunning := missingRefreshKey != "" && s.isUsageRefreshRunning(missingRefreshKey)
	pageRefreshRunning := pageRefreshKey != "" && s.isUsageRefreshRunning(pageRefreshKey)
	refreshAccounts := missingAccounts
	if refresh {
		refreshAccounts = pageRefreshAccounts
	}
	refreshing := len(missingAccounts) > 0 || missingRefreshRunning || pageRefreshRunning
	if len(refreshAccounts) > 0 && (refresh || !pageRefreshRunning) {
		s.ensureUsageCacheRefreshForAccounts(platform, refreshAccounts)
		refreshing = true
	}
	return result, refreshing, nil
}

// GetSingleAccountUsage 查询单个账号当前用量视图。
//
// 自动刷新路径使用批量 ids 接口；这个接口保留给手动单账号刷新和未来按需查询。
func (s *Service) GetSingleAccountUsage(ctx context.Context, id int) (map[string]any, error) {
	item, err := s.repo.FindByID(ctx, id, LoadOptions{})
	if err != nil {
		return nil, err
	}
	s.cacheAccountProfiles(ctx, []Account{item})

	key := strconv.Itoa(item.ID)
	accountUsage := map[string]any{}
	var hasCachedInfo bool
	if info, ok := s.getUsageInfoForAccount(ctx, item.ID); ok {
		hasCachedInfo = true
		accountUsage = accountUsageInfoToMap(info)
	}

	if item.Type != "apikey" && !hasCachedInfo {
		info, usageErrors, ok := s.fetchSingleAccountUsageDedup(ctx, item)

		s.handleSingleAccountUsageErrors(ctx, item, usageErrors)
		if ok {
			normalized := normalizeAccountUsageInfo(info)
			accountUsage = accountUsageInfoToMap(normalized)
			s.updateAccountUsageCache(ctx, item.Platform, item.ID, normalized)
			if item.State != "disabled" {
				s.persistRateLimitFromWindows(ctx, map[string]any{key: accountUsage})
			}
		}
	}

	result := map[string]any{key: accountUsage}
	s.enrichTodayStats(ctx, result)
	if accountMap, ok := result[key].(map[string]any); ok {
		return accountMap, nil
	}
	return map[string]any{}, nil
}

func (s *Service) fetchUpstreamUsageForAccounts(ctx context.Context, accounts []Account) (map[string]AccountUsageInfo, error) {
	merged := make(map[string]AccountUsageInfo)
	for _, item := range accounts {
		if isRefreshableUsageAccount(item) && item.ID > 0 {
			merged[strconv.Itoa(item.ID)] = AccountUsageInfo{}
		}
	}
	if s.plugins == nil || len(accounts) == 0 {
		return merged, nil
	}

	accountsByPlatform := make(map[string][]Account)
	for _, item := range accounts {
		if item.Platform == "" || !isRefreshableUsageAccount(item) {
			continue
		}
		accountsByPlatform[item.Platform] = append(accountsByPlatform[item.Platform], item)
	}

	for platform, platformAccounts := range accountsByPlatform {
		inst := s.plugins.GetPluginByPlatform(platform)
		if inst == nil || inst.Gateway == nil {
			continue
		}

		// 建立 accountID → 是否池子 的查询表，用于后面插件返回 errors
		// 时判断是否应该跳过 MarkError（池子账号永远不自动标错）
		poolByID := make(map[int]bool, len(platformAccounts))
		for _, item := range platformAccounts {
			poolByID[item.ID] = item.UpstreamIsPool
		}

		disabledIDs := make(map[int]bool, len(platformAccounts))
		allowedIDs := make(map[string]struct{}, len(platformAccounts))
		reqList := make([]accountUsageRequest, 0, len(platformAccounts))
		for _, item := range platformAccounts {
			key := strconv.Itoa(item.ID)
			if item.State == "disabled" {
				disabledIDs[item.ID] = true
			}
			allowedIDs[key] = struct{}{}
			reqList = append(reqList, accountUsageRequest{
				ID:          item.ID,
				Credentials: cloneStringMap(item.Credentials),
			})
		}
		if len(reqList) == 0 {
			continue
		}

		for start := 0; start < len(reqList); start += usageAccountsProbeBatchSize {
			end := start + usageAccountsProbeBatchSize
			if end > len(reqList) {
				end = len(reqList)
			}
			batch := reqList[start:end]
			body, _ := json.Marshal(batch)
			startedAt := time.Now()
			status, _, respBody, err := inst.Gateway.HandleHTTPRequest(ctx, "POST", "usage/accounts", "", nil, body)
			if err != nil || status != http.StatusOK {
				slog.Debug("account_usage_probe_batch_failed",
					sdk.LogFieldPlatform, platform,
					"account_count", len(batch),
					sdk.LogFieldStatus, status,
					sdk.LogFieldDurationMs, time.Since(startedAt).Milliseconds(),
					sdk.LogFieldError, err)
				if ctx.Err() != nil {
					break
				}
				continue
			}

			var result accountUsagePluginResponse
			if err := json.Unmarshal(respBody, &result); err != nil {
				slog.Debug("account_usage_probe_batch_parse_failed",
					sdk.LogFieldPlatform, platform,
					"account_count", len(batch),
					sdk.LogFieldDurationMs, time.Since(startedAt).Milliseconds(),
					sdk.LogFieldError, err)
				continue
			}

			normalizedAccounts := make(map[string]AccountUsageInfo, len(result.Accounts))
			for key, value := range result.Accounts {
				if _, ok := allowedIDs[key]; !ok {
					continue
				}
				normalized := normalizeAccountUsageInfo(value)
				merged[key] = normalized
				normalizedAccounts[key] = normalized
			}
			// 根据每个账号的 windows 反推限流恢复时间并持久化到 DB。
			// 已 disabled 的账号不参与限流状态推导，避免覆盖手动关闭调度的状态。
			activeAccounts := make(map[string]any, len(normalizedAccounts))
			for key, value := range normalizedAccounts {
				id, _ := strconv.Atoi(key)
				if !disabledIDs[id] {
					activeAccounts[key] = accountUsageInfoToMap(value)
				}
			}
			s.persistRateLimitFromWindows(ctx, activeAccounts)

			for _, item := range result.Errors {
				if _, ok := poolByID[item.ID]; !ok {
					continue
				}
				// 池账号 / 已禁用账号不自动改状态（避免覆盖人工关闭的 reason）。
				if poolByID[item.ID] || disabledIDs[item.ID] || s.stateWriter == nil {
					continue
				}
				s.markAccountUsageError(ctx, item.ID, item.Message)
			}
		}
		if ctx.Err() != nil {
			break
		}
	}

	return merged, nil
}

// fetchSingleAccountUsageDedup 在 (platform, accountID) 维度上对单账号 probe 做
// singleflight 合并：重复点同一账号（或后台 batch refresh 正在 flying 时穿透进来的
// probe）只会真正打一次插件。
// 调用方对 ctx 的语义：第一次入队的 goroutine 决定上游请求生命周期，30s 超时
// 是给 plugin 端的硬上限。后到的并发请求复用其结果，自己的 ctx.Done 仍可早退。
func (s *Service) fetchSingleAccountUsageDedup(ctx context.Context, item Account) (AccountUsageInfo, []accountUsageError, bool) {
	type result struct {
		info        AccountUsageInfo
		usageErrors []accountUsageError
		ok          bool
	}
	key := "single:" + usageCachePlatformKey(item.Platform) + ":" + strconv.Itoa(item.ID)
	v, _, _ := s.usageFlight.Do(key, func() (any, error) {
		queryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, usageErrors, ok := s.fetchSingleAccountUsage(queryCtx, item)
		return result{info: info, usageErrors: usageErrors, ok: ok}, nil
	})
	res, _ := v.(result)
	// 调用方早退（ctx.Done）时 res 仍是 zero/false。
	if err := ctx.Err(); err != nil {
		return AccountUsageInfo{}, nil, false
	}
	return res.info, res.usageErrors, res.ok
}

func (s *Service) fetchSingleAccountUsage(ctx context.Context, item Account) (AccountUsageInfo, []accountUsageError, bool) {
	if s.plugins == nil {
		return AccountUsageInfo{}, nil, false
	}
	inst := s.plugins.GetPluginByPlatform(item.Platform)
	if inst == nil || inst.Gateway == nil {
		return AccountUsageInfo{}, nil, false
	}

	req := accountUsageRequest{
		ID:          item.ID,
		Credentials: cloneStringMap(item.Credentials),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return AccountUsageInfo{}, nil, false
	}

	status, _, respBody, err := inst.Gateway.HandleHTTPRequest(ctx, "POST", "usage/probe", "", nil, body)
	if err == nil && status == http.StatusOK {
		info, usageErrors, ok := parseSingleAccountUsagePluginResponse(item.ID, respBody)
		if ok || len(usageErrors) > 0 {
			return info, usageErrors, ok
		}
	}

	body, err = json.Marshal([]accountUsageRequest{req})
	if err != nil {
		return AccountUsageInfo{}, nil, false
	}
	status, _, respBody, err = inst.Gateway.HandleHTTPRequest(ctx, "POST", "usage/accounts", "", nil, body)
	if err != nil || status != http.StatusOK {
		return AccountUsageInfo{}, nil, false
	}
	return parseSingleAccountUsagePluginResponse(item.ID, respBody)
}

func parseSingleAccountUsagePluginResponse(id int, body []byte) (AccountUsageInfo, []accountUsageError, bool) {
	var accountsResp accountUsagePluginResponse
	if err := json.Unmarshal(body, &accountsResp); err == nil {
		accountKey := strconv.Itoa(id)
		if info, ok := accountsResp.Accounts[accountKey]; ok {
			return normalizeAccountUsageInfo(info), accountsResp.Errors, true
		}
		if len(accountsResp.Errors) > 0 {
			return AccountUsageInfo{}, accountsResp.Errors, false
		}
	}

	var directResp AccountUsageInfo
	if err := json.Unmarshal(body, &directResp); err != nil {
		return AccountUsageInfo{}, nil, false
	}
	directResp = normalizeAccountUsageInfo(directResp)
	if directResp.UpdatedAt == "" && len(directResp.Windows) == 0 && directResp.Credits == nil {
		return AccountUsageInfo{}, nil, false
	}
	return directResp, nil, true
}

func (s *Service) handleSingleAccountUsageErrors(ctx context.Context, item Account, usageErrors []accountUsageError) {
	if s.stateWriter == nil || item.UpstreamIsPool || item.State == "disabled" {
		return
	}
	for _, usageErr := range usageErrors {
		if usageErr.ID != item.ID || usageErr.Message == "" {
			continue
		}
		s.markAccountUsageError(ctx, item.ID, usageErr.Message)
		return
	}
}

func (s *Service) markAccountUsageError(ctx context.Context, accountID int, message string) {
	if s.stateWriter == nil {
		return
	}
	if httperrors.IsForbiddenError(message, 0) {
		s.stateWriter.MarkDegraded(ctx, accountID, message)
		return
	}
	s.stateWriter.MarkDisabled(ctx, accountID, message)
}

// updateAccountUsageCache 把单账号最新探测结果写入单账号缓存。
func (s *Service) updateAccountUsageCache(ctx context.Context, platform string, accountID int, info AccountUsageInfo) {
	if accountID <= 0 {
		return
	}
	now := s.now()
	if existing, ok := s.getUsageInfoForAccount(ctx, accountID); ok {
		info = mergeAccountUsageInfo(existing, info, now)
	}
	s.writeUsageInfoCache(ctx, platform, accountID, info, now)
}

type accountUsageCacheWrite struct {
	account Account
	info    AccountUsageInfo
}

func (s *Service) updateAccountUsageCaches(ctx context.Context, accounts []Account, usage map[string]AccountUsageInfo) {
	if len(accounts) == 0 || len(usage) == 0 {
		return
	}

	writes := make([]accountUsageCacheWrite, 0, len(accounts))
	for _, item := range accounts {
		if !isRefreshableUsageAccount(item) {
			continue
		}
		info, ok := usage[strconv.Itoa(item.ID)]
		if !ok || !accountUsageInfoHasData(info) {
			continue
		}
		writes = append(writes, accountUsageCacheWrite{account: item, info: info})
	}
	if len(writes) == 0 {
		return
	}

	now := s.now()
	existing := s.getUsageInfosForCacheWrites(ctx, writes, now)
	for _, write := range writes {
		info := write.info
		if cached, ok := existing[write.account.ID]; ok {
			info = mergeAccountUsageInfo(cached, info, now)
		}
		s.writeUsageInfoCache(ctx, write.account.Platform, write.account.ID, info, now)
	}
}

func (s *Service) getUsageInfosForCacheWrites(ctx context.Context, writes []accountUsageCacheWrite, now time.Time) map[int]AccountUsageInfo {
	result := make(map[int]AccountUsageInfo, len(writes))
	if len(writes) == 0 {
		return result
	}
	if s.usageRedis == nil {
		for _, write := range writes {
			if info, _, ok := s.getUsageInfoMemoryCache(write.account.ID); ok {
				result[write.account.ID] = info
			}
		}
		return result
	}

	keys := make([]string, 0, len(writes))
	for _, write := range writes {
		keys = append(keys, accountcache.UsageKey(write.account.ID))
	}
	values, err := s.usageRedis.MGet(ctx, keys...).Result()
	if err != nil {
		return result
	}
	for index, write := range writes {
		raw, ok := redisValueBytes(values[index])
		if !ok {
			continue
		}
		var payload accountUsageCachePayload
		if err := json.Unmarshal(raw, &payload); err != nil || !payload.valid() {
			continue
		}
		info, ok := payload.cacheInfo(now)
		if ok {
			result[write.account.ID] = info
		}
	}
	return result
}

func (s *Service) writeUsageInfoCache(ctx context.Context, platform string, accountID int, info AccountUsageInfo, now time.Time) {
	info = liveAccountUsageInfo(accountUsageInfoWithAbsoluteResets(info, now), now, now.Add(usageCacheMaxTTL))
	if !accountUsageInfoHasData(info) {
		if s.usageRedis == nil {
			s.deleteUsageInfoMemoryCache(accountID)
		} else {
			_ = s.usageRedis.Del(ctx, accountcache.UsageKey(accountID)).Err()
		}
		return
	}
	expiresAt := accountUsageInfoExpiresAt(info, now)
	ttl := expiresAt.Sub(now)
	if ttl < usageCacheMinimumTTL {
		ttl = usageCacheMinimumTTL
		expiresAt = now.Add(ttl)
	}
	if s.usageRedis == nil {
		s.setUsageInfoMemoryCache(accountID, platform, info, now, expiresAt)
		return
	}
	payload := newAccountUsageCachePayload(info, now)
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := s.usageRedis.Set(ctx, accountcache.UsageKey(accountID), body, ttl).Err(); err != nil {
		slog.Debug("account_usage_cache_set_failed", sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
	}
}

func accountUsageInfoHasData(info AccountUsageInfo) bool {
	return len(info.Windows) > 0 || info.Credits != nil
}

func usageCachePlatformKey(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return "__all__"
	}
	return platform
}

func normalizeAccountIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func filterRefreshableUsageAccounts(accounts []Account) []Account {
	result := make([]Account, 0, len(accounts))
	for _, item := range accounts {
		if !isRefreshableUsageAccount(item) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func isRefreshableUsageAccount(item Account) bool {
	return item.Type != "apikey" && strings.TrimSpace(strings.ToLower(item.State)) != "disabled"
}

func accountIDsFromAccounts(accounts []Account) []int {
	ids := make([]int, 0, len(accounts))
	for _, item := range accounts {
		if item.ID > 0 {
			ids = append(ids, item.ID)
		}
	}
	return normalizeAccountIDs(ids)
}

func usageCacheAccountsRefreshKey(platform string, accounts []Account) string {
	return usageCacheAccountIDsRefreshKey(platform, accountIDsFromAccounts(filterRefreshableUsageAccounts(accounts)))
}

func usageCacheAccountIDsRefreshKey(platform string, ids []int) string {
	ids = normalizeAccountIDs(ids)
	if len(ids) == 0 {
		return ""
	}
	sort.Ints(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return usageCachePlatformKey(platform) + ":accounts:" + strings.Join(parts, ",")
}

func (s *Service) getUsageInfoForAccount(ctx context.Context, accountID int) (AccountUsageInfo, bool) {
	if s.usageRedis == nil {
		info, _, ok := s.getUsageInfoMemoryCache(accountID)
		return info, ok
	}
	raw, err := s.usageRedis.Get(ctx, accountcache.UsageKey(accountID)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Debug("account_usage_cache_get_failed", sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		}
		return AccountUsageInfo{}, false
	}
	var payload accountUsageCachePayload
	if err := json.Unmarshal(raw, &payload); err != nil || !payload.valid() {
		_ = s.usageRedis.Del(ctx, accountcache.UsageKey(accountID)).Err()
		return AccountUsageInfo{}, false
	}
	now := s.now()
	info, ok := payload.cacheInfo(now)
	if !ok {
		_ = s.usageRedis.Del(ctx, accountcache.UsageKey(accountID)).Err()
		return AccountUsageInfo{}, false
	}
	if !accountUsageInfoHasData(info) {
		_ = s.usageRedis.Del(ctx, accountcache.UsageKey(accountID)).Err()
		return AccountUsageInfo{}, false
	}
	return info, true
}

func (s *Service) getUsageInfosForAccounts(ctx context.Context, platform string, accounts []Account) (map[int]AccountUsageInfo, []Account) {
	result := make(map[int]AccountUsageInfo, len(accounts))
	if len(accounts) == 0 {
		return result, nil
	}
	missingAccounts := make([]Account, 0)
	if s.usageRedis == nil {
		for _, item := range accounts {
			info, fresh, ok := s.getUsageInfoMemoryCache(item.ID)
			if !ok {
				if isRefreshableUsageAccount(item) {
					missingAccounts = append(missingAccounts, item)
				}
				continue
			}
			result[item.ID] = info
			if !fresh && isRefreshableUsageAccount(item) {
				missingAccounts = append(missingAccounts, item)
			}
		}
		return result, missingAccounts
	}
	keys := make([]string, 0, len(accounts))
	ordered := make([]Account, 0, len(accounts))
	for _, item := range accounts {
		keys = append(keys, accountcache.UsageKey(item.ID))
		ordered = append(ordered, item)
	}
	values, err := s.usageRedis.MGet(ctx, keys...).Result()
	if err != nil {
		return result, filterRefreshableUsageAccounts(accounts)
	}
	staleKeys := make([]string, 0)
	for index, item := range ordered {
		raw, ok := redisValueBytes(values[index])
		if !ok {
			if isRefreshableUsageAccount(item) {
				missingAccounts = append(missingAccounts, item)
			}
			continue
		}
		var payload accountUsageCachePayload
		if err := json.Unmarshal(raw, &payload); err != nil || !payload.valid() {
			staleKeys = append(staleKeys, accountcache.UsageKey(item.ID))
			if isRefreshableUsageAccount(item) {
				missingAccounts = append(missingAccounts, item)
			}
			continue
		}
		now := s.now()
		info, ok := payload.cacheInfo(now)
		if !ok {
			staleKeys = append(staleKeys, accountcache.UsageKey(item.ID))
			if isRefreshableUsageAccount(item) {
				missingAccounts = append(missingAccounts, item)
			}
			continue
		}
		if !accountUsageInfoHasData(info) && isRefreshableUsageAccount(item) {
			staleKeys = append(staleKeys, accountcache.UsageKey(item.ID))
			missingAccounts = append(missingAccounts, item)
			continue
		}
		result[item.ID] = info
	}
	if len(staleKeys) > 0 {
		_ = s.usageRedis.Del(ctx, staleKeys...).Err()
	}
	return result, missingAccounts
}

func (s *Service) setUsageInfoMemoryCache(accountID int, platform string, info AccountUsageInfo, fetchedAt, expiresAt time.Time) {
	s.usageMu.Lock()
	s.usageCache[accountID] = &usageCacheEntry{
		platform:  strings.TrimSpace(platform),
		info:      info,
		fetchedAt: fetchedAt,
		expiresAt: expiresAt,
	}
	s.usageMu.Unlock()
}

func (s *Service) deleteUsageInfoMemoryCache(accountID int) {
	s.usageMu.Lock()
	delete(s.usageCache, accountID)
	s.usageMu.Unlock()
}

func (s *Service) getUsageInfoMemoryCache(accountID int) (AccountUsageInfo, bool, bool) {
	now := s.now()
	s.usageMu.RLock()
	entry, ok := s.usageCache[accountID]
	if !ok {
		s.usageMu.RUnlock()
		return AccountUsageInfo{}, false, false
	}
	info := entry.info
	fetchedAt := entry.fetchedAt
	expiresAt := entry.expiresAt
	fresh := now.Before(entry.expiresAt)
	s.usageMu.RUnlock()
	if fetchedAt.IsZero() {
		fetchedAt = expiresAt.Add(-usageCacheMaxTTL)
	}
	info = liveAccountUsageInfo(info, now, fetchedAt.Add(usageCacheMaxTTL))
	if !accountUsageInfoHasData(info) {
		s.deleteUsageInfoMemoryCache(accountID)
		return AccountUsageInfo{}, false, false
	}
	return info, fresh, true
}

func (s *Service) deleteUsageCacheKeysForPlatform(platform string) {
	if s.usageRedis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	members, err := s.usageRedis.SMembers(ctx, accountcache.PlatformKey(platform)).Result()
	if err != nil || len(members) == 0 {
		return
	}
	redisKeys := make([]string, 0, len(members))
	for _, member := range members {
		id, err := strconv.Atoi(member)
		if err != nil || id <= 0 {
			continue
		}
		redisKeys = append(redisKeys, accountcache.UsageKey(id))
	}
	if len(redisKeys) > 0 {
		_ = s.usageRedis.Del(ctx, redisKeys...).Err()
	}
}

func (s *Service) deleteAllUsageCacheKeys() {
	if s.usageRedis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var cursor uint64
	for {
		keys, next, err := s.usageRedis.Scan(ctx, cursor, accountcache.UsagePattern(), 50).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = s.usageRedis.Del(ctx, keys...).Err()
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

func (s *Service) ensureUsageCacheRefreshForAccounts(platform string, accounts []Account) {
	s.startUsageCacheRefreshForAccountIDs(platform, accountIDsFromAccounts(filterRefreshableUsageAccounts(accounts)))
}

func (s *Service) isUsageRefreshRunning(cacheKey string) bool {
	cacheKey = usageCachePlatformKey(cacheKey)
	s.usageRefreshMu.Lock()
	_, running := s.usageRefreshRunning[cacheKey]
	s.usageRefreshMu.Unlock()
	return running
}

func (s *Service) startUsageCacheRefreshForAccountIDs(platform string, accountIDs []int) {
	accountIDs = normalizeAccountIDs(accountIDs)
	cacheKey := usageCacheAccountIDsRefreshKey(platform, accountIDs)
	if cacheKey == "" {
		return
	}

	s.usageRefreshMu.Lock()
	if _, running := s.usageRefreshRunning[cacheKey]; running {
		s.usageRefreshMu.Unlock()
		return
	}
	s.usageRefreshRunning[cacheKey] = struct{}{}
	s.usageRefreshMu.Unlock()

	ids := append([]int(nil), accountIDs...)
	safego.Go("account_usage_cache_refresh", func() {
		s.runUsageCacheRefreshAccountIDsLoop(platform, cacheKey, ids)
	})
}

func (s *Service) runUsageCacheRefreshAccountIDsLoop(platform, cacheKey string, accountIDs []int) {
	defer func() {
		s.usageRefreshMu.Lock()
		delete(s.usageRefreshRunning, cacheKey)
		s.usageRefreshMu.Unlock()
		if r := recover(); r != nil {
			slog.Error("account_usage_cache_refresh_panic",
				sdk.LogFieldPlatform, platform,
				"panic", r)
		}
	}()
	fetchCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	accounts, err := s.repo.ListAll(fetchCtx, ListFilter{Platform: platform, IDs: accountIDs})
	var usage map[string]AccountUsageInfo
	if err == nil {
		usage, err = s.fetchUpstreamUsageForAccounts(fetchCtx, accounts)
	}
	cancel()
	if err == nil {
		writeCtx, writeCancel := context.WithTimeout(context.Background(), usageCacheWriteTimeout)
		s.cacheAccountProfiles(writeCtx, accounts)
		s.updateAccountUsageCaches(writeCtx, accounts, usage)
		writeCancel()
		return
	}
	slog.Debug("account_usage_cache_refresh_failed",
		sdk.LogFieldPlatform, platform,
		sdk.LogFieldError, err)
}

// persistRateLimitFromWindows 扫描每个账号的 windows，把"有窗口已 100%"的情况
// 当作限流态通过状态机写入（与真实 429 走同一入口）。
// 插件可在 window 上返回 ignore_limit=true，表示该窗口仅用于展示，不参与调度限流。
//
//   - 任意窗口 used_percent >= 100 → MarkRateLimited 到所有已满窗口中最晚的 reset_at
//   - 所有窗口 < 100%              → ClearRateLimited，账号回到 active
func (s *Service) persistRateLimitFromWindows(ctx context.Context, accounts map[string]any) {
	if s.stateWriter == nil {
		return
	}
	now := time.Now()
	for key, raw := range accounts {
		accountMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		windowsRaw, ok := accountMap["windows"].([]any)
		if !ok {
			continue
		}
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		var latestReset *time.Time
		anyMaxed := false
		for _, w := range windowsRaw {
			wm, ok := w.(map[string]any)
			if !ok {
				continue
			}
			if usageWindowIgnoresLimit(wm) {
				continue
			}
			pct, _ := usageNumber(wm["used_percent"])
			if pct < 100 {
				continue
			}
			anyMaxed = true
			reset := parseWindowReset(wm, now)
			if reset == nil {
				continue
			}
			if latestReset == nil || reset.After(*latestReset) {
				latestReset = reset
			}
		}

		switch {
		case anyMaxed && latestReset != nil:
			s.stateWriter.MarkRateLimited(ctx, id, *latestReset, "quota window saturated")
		case !anyMaxed:
			s.stateWriter.ClearRateLimited(ctx, id)
		}
	}
}

func usageWindowIgnoresLimit(w map[string]any) bool {
	if ignore, ok := w["ignore_limit"].(bool); ok && ignore {
		return true
	}
	if enforce, ok := w["enforce_limit"].(bool); ok && !enforce {
		return true
	}
	return false
}

// parseWindowReset 从 window map 解析 reset 时间。
// 优先使用绝对时间 reset_at（RFC3339），回退到相对秒数 reset_seconds。
func parseWindowReset(w map[string]any, now time.Time) *time.Time {
	if s, ok := w["reset_at"].(string); ok && s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return &t
		}
	}
	if secs, ok := usageNumber(w["reset_seconds"]); ok && secs > 0 {
		t := now.Add(time.Duration(secs) * time.Second)
		return &t
	}
	if secs, ok := usageNumber(w["reset_after_seconds"]); ok && secs > 0 {
		t := now.Add(time.Duration(secs) * time.Second)
		return &t
	}
	return nil
}

func usageNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

// cloneMergedShallow 浅克隆 map[accountID]accountMap 两层结构。
//
// 场景：上游缓存里存的是"纯上游数据"，返回前需要额外注入 today_stats，
// 但不能在缓存原件上打补丁（会造成并发读到半成品、或者今日 stats 被冻在缓存里）。
// 两层浅克隆就够了：我们只给外层 map 的每个 account entry 新增一个字段，
// 不会改动 windows / credits 等引用字段。
func cloneMergedShallow(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if accountMap, ok := v.(map[string]any); ok {
			accountCopy := make(map[string]any, len(accountMap)+1)
			for ak, av := range accountMap {
				accountCopy[ak] = av
			}
			dst[k] = accountCopy
		} else {
			dst[k] = v
		}
	}
	return dst
}

// enrichTodayStats 为每个账号从 usage_logs 聚合**当天**（本地时区自然日）的
// 请求数 / token 数 / 账号成本 / 用户消耗，作为 account-level `today_stats` 字段
// 注入 merged 返回体。
//
// 和上游 quota 窗口（"5h"/"7d"/"7d_spark"）完全解耦：那些窗口来自插件上报的
// upstream API percentages，这里反映的是本地 gateway 视角的账号当天真实消耗。
//
// 实现：所有账号共用同一个 startTime（今天 00:00），一次批量聚合即可。
func (s *Service) enrichTodayStats(ctx context.Context, merged map[string]any) {
	if len(merged) == 0 {
		return
	}

	// 收集所有合法的 accountID
	accountIDs := make([]int, 0, len(merged))
	accountMaps := make(map[int]map[string]any, len(merged))
	for accountIDStr, raw := range merged {
		accountID, err := strconv.Atoi(accountIDStr)
		if err != nil {
			continue
		}
		accountMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		accountIDs = append(accountIDs, accountID)
		accountMaps[accountID] = accountMap
	}
	if len(accountIDs) == 0 {
		return
	}

	now := s.now()
	day := accountcache.Day(now)
	statsMap, missingIDs := s.loadTodayStatsCache(ctx, day, accountIDs)
	if len(missingIDs) > 0 {
		// 今天 00:00（服务器本地时区；time.Local 与 usage_logs.created_at 存储时区一致）
		todayStart := timezone.StartOfDay(now.In(time.Local))
		fallback, err := s.repo.BatchWindowStats(ctx, missingIDs, todayStart)
		if err == nil {
			for _, accountID := range missingIDs {
				stats := fallback[accountID]
				statsMap[accountID] = stats
				s.writeTodayStatsCache(ctx, day, accountID, stats)
			}
		}
	}

	for accountID, accountMap := range accountMaps {
		stats, ok := statsMap[accountID]
		if !ok {
			// 没有任何请求时也回填 0，前端据此稳定展示"0 req / 0 / A $0.00 / U $0.00"
			stats = AccountWindowStats{}
		}
		accountMap["today_stats"] = map[string]any{
			"requests":     stats.Requests,
			"tokens":       stats.Tokens,
			"account_cost": stats.AccountCost,
			"user_cost":    stats.UserCost,
		}
	}
}

func (s *Service) loadTodayStatsCache(ctx context.Context, day string, accountIDs []int) (map[int]AccountWindowStats, []int) {
	result := make(map[int]AccountWindowStats, len(accountIDs))
	if s.usageRedis == nil || len(accountIDs) == 0 {
		return result, accountIDs
	}
	fields := make([]string, 0, len(accountIDs)*5)
	for _, accountID := range accountIDs {
		fields = append(fields, accountcache.TodayStatsFields(accountID)...)
	}
	values, err := s.usageRedis.HMGet(ctx, accountcache.TodayStatsKey(day), fields...).Result()
	if err != nil {
		return result, accountIDs
	}
	missing := make([]int, 0)
	for index, accountID := range accountIDs {
		offset := index * 5
		if offset+4 >= len(values) || values[offset] == nil {
			missing = append(missing, accountID)
			continue
		}
		stats, ok := parseCachedTodayStats(values[offset : offset+5])
		if !ok {
			missing = append(missing, accountID)
			continue
		}
		result[accountID] = stats
	}
	return result, missing
}

func parseCachedTodayStats(values []any) (AccountWindowStats, bool) {
	if len(values) < 4 || values[0] == nil {
		return AccountWindowStats{}, false
	}
	requests, _ := redisValueInt64(values[0])
	tokens, _ := redisValueInt64(values[1])
	accountCost, _ := redisValueFloat64(values[2])
	userCost, _ := redisValueFloat64(values[3])
	return AccountWindowStats{
		Requests:    requests,
		Tokens:      tokens,
		AccountCost: accountCost,
		UserCost:    userCost,
	}, true
}

func (s *Service) writeTodayStatsCache(ctx context.Context, day string, accountID int, stats AccountWindowStats) {
	if s.usageRedis == nil {
		return
	}
	key := accountcache.TodayStatsKey(day)
	pipe := s.usageRedis.Pipeline()
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "requests"), stats.Requests)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "tokens"), stats.Tokens)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "account_cost"), stats.AccountCost)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "user_cost"), stats.UserCost)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "updated_at"), s.now().UTC().Format(time.RFC3339))
	pipe.Expire(ctx, key, accountcache.TodayStatsTTL)
	_, _ = pipe.Exec(ctx)
}

func (s *Service) loadImageStatsCache(ctx context.Context, day string, accountIDs []int) (map[int]AccountImageStats, []int) {
	result := make(map[int]AccountImageStats, len(accountIDs))
	if s.usageRedis == nil || len(accountIDs) == 0 {
		return result, accountIDs
	}
	totalKeys := make([]string, 0, len(accountIDs))
	todayKeys := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		totalKeys = append(totalKeys, accountcache.ImageTotalKey(accountID))
		todayKeys = append(todayKeys, accountcache.ImageTodayKey(day, accountID))
	}
	totalValues, totalErr := s.usageRedis.MGet(ctx, totalKeys...).Result()
	todayValues, todayErr := s.usageRedis.MGet(ctx, todayKeys...).Result()
	if totalErr != nil || todayErr != nil {
		return result, accountIDs
	}
	missing := make([]int, 0)
	for index, accountID := range accountIDs {
		total, totalOK := redisValueInt64(totalValues[index])
		today, todayOK := redisValueInt64(todayValues[index])
		if !totalOK || !todayOK {
			missing = append(missing, accountID)
			continue
		}
		result[accountID] = AccountImageStats{TodayCount: today, TotalCount: total}
	}
	return result, missing
}

func (s *Service) writeImageStatsCache(ctx context.Context, day string, accountID int, stats AccountImageStats) {
	if s.usageRedis == nil {
		return
	}
	pipe := s.usageRedis.Pipeline()
	pipe.Set(ctx, accountcache.ImageTotalKey(accountID), stats.TotalCount, accountcache.ImageTotalTTL)
	pipe.Set(ctx, accountcache.ImageTodayKey(day, accountID), stats.TodayCount, accountcache.TodayStatsTTL)
	_, _ = pipe.Exec(ctx)
}

func (s *Service) loadAccountProfilesForUsage(ctx context.Context, platform string, accountIDs []int) ([]Account, []int) {
	if s.usageRedis == nil || len(accountIDs) == 0 {
		return nil, accountIDs
	}
	keys := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		keys = append(keys, accountcache.ProfileKey(accountID))
	}
	values, err := s.usageRedis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, accountIDs
	}

	accounts := make([]Account, 0, len(accountIDs))
	missing := make([]int, 0)
	staleKeys := make([]string, 0)
	for index, accountID := range accountIDs {
		raw, ok := redisValueBytes(values[index])
		if !ok {
			missing = append(missing, accountID)
			continue
		}
		payload, ok := decodeAccountProfileCache(raw, accountID)
		if !ok {
			staleKeys = append(staleKeys, accountcache.ProfileKey(accountID))
			missing = append(missing, accountID)
			continue
		}
		account, ok := accountProfileCacheToAccount(payload)
		if !ok || (platform != "" && account.Platform != platform) {
			missing = append(missing, accountID)
			continue
		}
		accounts = append(accounts, account)
	}
	if len(staleKeys) > 0 {
		_ = s.usageRedis.Del(ctx, staleKeys...).Err()
	}
	return accounts, missing
}

func (s *Service) cacheAccountProfiles(ctx context.Context, accounts []Account) {
	if s.usageRedis == nil || len(accounts) == 0 {
		return
	}
	validAccounts := make([]Account, 0, len(accounts))
	keys := make([]string, 0, len(accounts))
	for _, item := range accounts {
		if item.ID <= 0 {
			continue
		}
		validAccounts = append(validAccounts, item)
		keys = append(keys, accountcache.ProfileKey(item.ID))
	}
	if len(validAccounts) == 0 {
		return
	}

	oldRaw := make(map[int][]byte, len(validAccounts))
	oldPayloads := make(map[int]accountProfileCachePayload, len(validAccounts))
	if values, err := s.usageRedis.MGet(ctx, keys...).Result(); err == nil {
		for index, item := range validAccounts {
			raw, ok := redisValueBytes(values[index])
			if !ok {
				continue
			}
			oldRaw[item.ID] = raw
			if payload, ok := decodeAccountProfileCache(raw, item.ID); ok {
				oldPayloads[item.ID] = payload
			}
		}
	}

	pipe := s.usageRedis.Pipeline()
	writes := 0
	for _, item := range validAccounts {
		payload := accountProfileCacheFromAccount(item)
		body, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		if raw, ok := oldRaw[item.ID]; ok && bytes.Equal(raw, body) {
			continue
		}
		if oldPayload, ok := oldPayloads[item.ID]; ok && oldPayload.Platform != "" && oldPayload.Platform != item.Platform {
			pipe.SRem(ctx, accountcache.PlatformKey(oldPayload.Platform), item.ID)
			writes++
		}
		pipe.Set(ctx, accountcache.ProfileKey(item.ID), body, accountcache.ProfileTTL)
		writes++
		if item.Platform != "" {
			pipe.SAdd(ctx, accountcache.PlatformKey(item.Platform), item.ID)
			pipe.Expire(ctx, accountcache.PlatformKey(item.Platform), accountcache.ProfileTTL)
			writes += 2
		}
	}
	if writes > 0 {
		_, _ = pipe.Exec(ctx)
	}
}

func decodeAccountProfileCache(raw []byte, accountID int) (accountProfileCachePayload, bool) {
	var payload accountProfileCachePayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.ID != accountID {
		return accountProfileCachePayload{}, false
	}
	return payload, true
}

func accountProfileCacheFromAccount(item Account) accountProfileCachePayload {
	payload := accountProfileCachePayload{
		ID:             item.ID,
		Name:           item.Name,
		Platform:       item.Platform,
		Type:           item.Type,
		State:          item.State,
		Priority:       item.Priority,
		MaxConcurrency: item.MaxConcurrency,
		RateMultiplier: item.RateMultiplier,
		ErrorMsg:       item.ErrorMsg,
		UpstreamIsPool: item.UpstreamIsPool,
		GroupIDs:       item.GroupIDs,
		CreatedAt:      item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      item.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if item.StateUntil != nil {
		payload.StateUntil = item.StateUntil.UTC().Format(time.RFC3339)
	}
	if item.LastUsedAt != nil {
		payload.LastUsedAt = item.LastUsedAt.UTC().Format(time.RFC3339)
	}
	if item.Proxy != nil {
		proxyID := item.Proxy.ID
		payload.ProxyID = &proxyID
	}
	return payload
}

func accountProfileCacheToAccount(payload accountProfileCachePayload) (Account, bool) {
	if payload.ID <= 0 {
		return Account{}, false
	}
	account := Account{
		ID:                 payload.ID,
		Name:               payload.Name,
		Platform:           payload.Platform,
		Type:               payload.Type,
		Credentials:        map[string]string{},
		State:              payload.State,
		Priority:           payload.Priority,
		MaxConcurrency:     payload.MaxConcurrency,
		RateMultiplier:     payload.RateMultiplier,
		ErrorMsg:           payload.ErrorMsg,
		UpstreamIsPool:     payload.UpstreamIsPool,
		GroupIDs:           append([]int64(nil), payload.GroupIDs...),
		CreatedAt:          parseAccountProfileCacheTime(payload.CreatedAt),
		UpdatedAt:          parseAccountProfileCacheTime(payload.UpdatedAt),
		CurrentConcurrency: 0,
	}
	if payload.StateUntil != "" {
		parsed := parseAccountProfileCacheTime(payload.StateUntil)
		account.StateUntil = &parsed
	}
	if payload.LastUsedAt != "" {
		parsed := parseAccountProfileCacheTime(payload.LastUsedAt)
		account.LastUsedAt = &parsed
	}
	if payload.ProxyID != nil {
		account.Proxy = &Proxy{ID: *payload.ProxyID}
	}
	return account, true
}

func parseAccountProfileCacheTime(raw string) time.Time {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed
	}
	return time.Time{}
}

func redisValueBytes(value any) ([]byte, bool) {
	switch v := value.(type) {
	case string:
		return []byte(v), true
	case []byte:
		return v, true
	default:
		return nil, false
	}
}

func redisValueInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	case []byte:
		n, err := strconv.ParseInt(string(v), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func redisValueFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case string:
		n, err := strconv.ParseFloat(v, 64)
		return n, err == nil
	case []byte:
		n, err := strconv.ParseFloat(string(v), 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func (s *Service) deleteAccountCacheKeys(accountIDs []int) {
	if s.usageRedis == nil || len(accountIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	profileCmds := make(map[int]*redis.StringCmd, len(accountIDs))
	validIDs := make([]int, 0, len(accountIDs))
	readPipe := s.usageRedis.Pipeline()
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		validIDs = append(validIDs, id)
		profileCmds[id] = readPipe.Get(ctx, accountcache.ProfileKey(id))
	}
	if len(validIDs) == 0 {
		return
	}
	_, _ = readPipe.Exec(ctx)

	day := accountcache.Day(s.now())
	keys := make([]string, 0, len(validIDs)*4)
	todayStatsFields := make([]string, 0, len(validIDs)*5)
	pipe := s.usageRedis.Pipeline()
	for _, id := range validIDs {
		if raw, err := profileCmds[id].Bytes(); err == nil {
			if payload, ok := decodeAccountProfileCache(raw, id); ok && payload.Platform != "" {
				pipe.SRem(ctx, accountcache.PlatformKey(payload.Platform), id)
			}
		}
		keys = append(keys,
			accountcache.ProfileKey(id),
			accountcache.UsageKey(id),
			accountcache.ImageTotalKey(id),
			accountcache.ImageTodayKey(day, id),
		)
		todayStatsFields = append(todayStatsFields, accountcache.TodayStatsFields(id)...)
	}
	if len(keys) > 0 {
		pipe.Del(ctx, keys...)
	}
	if len(todayStatsFields) > 0 {
		pipe.HDel(ctx, accountcache.TodayStatsKey(day), todayStatsFields...)
	}
	_, _ = pipe.Exec(ctx)
}

// GetCredentialsSchema 获取指定平台凭证字段 schema。
func (s *Service) GetCredentialsSchema(platform string) CredentialSchema {
	if accountTypes := s.plugins.GetAccountTypes(platform); len(accountTypes) > 0 {
		result := CredentialSchema{
			AccountTypes: make([]AccountType, 0, len(accountTypes)),
		}
		for _, item := range accountTypes {
			accountType := AccountType{
				Key:         item.Key,
				Label:       item.Label,
				Description: item.Description,
			}
			for _, field := range item.Fields {
				accountType.Fields = append(accountType.Fields, CredentialField{
					Key:          field.Key,
					Label:        field.Label,
					Type:         field.Type,
					Required:     field.Required,
					Placeholder:  field.Placeholder,
					EditDisabled: field.EditDisabled,
				})
			}
			result.AccountTypes = append(result.AccountTypes, accountType)
		}
		if len(result.AccountTypes) > 0 {
			result.Fields = result.AccountTypes[0].Fields
		}
		return result
	}

	if fields := s.plugins.GetCredentialFields(platform); len(fields) > 0 {
		result := CredentialSchema{
			Fields: make([]CredentialField, 0, len(fields)),
		}
		for _, field := range fields {
			result.Fields = append(result.Fields, CredentialField{
				Key:          field.Key,
				Label:        field.Label,
				Type:         field.Type,
				Required:     field.Required,
				Placeholder:  field.Placeholder,
				EditDisabled: field.EditDisabled,
			})
		}
		return result
	}

	fallback := map[string]CredentialSchema{
		"openai": {
			Fields: []CredentialField{
				{Key: "api_key", Label: "API Key", Type: "password", Required: true, Placeholder: "sk-..."},
				{Key: "base_url", Label: "Base URL", Type: "text", Required: false, Placeholder: "https://api.openai.com/v1"},
			},
		},
		"claude": {
			Fields: []CredentialField{
				{Key: "api_key", Label: "API Key", Type: "password", Required: true, Placeholder: "sk-ant-..."},
				{Key: "base_url", Label: "Base URL", Type: "text", Required: false, Placeholder: "https://api.anthropic.com"},
			},
		},
		"gemini": {
			Fields: []CredentialField{
				{Key: "api_key", Label: "API Key", Type: "password", Required: true, Placeholder: "AIza..."},
			},
		},
	}

	if schema, ok := fallback[platform]; ok {
		return schema
	}

	return CredentialSchema{
		Fields: []CredentialField{
			{Key: "api_key", Label: "API Key", Type: "password", Required: true},
			{Key: "base_url", Label: "Base URL", Type: "text", Required: false},
		},
	}
}

// RefreshQuota 刷新账号额度。
func (s *Service) RefreshQuota(ctx context.Context, id int) (QuotaRefreshResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	// WithProxy: 让 queryQuotaRefresh 能把账号绑定的代理 URL 注入到 credentials
	// 里给插件使用，否则代理只对真实转发/连通性测试生效，刷额度这条独立心跳
	// 路径会直连上游 (OpenAI auth / chatgpt.com session 端点)。
	item, err := s.repo.FindByID(ctx, id, LoadOptions{WithProxy: true})
	if err != nil {
		logger.Error("account_lookup_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldError, err)
		return QuotaRefreshResult{}, err
	}

	return s.refreshQuota(ctx, item, true)
}

func (s *Service) refreshQuota(ctx context.Context, item Account, probeUsage bool) (QuotaRefreshResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	inst := s.plugins.GetPluginByPlatform(item.Platform)
	if inst == nil || inst.Gateway == nil {
		logger.Warn("account_credential_validation_failed",
			sdk.LogFieldAccountID, item.ID,
			sdk.LogFieldPlatform, item.Platform,
			sdk.LogFieldReason, "quota_refresh_unsupported")
		return QuotaRefreshResult{}, ErrQuotaRefreshUnsupported
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	quota, err := s.queryQuotaRefresh(callCtx, inst, item)
	if err != nil {
		if errors.Is(err, ErrQuotaRefreshUnsupported) {
			logger.Warn("account_credential_validation_failed",
				sdk.LogFieldAccountID, item.ID,
				sdk.LogFieldPlatform, item.Platform,
				sdk.LogFieldReason, "quota_refresh_unsupported")
			return QuotaRefreshResult{}, ErrQuotaRefreshUnsupported
		}
		if errors.Is(err, ErrReauthRequired) {
			logger.Warn("account_credential_validation_failed",
				sdk.LogFieldAccountID, item.ID,
				sdk.LogFieldPlatform, item.Platform,
				sdk.LogFieldReason, "reauth_required")
			s.recordQuotaRefreshFailure(ctx, item, "reauth_required", err, monitoring.SeverityCritical)
			return QuotaRefreshResult{}, ErrReauthRequired
		}
		logger.Error("account_credential_validation_failed",
			sdk.LogFieldAccountID, item.ID,
			sdk.LogFieldPlatform, item.Platform,
			sdk.LogFieldError, err)
		s.recordQuotaRefreshFailure(ctx, item, "quota_refresh_failed", err, monitoring.SeverityError)
		return QuotaRefreshResult{}, fmt.Errorf("刷新额度失败: %w", err)
	}

	// refresh_warning 是降级信号，不落库；取出后从 Extra 删除，避免写入 credentials。
	warning := quota.ReauthWarning
	if quota.Extra != nil {
		if w, ok := quota.Extra["refresh_warning"]; ok {
			warning = w
			delete(quota.Extra, "refresh_warning")
		}
	}

	credentials := cloneStringMap(item.Credentials)
	updated := false
	for key, value := range quota.Extra {
		if shouldPersistQuotaExtra(key, value) && credentials[key] != value {
			credentials[key] = value
			updated = true
		}
	}
	if quota.ExpiresAt != "" && credentials["subscription_active_until"] != quota.ExpiresAt {
		credentials["subscription_active_until"] = quota.ExpiresAt
		updated = true
	}
	if updated {
		if err := s.repo.SaveCredentials(ctx, item.ID, credentials); err != nil {
			logger.Error("account_credential_persist_failed",
				sdk.LogFieldAccountID, item.ID,
				"op", "save_credentials",
				sdk.LogFieldError, err)
			return QuotaRefreshResult{}, err
		}
		if s.stateWriter != nil {
			s.stateWriter.RefreshRouteGraphAccount(ctx, item.ID)
		}
	}

	if probeUsage {
		// 顺手触发一次用量强制重探测：账号额度刷新只负责刷订阅信息（plan_type / 过期时间），
		// 不动用量窗口缓存。用户点"刷新"时如果账号从没探测过，还是看不到 5h/7d 进度条。
		// 主动调一次 usage/probe 并写入该账号缓存；失败不阻断主流程。
		s.triggerUsageProbe(ctx, inst, item.Platform, item.ID, credentials)
	}
	s.resolveAccountMonitorEvents(ctx, item.ID)

	return QuotaRefreshResult{
		PlanType:                credentials["plan_type"],
		Email:                   credentials["email"],
		SubscriptionActiveUntil: credentials["subscription_active_until"],
		ReauthWarning:           warning,
	}, nil
}

type quotaRefreshRequest struct {
	ID          int               `json:"id"`
	Credentials map[string]string `json:"credentials"`
}

type quotaRefreshResponse struct {
	ExpiresAt     string            `json:"expires_at"`
	Extra         map[string]string `json:"extra"`
	ErrorCode     string            `json:"error_code"`
	ErrorMessage  string            `json:"error_message"`
	ReauthWarning string            `json:"reauth_warning"`
}

func (s *Service) queryQuotaRefresh(ctx context.Context, inst *plugin.PluginInstance, item Account) (quotaRefreshResponse, error) {
	reqBody, err := json.Marshal(quotaRefreshRequest{
		ID:          item.ID,
		Credentials: quotaRefreshCredentials(item),
	})
	if err != nil {
		return quotaRefreshResponse{}, err
	}

	statusCode, _, respBody, err := inst.Gateway.HandleHTTPRequest(ctx, "POST", "accounts/quota", "", nil, reqBody)
	if err != nil {
		return quotaRefreshResponse{}, err
	}
	if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed {
		return quotaRefreshResponse{}, ErrQuotaRefreshUnsupported
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		var resp quotaRefreshResponse
		_ = json.Unmarshal(respBody, &resp)
		if resp.ErrorCode == "reauth_required" {
			return quotaRefreshResponse{}, ErrReauthRequired
		}
		return quotaRefreshResponse{}, ErrReauthRequired
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return quotaRefreshResponse{}, fmt.Errorf("刷新额度失败: HTTP %d", statusCode)
	}

	var resp quotaRefreshResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return quotaRefreshResponse{}, fmt.Errorf("刷新额度失败: %w", err)
		}
	}
	if resp.ErrorCode == "reauth_required" {
		return quotaRefreshResponse{}, ErrReauthRequired
	}
	return resp, nil
}

func shouldPersistQuotaExtra(key, value string) bool {
	if value != "" {
		return true
	}
	switch key {
	case "plan_type", "subscription_active_until":
		return true
	default:
		return false
	}
}

// triggerUsageProbe 调用插件的 usage/probe 路径强制重探测单账号用量窗口。
// 只更新当前账号缓存；失败只记日志，不影响调用方。
func (s *Service) triggerUsageProbe(ctx context.Context, inst *plugin.PluginInstance, platform string, id int, credentials map[string]string) {
	if inst == nil || inst.Gateway == nil {
		return
	}
	reqBody, _ := json.Marshal(map[string]any{
		"id":          id,
		"credentials": credentials,
	})
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, _, respBody, err := inst.Gateway.HandleHTTPRequest(probeCtx, "POST", "usage/probe", "", nil, reqBody)
	if err != nil || status != http.StatusOK {
		slog.Debug("account_usage_probe_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldStatus, status,
			sdk.LogFieldError, err)
		return
	}
	info, usageErrors, ok := parseSingleAccountUsagePluginResponse(id, respBody)
	if len(usageErrors) > 0 {
		slog.Debug("account_usage_probe_reported_errors",
			sdk.LogFieldAccountID, id,
			"error_count", len(usageErrors))
	}
	if !ok {
		return
	}
	s.updateAccountUsageCache(ctx, platform, id, info)
}

// GetStats 获取单个账号统计。
func (s *Service) GetStats(ctx context.Context, id int, query StatsQuery) (StatsResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	item, err := s.repo.FindByID(ctx, id, LoadOptions{})
	if err != nil {
		logger.Error("account_lookup_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldError, err)
		return StatsResult{}, err
	}

	loc := timezone.Resolve(query.TZ)
	now := s.now().In(loc)
	startDate, endDate, err := ResolveStatsRange(now, query)
	if err != nil {
		return StatsResult{}, err
	}

	logs, err := s.repo.FindUsageLogs(ctx, id, startDate, endDate)
	if err != nil {
		logger.Error("account_lookup_failed",
			sdk.LogFieldAccountID, id,
			"op", "find_usage_logs",
			sdk.LogFieldError, err)
		return StatsResult{}, err
	}

	return BuildStatsResult(item, logs, now, startDate, endDate), nil
}

func buildProxyURL(proxyInfo *Proxy) string {
	if proxyInfo == nil {
		return ""
	}
	if proxyInfo.Username != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", proxyInfo.Protocol, proxyInfo.Username, proxyInfo.Password, proxyInfo.Address, proxyInfo.Port)
	}
	return fmt.Sprintf("%s://%s:%d", proxyInfo.Protocol, proxyInfo.Address, proxyInfo.Port)
}

// quotaRefreshCredentials 克隆账号 credentials 并把绑定 Proxy 拼出来的 URL
// 写到 "proxy_url" key，让插件 QueryQuota 这条心跳路径也走代理。
// 真实转发/连通性测试走 sdk.Account.ProxyURL，与本路径独立；这里只补刷额度。
//
// 用户手填 credentials["proxy_url"] 时不覆盖——既然用户主动设置了，认为是
// 有意覆写绑定的代理（也许测试用别的出口）。
func quotaRefreshCredentials(item Account) map[string]string {
	creds := cloneStringMap(item.Credentials)
	if creds == nil {
		creds = map[string]string{}
	}
	if _, exists := creds["proxy_url"]; exists {
		return creds
	}
	if url := buildProxyURL(item.Proxy); url != "" {
		creds["proxy_url"] = url
	}
	return creds
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
