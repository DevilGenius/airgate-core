package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/internal/accountpriority"
	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	"github.com/DevilGenius/airgate-core/internal/modelpolicy"
	"github.com/DevilGenius/airgate-core/internal/monitoring"
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

const autoTokenRefreshInterval = 6 * time.Hour

const deprecatedGroupPrioritiesExtraKey = "group_priorities"

// StateWriter 管理员巡检场景下对账号状态的写入口。
// 由 scheduler 包实现；让 account service 不直接依赖 scheduler。
type StateWriter interface {
	// ApplyAccountTestOutcome 使用与调度请求相同的状态机规则处理账号测试结果。
	// 账号测试不占用调度 RPM，因此实现方不应执行调度请求专用的 RPM 回退。
	ApplyAccountTestOutcome(ctx context.Context, accountID int, platform, model string, outcome sdk.ForwardOutcome, isPool bool)
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

// AccountDeletionObserver 接收账号删除事件，用于清理其它域拥有的账号级运行时状态。
// 回调在删除 Redis 缓存前同步执行，确保删除后的状态不会再次被后台持久化。
type AccountDeletionObserver interface {
	OnAccountsDeleted(accountIDs []int)
}

// Service 提供账号域用例编排；账号用量探测与缓存由内部 accountUsageService 负责。
type Service struct {
	repo             Repository
	plugins          PluginCatalog
	concurrency      ConcurrencyReader
	stateWriter      StateWriter
	deletionObserver AccountDeletionObserver
	monitor          monitoring.Recorder
	now              func() time.Time
	usage            *accountUsageService
}

// NewService 创建账号服务。
// stateWriter 可传 nil（测试场景）；nil 时额度巡检不会主动标记账号状态。
func NewService(repo Repository, plugins PluginCatalog, concurrency ConcurrencyReader, stateWriter StateWriter) *Service {
	service := &Service{
		repo:        repo,
		plugins:     plugins,
		concurrency: concurrency,
		stateWriter: stateWriter,
		now:         time.Now,
	}
	service.usage = newAccountUsageService(repo, plugins, stateWriter, func() time.Time {
		return service.now()
	})
	return service
}

// SetUsageCacheRedis enables the cross-process account usage cache.
func (s *Service) SetUsageCacheRedis(rdb *redis.Client) {
	if s == nil || s.usage == nil {
		return
	}
	s.usage.setRedis(rdb)
}

// InvalidateUsageCache invalidates cached account usage for one platform, or
// all platforms when platform is empty.
func (s *Service) InvalidateUsageCache(platform string) {
	if s == nil || s.usage == nil {
		return
	}
	s.usage.InvalidateUsageCache(platform)
}

// GetAccountUsage returns the cached account usage view and schedules refresh
// work through the dedicated usage service when needed.
func (s *Service) GetAccountUsage(ctx context.Context, platform string, accountIDs []int, refresh bool) (map[string]any, bool, error) {
	return s.usage.GetAccountUsage(ctx, platform, accountIDs, refresh)
}

// GetSingleAccountUsage returns one account's usage view.
func (s *Service) GetSingleAccountUsage(ctx context.Context, id int, refresh bool) (map[string]any, error) {
	return s.usage.GetSingleAccountUsage(ctx, id, refresh)
}

// SetAccountDeletionObserver injects the synchronous account deletion hook.
func (s *Service) SetAccountDeletionObserver(observer AccountDeletionObserver) {
	if s == nil {
		return
	}
	s.deletionObserver = observer
}

// SetMonitorRecorder injects the best-effort monitor event recorder.
func (s *Service) SetMonitorRecorder(recorder monitoring.Recorder) {
	if s == nil {
		return
	}
	s.monitor = recorder
}

// StartTokenRefreshLoop periodically refreshes OAuth tokens and account plan metadata written into credentials.
func (s *Service) StartTokenRefreshLoop(ctx context.Context) {
	safego.Go("account_token_refresh_loop", func() { s.runTokenRefreshLoop(ctx) })
}

func (s *Service) runTokenRefreshLoop(ctx context.Context) {
	s.refreshAllOAuthTokens(ctx)

	ticker := time.NewTicker(autoTokenRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAllOAuthTokens(ctx)
		}
	}
}

func (s *Service) refreshAllOAuthTokens(ctx context.Context) {
	logger := sdk.LoggerFromContext(ctx)
	accounts, err := s.repo.ListAll(ctx, ListFilter{})
	if err != nil {
		logger.Warn("account_token_auto_refresh_list_failed", sdk.LogFieldError, err)
		return
	}

	success, failed, skipped := 0, 0, 0
	for _, item := range accounts {
		if ctx.Err() != nil {
			return
		}
		if !shouldAutoRefreshToken(item) {
			skipped++
			continue
		}
		if _, err := s.refreshToken(ctx, item, false); err != nil {
			failed++
			logger.Warn("account_token_auto_refresh_failed",
				sdk.LogFieldAccountID, item.ID,
				sdk.LogFieldPlatform, item.Platform,
				sdk.LogFieldError, err)
			continue
		}
		success++
	}

	logger.Info("account_token_auto_refresh_complete", "success", success, "failed", failed, "skipped", skipped)
}

func shouldAutoRefreshToken(item Account) bool {
	if len(item.Credentials) == 0 {
		return false
	}
	accountType := strings.ToLower(strings.TrimSpace(item.Type))
	if accountType == "apikey" || accountType == "api_key" {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(item.Credentials["auth_mode"]))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(item.Credentials["authMode"]))
	}
	mode = strings.ReplaceAll(mode, "_", "")
	runtimeID := item.Credentials["agent_runtime_id"]
	if strings.TrimSpace(runtimeID) == "" {
		runtimeID = item.Credentials["agentRuntimeId"]
	}
	privateKey := item.Credentials["agent_private_key"]
	if strings.TrimSpace(privateKey) == "" {
		privateKey = item.Credentials["agentPrivateKey"]
	}
	if mode == "agentidentity" ||
		(strings.TrimSpace(runtimeID) != "" && strings.TrimSpace(privateKey) != "") {
		return true
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
	s.hydrateAndCacheAccountListRuntimeData(ctx, accounts, workingCounts)

	return ListResult{
		List:     accounts,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// HydrateAccountListRuntimeData fills runtime-only list fields after callers
// perform a non-DB filter such as Redis-backed family cooldown filtering.
func (s *Service) HydrateAccountListRuntimeData(ctx context.Context, accounts []Account) {
	s.hydrateAndCacheAccountListRuntimeData(ctx, accounts, nil)
}

func (s *Service) hydrateAndCacheAccountListRuntimeData(ctx context.Context, accounts []Account, workingCounts map[int]int) {
	s.hydrateAccountListRuntimeData(ctx, accounts, workingCounts)
	s.usage.cacheAccountProfiles(ctx, accounts)
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
		imageStats, missingIDs := s.usage.loadImageStatsCache(ctx, day, openaiIDs)
		if len(missingIDs) > 0 {
			todayStart := timezone.StartOfDay(s.now().In(time.Local))
			if fallback, err := s.repo.BatchImageStats(ctx, missingIDs, todayStart); err == nil {
				for _, id := range missingIDs {
					stats := fallback[id]
					imageStats[id] = stats
					s.usage.writeImageStatsCache(ctx, day, id, stats)
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

// WorkingAccountIDs returns accounts with live concurrency for overlapping
// admin state filters such as working + active.
func (s *Service) WorkingAccountIDs(ctx context.Context) []int {
	return mapKeys(s.workingConcurrencyCounts(ctx))
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
	if err := validateModelDowngradeThreshold(input.ModelDowngradeThreshold); err != nil {
		return Account{}, err
	}
	input.RateMultiplier = &rateMultiplier
	input.ModelPolicy = modelpolicy.Normalize(input.ModelPolicy)
	if err := validateModelPolicy(input.ModelPolicy); err != nil {
		return Account{}, err
	}
	input.Email, input.Credentials, err = normalizeAccountIdentity(input.Email, input.Credentials)
	if err != nil {
		return Account{}, err
	}
	input.Extra = stripDeprecatedAccountExtra(input.Extra)

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
	s.usage.cacheAccountProfiles(ctx, []Account{account})
	s.InvalidateUsageCache("") // 新账号创建后清除用量缓存
	return account, err
}

// ExportAll 查询符合筛选条件的全部账号（用于导出，不分页、不带并发计数）。
func (s *Service) ExportAll(ctx context.Context, filter ListFilter) ([]Account, error) {
	return s.ListAll(ctx, filter)
}

// ListAll 查询符合筛选条件的全部账号，不填充列表运行态字段。
func (s *Service) ListAll(ctx context.Context, filter ListFilter) ([]Account, error) {
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

// OccupiedPriorities 返回现有账号按优先级聚合后的占用数量。
func (s *Service) OccupiedPriorities(ctx context.Context) (map[int]int, error) {
	return s.repo.OccupiedPriorities(ctx, nil)
}

// Import 批量导入账号，逐条创建并收集失败信息（不使用事务，允许部分成功）。
func (s *Service) Import(ctx context.Context, items []CreateInput) ImportSummary {
	return s.importAccounts(ctx, items, false)
}

// ImportConfigured 批量导入已由 Core 导入 DSL 生成的账号。
// GroupIDs 只来自服务端保存并校验过的 DSL，因此允许写入；普通文件导入仍使用 Import，
// 继续清空跨环境的分组和代理引用。
func (s *Service) ImportConfigured(ctx context.Context, items []CreateInput) ImportSummary {
	return s.importAccounts(ctx, items, true)
}

// ValidateConfiguredImport 使用正式导入相同的字段归一化和校验逻辑检查账号草稿，
// 但不访问持久化层，也不会写入数据库。
func (s *Service) ValidateConfiguredImport(_ context.Context, items []CreateInput) []ImportItemError {
	errors := make([]ImportItemError, 0)
	for index, input := range items {
		if _, err := prepareImportAccount(input, true); err != nil {
			errors = append(errors, ImportItemError{
				Index:   index,
				Name:    input.Name,
				Message: err.Error(),
			})
		}
	}
	return errors
}

func (s *Service) importAccounts(ctx context.Context, items []CreateInput, preserveGroupIDs bool) ImportSummary {
	summary := ImportSummary{}
	for index, input := range items {
		prepared, err := prepareImportAccount(input, preserveGroupIDs)
		if err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, ImportItemError{
				Index:   index,
				Name:    input.Name,
				Message: err.Error(),
			})
			continue
		}
		created, err := s.repo.Create(ctx, prepared)
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

func prepareImportAccount(input CreateInput, preserveGroupIDs bool) (CreateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	if input.Name == "" {
		return CreateInput{}, errors.New("账号名称不能为空")
	}
	if input.Platform == "" {
		return CreateInput{}, errors.New("账号平台不能为空")
	}
	if len(input.Credentials) == 0 {
		return CreateInput{}, errors.New("账号凭证不能为空")
	}
	if input.Type == "" {
		if strings.TrimSpace(input.Credentials["api_key"]) != "" {
			input.Type = "apikey"
		} else {
			input.Type = "oauth"
		}
	}
	if input.MaxConcurrency < 0 {
		return CreateInput{}, errors.New("账号容量不能小于 0")
	}
	input.Priority = accountpriority.Clamp(input.Priority)

	rateMultiplier, err := normalizeCreateRateMultiplier(input.RateMultiplier)
	if err != nil {
		return CreateInput{}, err
	}
	input.RateMultiplier = &rateMultiplier
	input.ModelPolicy = modelpolicy.Normalize(input.ModelPolicy)
	if err := validateModelPolicy(input.ModelPolicy); err != nil {
		return CreateInput{}, err
	}
	input.Email, input.Credentials, err = normalizeAccountIdentity(input.Email, input.Credentials)
	if err != nil {
		return CreateInput{}, err
	}
	if !preserveGroupIDs {
		input.GroupIDs = nil
	}
	input.ProxyID = nil
	input.Extra = stripDeprecatedAccountExtra(input.Extra)
	return input, nil
}

// Update 更新账号。
func (s *Service) Update(ctx context.Context, id int, input UpdateInput) (Account, error) {
	logger := sdk.LoggerFromContext(ctx)
	if input.HasEmail || input.Credentials != nil {
		current, err := s.repo.FindByID(ctx, id, LoadOptions{})
		if err != nil {
			return Account{}, err
		}
		input, err = normalizeAccountIdentityUpdate(current, input)
		if err != nil {
			return Account{}, err
		}
	}
	if input.RateMultiplier != nil {
		if err := validateRateMultiplier(*input.RateMultiplier); err != nil {
			return Account{}, err
		}
	}
	if input.ModelDowngradeThreshold != nil {
		if err := validateModelDowngradeThreshold(*input.ModelDowngradeThreshold); err != nil {
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
	if input.HasExtra {
		input.Extra = stripDeprecatedAccountExtra(input.Extra)
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
	s.usage.cacheAccountProfiles(ctx, []Account{updated})
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
	s.deleteAccountRuntimeState([]int{id})
	return err
}

// BulkUpdate 批量更新账号。逐条执行并收集每个账号的成功/失败信息，允许部分成功。
// group_ids 为整体替换：若提供则覆盖账号原有分组，未提供则不触碰。
func (s *Service) BulkUpdate(ctx context.Context, input BulkUpdateInput) BulkResult {
	result := BulkResult{Results: make([]BulkResultItem, 0, len(input.IDs))}
	priorityModeCount := 0
	if input.Priority != nil {
		priorityModeCount++
	}
	if input.PriorityOffset != nil {
		priorityModeCount++
	}
	if input.PrioritySequence != nil {
		priorityModeCount++
	}
	if priorityModeCount > 1 {
		for _, id := range input.IDs {
			result.appendFailure(id, ErrConflictingPriorityUpdate)
		}
		return result
	}
	if err := validatePrioritySequenceInput(input.PrioritySequence); err != nil {
		for _, id := range input.IDs {
			result.appendFailure(id, err)
		}
		return result
	}
	var occupiedPriorities map[int]int
	var err error
	if input.PrioritySequence != nil {
		occupiedPriorities, err = s.repo.OccupiedPriorities(ctx, input.IDs)
		if err != nil {
			for _, id := range input.IDs {
				result.appendFailure(id, fmt.Errorf("读取已占用优先级失败: %w", err))
			}
			return result
		}
	}
	sequencePriorities, err := buildPrioritySequence(input.PrioritySequence, len(input.IDs), occupiedPriorities)
	if err != nil {
		for _, id := range input.IDs {
			result.appendFailure(id, err)
		}
		return result
	}
	if input.RateMultiplier != nil {
		if err := validateRateMultiplier(*input.RateMultiplier); err != nil {
			for _, id := range input.IDs {
				result.appendFailure(id, err)
			}
			return result
		}
	}
	if input.ModelDowngradeThreshold != nil {
		if err := validateModelDowngradeThreshold(*input.ModelDowngradeThreshold); err != nil {
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
	for index, id := range input.IDs {
		priority := input.Priority
		if sequencePriorities != nil {
			priority = &sequencePriorities[index]
		}
		patch := UpdateInput{
			State:                   input.State,
			Priority:                priority,
			MaxConcurrency:          input.MaxConcurrency,
			RateMultiplier:          input.RateMultiplier,
			ModelDowngradeThreshold: input.ModelDowngradeThreshold,
			ModelPolicy:             input.ModelPolicy,
		}
		var existing *Account
		needsExisting := input.HasExtra || (input.PriorityOffset != nil && *input.PriorityOffset != 0)
		if needsExisting {
			account, err := s.repo.FindByID(ctx, id, LoadOptions{})
			if err != nil {
				result.appendFailure(id, err)
				continue
			}
			existing = &account
		}
		if input.PriorityOffset != nil && *input.PriorityOffset != 0 {
			nextPriority, ok := accountpriority.AddOffset(existing.Priority, *input.PriorityOffset)
			if !ok {
				result.appendFailure(id, fmt.Errorf("%w：当前优先级 %d，偏移量 %+d", ErrInvalidPriorityOffset, existing.Priority, *input.PriorityOffset))
				continue
			}
			patch.Priority = &nextPriority
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
			patch.Extra = stripDeprecatedAccountExtra(mergeAnyMap(existing.Extra, input.Extra))
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
			s.usage.cacheAccountProfiles(ctx, accounts)
		}
	}
	if result.Success > 0 && input.State != nil {
		s.InvalidateUsageCache("")
	}
	return result
}

func buildPrioritySequence(input *PrioritySequenceInput, count int, occupied map[int]int) ([]int, error) {
	if input == nil {
		return nil, nil
	}
	if err := validatePrioritySequenceInput(input); err != nil {
		return nil, err
	}
	if occupied == nil {
		occupied = make(map[int]int)
	}

	priorities := make([]int, count)
	sequenceInput := accountpriority.SequenceInput{
		Initial:      input.Initial,
		Step:         input.Step,
		GroupSize:    input.GroupSize,
		Min:          accountpriority.Min,
		Max:          accountpriority.Max,
		OverflowMode: accountpriority.OverflowClampAfterFull,
	}
	state := accountpriority.SequenceState{}
	for index := range priorities {
		priority, err := accountpriority.NextSequencePriority(sequenceInput, occupied, &state)
		if err != nil {
			return nil, fmt.Errorf("%w：第 %d 个账号的优先级序列超出 %d 到 %d 范围", ErrInvalidPrioritySequence, index+1, accountpriority.Min, accountpriority.Max)
		}
		priorities[index] = priority
	}
	return priorities, nil
}

func validatePrioritySequenceInput(input *PrioritySequenceInput) error {
	if input == nil {
		return nil
	}
	if input.GroupSize <= 0 {
		return fmt.Errorf("%w：每组账号数必须大于 0", ErrInvalidPrioritySequence)
	}
	if input.Step == 0 {
		return fmt.Errorf("%w：步进不能为 0", ErrInvalidPrioritySequence)
	}
	if input.Initial < accountpriority.Min || input.Initial > accountpriority.Max {
		return fmt.Errorf("%w：初始值必须在 %d 到 %d 范围内", ErrInvalidPrioritySequence, accountpriority.Min, accountpriority.Max)
	}
	return nil
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
		s.deleteAccountRuntimeState(result.SuccessIDs)
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
		input.HasEmail ||
		input.Type != nil ||
		input.Credentials != nil ||
		input.State != nil ||
		input.Priority != nil ||
		input.MaxConcurrency != nil ||
		input.RateMultiplier != nil ||
		input.ModelDowngradeThreshold != nil ||
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

func validateModelDowngradeThreshold(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return ErrInvalidModelDowngradeThreshold
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

func stripDeprecatedAccountExtra(extra map[string]any) map[string]any {
	if _, exists := extra[deprecatedGroupPrioritiesExtraKey]; !exists {
		return extra
	}
	cleaned := maps.Clone(extra)
	delete(cleaned, deprecatedGroupPrioritiesExtraKey)
	return cleaned
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
		run: func(runCtx context.Context, writer http.ResponseWriter) (ConnectivityTestTiming, error) {
			req := *forwardReq
			req.Writer = writer
			outcome, forwardErr := inst.Gateway.Forward(runCtx, &req)
			timing := connectivityTestTiming(outcome)
			if forwardErr != nil {
				s.applyConnectivityTestOutcome(runCtx, item, modelID, outcome, forwardErr)
				s.recordConnectivityTestFailure(runCtx, item, modelID, "plugin_forward_error", forwardErr)
				return timing, forwardErr
			}
			// 测试路径严格判定：只有 OutcomeSuccess 算通过；任何其它 Kind 都报告失败。
			// 这是管理员工具，失败原因要保留真实上游诊断，方便直接排查账号态 / 上游态问题。
			if outcome.Kind == sdk.OutcomeSuccess {
				s.applyConnectivityTestOutcome(runCtx, item, modelID, outcome, nil)
				s.resolveAccountMonitorEvents(runCtx, item.ID)
				return timing, nil
			}
			msg := connectivityTestErrorMessage(outcome)
			if msg == "" && outcome.Upstream.StatusCode > 0 {
				msg = fmt.Sprintf("upstream returned HTTP %d", outcome.Upstream.StatusCode)
			}
			if msg == "" {
				msg = fmt.Sprintf("plugin returned %s", outcome.Kind)
			}
			err := errors.New(msg)
			s.applyConnectivityTestOutcome(runCtx, item, modelID, outcome, err)
			s.recordConnectivityTestFailure(runCtx, item, modelID, outcome.Kind.String(), err)
			return timing, err
		},
	}, nil
}

func connectivityTestTiming(outcome sdk.ForwardOutcome) ConnectivityTestTiming {
	timing := ConnectivityTestTiming{DurationMs: outcome.Duration.Milliseconds()}
	if outcome.Usage != nil {
		timing.FirstEventMs = outcome.Usage.FirstEventMs
	}
	return timing
}

func (s *Service) applyConnectivityTestOutcome(ctx context.Context, item Account, modelID string, outcome sdk.ForwardOutcome, forwardErr error) {
	if s == nil || s.stateWriter == nil || item.ID <= 0 {
		return
	}
	if strings.TrimSpace(outcome.Reason) == "" && forwardErr != nil {
		outcome.Reason = forwardErr.Error()
	}
	s.stateWriter.ApplyAccountTestOutcome(ctx, item.ID, item.Platform, modelID, outcome, item.UpstreamIsPool)
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
	case sdk.OutcomeAccountQuotaExhausted:
		if reason != "" {
			return "上游账号额度不足: " + reason
		}
		return "上游账号额度不足，请充值或更换账号"
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

func (s *Service) deleteAccountRuntimeState(accountIDs []int) {
	if s.deletionObserver != nil {
		s.deletionObserver.OnAccountsDeleted(accountIDs)
	}
	if s.usage != nil {
		s.usage.forgetAccounts(accountIDs)
	}
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

// RefreshToken 刷新账号令牌及订阅元数据。
func (s *Service) RefreshToken(ctx context.Context, id int) (TokenRefreshResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	// WithProxy: 让 queryTokenRefresh 能把账号绑定的代理 URL 注入到 credentials
	// 里给插件使用，否则代理只对真实转发/连通性测试生效，刷新令牌这条独立心跳
	// 路径会直连上游 (OpenAI auth / chatgpt.com session 端点)。
	item, err := s.repo.FindByID(ctx, id, LoadOptions{WithProxy: true})
	if err != nil {
		logger.Error("account_lookup_failed",
			sdk.LogFieldAccountID, id,
			sdk.LogFieldError, err)
		return TokenRefreshResult{}, err
	}

	return s.refreshToken(ctx, item, true)
}

func (s *Service) refreshToken(ctx context.Context, item Account, probeUsage bool) (TokenRefreshResult, error) {
	logger := sdk.LoggerFromContext(ctx)
	inst := s.plugins.GetPluginByPlatform(item.Platform)
	if inst == nil || inst.Gateway == nil {
		logger.Warn("account_credential_validation_failed",
			sdk.LogFieldAccountID, item.ID,
			sdk.LogFieldPlatform, item.Platform,
			sdk.LogFieldReason, "token_refresh_unsupported")
		return TokenRefreshResult{}, ErrTokenRefreshUnsupported
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := s.queryTokenRefresh(callCtx, inst, item)
	if err != nil {
		if errors.Is(err, ErrTokenRefreshUnsupported) {
			logger.Warn("account_credential_validation_failed",
				sdk.LogFieldAccountID, item.ID,
				sdk.LogFieldPlatform, item.Platform,
				sdk.LogFieldReason, "token_refresh_unsupported")
			return TokenRefreshResult{}, ErrTokenRefreshUnsupported
		}
		if errors.Is(err, ErrReauthRequired) {
			logger.Warn("account_credential_validation_failed",
				sdk.LogFieldAccountID, item.ID,
				sdk.LogFieldPlatform, item.Platform,
				sdk.LogFieldReason, "reauth_required")
			s.recordTokenRefreshFailure(ctx, item, "reauth_required", err, monitoring.SeverityCritical)
			return TokenRefreshResult{}, ErrReauthRequired
		}
		logger.Error("account_credential_validation_failed",
			sdk.LogFieldAccountID, item.ID,
			sdk.LogFieldPlatform, item.Platform,
			sdk.LogFieldError, err)
		s.recordTokenRefreshFailure(ctx, item, "token_refresh_failed", err, monitoring.SeverityError)
		return TokenRefreshResult{}, fmt.Errorf("刷新令牌失败: %w", err)
	}

	// refresh_warning 是降级信号，不落库；取出后从 Extra 删除，避免写入 credentials。
	warning := result.ReauthWarning
	if result.Extra != nil {
		if w, ok := result.Extra["refresh_warning"]; ok {
			warning = w
			delete(result.Extra, "refresh_warning")
		}
	}
	resolvedEmail, resolvedCredentials, err := normalizeAccountIdentity(item.Email, item.Credentials)
	if err != nil {
		return TokenRefreshResult{}, err
	}
	refreshedEmail := resolvedEmail
	if rawEmail, ok := result.Extra["email"]; ok {
		delete(result.Extra, "email")
		normalizedEmail, normalizeErr := normalizeAccountEmail(&rawEmail)
		if normalizeErr != nil {
			return TokenRefreshResult{}, normalizeErr
		}
		if normalizedEmail != nil {
			refreshedEmail = normalizedEmail
		}
	}

	credentials := cloneStringMap(resolvedCredentials)
	if credentials == nil {
		credentials = map[string]string{}
	}
	for key, value := range result.Extra {
		if shouldPersistTokenRefreshExtra(key, value) {
			credentials[key] = value
		}
	}
	if result.ExpiresAt != "" {
		credentials["subscription_active_until"] = result.ExpiresAt
	}
	credentials = syncAccountCredentials(credentials, refreshedEmail)
	var usageProbeCredentials map[string]string
	if probeUsage {
		usageProbeCredentials = accountMaintenanceCredentials(Account{
			Credentials: credentials,
			Proxy:       item.Proxy,
		})
	}
	credentialsChanged := !maps.Equal(item.Credentials, credentials)
	emailChanged := !accountEmailsEqual(item.Email, refreshedEmail)
	if credentialsChanged || emailChanged {
		patch := UpdateInput{
			Credentials: credentials,
			Email:       refreshedEmail,
			HasEmail:    true,
		}
		persisted, persistErr := s.repo.Update(ctx, item.ID, patch)
		if persistErr != nil {
			logger.Error("account_credential_persist_failed",
				sdk.LogFieldAccountID, item.ID,
				"op", "update_credentials",
				sdk.LogFieldError, persistErr)
			return TokenRefreshResult{}, persistErr
		}
		item = persisted
		if s.stateWriter != nil {
			s.stateWriter.RefreshRouteGraphAccount(ctx, item.ID)
		}
	}

	if probeUsage {
		// 顺手触发一次用量强制重探测：账号令牌刷新只负责刷订阅信息（plan_type / 过期时间），
		// 不动用量窗口缓存。用户点"刷新"时如果账号从没探测过，还是看不到 5h/7d 进度条。
		// 主动调一次 usage/probe 并写入该账号缓存；失败不阻断主流程。
		s.triggerUsageProbe(ctx, inst, item.Platform, item.ID, usageProbeCredentials)
	}
	s.resolveAccountMonitorEvents(ctx, item.ID)
	email := ""
	if item.Email != nil {
		email = *item.Email
	}

	return TokenRefreshResult{
		PlanType:                credentials["plan_type"],
		Email:                   email,
		SubscriptionActiveUntil: credentials["subscription_active_until"],
		ReauthWarning:           warning,
	}, nil
}

type tokenRefreshRequest struct {
	ID          int               `json:"id"`
	Credentials map[string]string `json:"credentials"`
}

type tokenRefreshResponse struct {
	ExpiresAt     string            `json:"expires_at"`
	Extra         map[string]string `json:"extra"`
	ErrorCode     string            `json:"error_code"`
	ErrorMessage  string            `json:"error_message"`
	ReauthWarning string            `json:"reauth_warning"`
}

func (s *Service) queryTokenRefresh(ctx context.Context, inst *plugin.PluginInstance, item Account) (tokenRefreshResponse, error) {
	reqBody, err := json.Marshal(tokenRefreshRequest{
		ID:          item.ID,
		Credentials: accountMaintenanceCredentials(item),
	})
	if err != nil {
		return tokenRefreshResponse{}, err
	}

	statusCode, _, respBody, err := inst.Gateway.HandleHTTPRequest(ctx, "POST", "accounts/token-refresh", "", nil, reqBody)
	if err != nil {
		return tokenRefreshResponse{}, err
	}
	if statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed {
		return tokenRefreshResponse{}, ErrTokenRefreshUnsupported
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		var resp tokenRefreshResponse
		_ = json.Unmarshal(respBody, &resp)
		if resp.ErrorCode == "reauth_required" {
			return tokenRefreshResponse{}, ErrReauthRequired
		}
		return tokenRefreshResponse{}, ErrReauthRequired
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return tokenRefreshResponse{}, fmt.Errorf("刷新令牌失败: HTTP %d", statusCode)
	}

	var resp tokenRefreshResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return tokenRefreshResponse{}, fmt.Errorf("刷新令牌失败: %w", err)
		}
	}
	if resp.ErrorCode == "reauth_required" {
		return tokenRefreshResponse{}, ErrReauthRequired
	}
	return resp, nil
}

func shouldPersistTokenRefreshExtra(key, value string) bool {
	if key == "email" {
		return false
	}
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
	s.usage.updateAccountUsageCache(ctx, platform, id, info)
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

// accountMaintenanceCredentials 克隆账号 credentials 并把绑定 Proxy 拼出来的
// URL 写到 "proxy_url" key，让 token refresh、usage probe 等账号维护请求走代理。
// 真实转发/连通性测试走 sdk.Account.ProxyURL，与这些独立维护路径无关。
//
// 用户手填 credentials["proxy_url"] 时不覆盖——既然用户主动设置了，认为是
// 有意覆写绑定的代理（也许测试用别的出口）。
func accountMaintenanceCredentials(item Account) map[string]string {
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
