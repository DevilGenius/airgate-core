package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/internal/infra/accountcache"
	"github.com/DevilGenius/airgate-core/internal/pkg/httperrors"
	"github.com/DevilGenius/airgate-core/internal/pkg/timezone"
	"github.com/DevilGenius/airgate-core/internal/safego"
)

// InvalidateUsageCache 清除指定平台的用量缓存（凭证、状态或平台变化后调用）。
// platform 为空时清理所有账号用量缓存；platform 非空时清理该平台账号缓存。
func (s *accountUsageCache) InvalidateUsageCache(platform string) {
	platform = strings.TrimSpace(platform)
	s.mu.Lock()
	if platform == "" {
		s.entries = make(map[int]*usageCacheEntry)
	} else {
		for accountID, entry := range s.entries {
			if entry != nil && entry.platform == platform {
				delete(s.entries, accountID)
			}
		}
	}
	s.mu.Unlock()

	if s.rdb == nil {
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
func (s *accountUsageService) GetAccountUsage(ctx context.Context, platform string, accountIDs []int, refresh bool) (map[string]any, bool, error) {
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
// 自动刷新路径使用批量 ids 接口；refresh=true 供凭证管理页手动刷新单个账号的
// 用量窗口。用量探测只写用量缓存和限流状态，不会更新账号 credentials（包括 plan_type）。
func (s *accountUsageService) GetSingleAccountUsage(ctx context.Context, id int, refresh bool) (map[string]any, error) {
	item, err := s.repo.FindByID(ctx, id, LoadOptions{WithProxy: true})
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

	if item.Type != "apikey" && (refresh || !hasCachedInfo) {
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

func (s *accountUsageService) fetchUpstreamUsageForAccounts(ctx context.Context, accounts []Account) (map[string]AccountUsageInfo, error) {
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
				Credentials: accountMaintenanceCredentials(item),
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
func (s *accountUsageService) fetchSingleAccountUsageDedup(ctx context.Context, item Account) (AccountUsageInfo, []accountUsageError, bool) {
	type result struct {
		info        AccountUsageInfo
		usageErrors []accountUsageError
		ok          bool
	}
	key := "single:" + usageCachePlatformKey(item.Platform) + ":" + strconv.Itoa(item.ID)
	v, _, _ := s.probeFlight.Do(key, func() (any, error) {
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

func (s *accountUsageService) fetchSingleAccountUsage(ctx context.Context, item Account) (AccountUsageInfo, []accountUsageError, bool) {
	if s.plugins == nil {
		return AccountUsageInfo{}, nil, false
	}
	inst := s.plugins.GetPluginByPlatform(item.Platform)
	if inst == nil || inst.Gateway == nil {
		return AccountUsageInfo{}, nil, false
	}

	req := accountUsageRequest{
		ID:          item.ID,
		Credentials: accountMaintenanceCredentials(item),
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

func (s *accountUsageService) handleSingleAccountUsageErrors(ctx context.Context, item Account, usageErrors []accountUsageError) {
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

func (s *accountUsageService) markAccountUsageError(ctx context.Context, accountID int, message string) {
	if s.stateWriter == nil {
		return
	}
	if httperrors.IsInactiveWorkspaceMemberError(message) {
		s.stateWriter.MarkDisabled(ctx, accountID, message)
		return
	}
	if httperrors.IsForbiddenError(message, 0) {
		s.stateWriter.MarkDegraded(ctx, accountID, message)
		return
	}
	s.stateWriter.MarkDisabled(ctx, accountID, message)
}

// updateAccountUsageCache 把单账号最新探测结果写入单账号缓存。
func (s *accountUsageService) updateAccountUsageCache(ctx context.Context, platform string, accountID int, info AccountUsageInfo) {
	if accountID <= 0 {
		return
	}
	now := s.now()
	s.observeDailyUsageGrowth(ctx, accountID, info, now)
	if existing, ok := s.getUsageInfoForAccount(ctx, accountID); ok {
		info = mergeAccountUsageInfo(existing, info, now)
	}
	s.writeUsageInfoCache(ctx, platform, accountID, info, now)
}

type accountUsageCacheWrite struct {
	account Account
	info    AccountUsageInfo
}

func (s *accountUsageService) updateAccountUsageCaches(ctx context.Context, accounts []Account, usage map[string]AccountUsageInfo) {
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
		s.observeDailyUsageGrowth(ctx, write.account.ID, write.info, now)
		info := write.info
		if cached, ok := existing[write.account.ID]; ok {
			info = mergeAccountUsageInfo(cached, info, now)
		}
		s.writeUsageInfoCache(ctx, write.account.Platform, write.account.ID, info, now)
	}
}

func (s *accountUsageService) observeDailyUsageGrowth(ctx context.Context, accountID int, info AccountUsageInfo, now time.Time) {
	observation := usageGrowthObservation(info, now.In(time.Local).Format("2006-01-02"))
	if observation.FiveHourPercent == nil && observation.SevenDayPercent == nil {
		return
	}
	if err := s.repo.ObserveUsageGrowth(ctx, accountID, observation); err != nil {
		slog.Debug("account_usage_growth_update_failed",
			sdk.LogFieldAccountID, accountID,
			sdk.LogFieldError, err)
	}
}

func usageGrowthObservation(info AccountUsageInfo, day string) UsageGrowthObservation {
	result := UsageGrowthObservation{Day: day}
	for _, window := range normalizeAccountUsageInfo(info).Windows {
		if window.Group != "base" {
			continue
		}
		value := window.UsedPercent
		switch window.Slot {
		case "5h":
			if result.FiveHourPercent == nil {
				result.FiveHourPercent = &value
			}
		case "7d":
			if result.SevenDayPercent == nil {
				result.SevenDayPercent = &value
			}
		}
	}
	return result
}

func (s *accountUsageCache) getUsageInfosForCacheWrites(ctx context.Context, writes []accountUsageCacheWrite, now time.Time) map[int]AccountUsageInfo {
	result := make(map[int]AccountUsageInfo, len(writes))
	if len(writes) == 0 {
		return result
	}
	if s.rdb == nil {
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
	values, err := s.rdb.MGet(ctx, keys...).Result()
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

func (s *accountUsageCache) writeUsageInfoCache(ctx context.Context, platform string, accountID int, info AccountUsageInfo, now time.Time) {
	info = liveAccountUsageInfo(accountUsageInfoWithAbsoluteResets(info, now), now, now.Add(usageCacheMaxTTL))
	if !accountUsageInfoHasData(info) {
		if s.rdb == nil {
			s.deleteUsageInfoMemoryCache(accountID)
		} else {
			_ = s.rdb.Del(ctx, accountcache.UsageKey(accountID)).Err()
		}
		return
	}
	expiresAt := accountUsageInfoExpiresAt(info, now)
	ttl := expiresAt.Sub(now)
	if ttl < usageCacheMinimumTTL {
		ttl = usageCacheMinimumTTL
		expiresAt = now.Add(ttl)
	}
	if s.rdb == nil {
		s.setUsageInfoMemoryCache(accountID, platform, info, now, expiresAt)
		return
	}
	payload := newAccountUsageCachePayload(info, now)
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := s.rdb.Set(ctx, accountcache.UsageKey(accountID), body, ttl).Err(); err != nil {
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

func (s *accountUsageCache) getUsageInfoForAccount(ctx context.Context, accountID int) (AccountUsageInfo, bool) {
	if s.rdb == nil {
		info, _, ok := s.getUsageInfoMemoryCache(accountID)
		return info, ok
	}
	raw, err := s.rdb.Get(ctx, accountcache.UsageKey(accountID)).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Debug("account_usage_cache_get_failed", sdk.LogFieldAccountID, accountID, sdk.LogFieldError, err)
		}
		return AccountUsageInfo{}, false
	}
	var payload accountUsageCachePayload
	if err := json.Unmarshal(raw, &payload); err != nil || !payload.valid() {
		_ = s.rdb.Del(ctx, accountcache.UsageKey(accountID)).Err()
		return AccountUsageInfo{}, false
	}
	now := s.now()
	info, ok := payload.cacheInfo(now)
	if !ok {
		_ = s.rdb.Del(ctx, accountcache.UsageKey(accountID)).Err()
		return AccountUsageInfo{}, false
	}
	if !accountUsageInfoHasData(info) {
		_ = s.rdb.Del(ctx, accountcache.UsageKey(accountID)).Err()
		return AccountUsageInfo{}, false
	}
	return info, true
}

func (s *accountUsageCache) getUsageInfosForAccounts(ctx context.Context, platform string, accounts []Account) (map[int]AccountUsageInfo, []Account) {
	result := make(map[int]AccountUsageInfo, len(accounts))
	if len(accounts) == 0 {
		return result, nil
	}
	missingAccounts := make([]Account, 0)
	if s.rdb == nil {
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
	values, err := s.rdb.MGet(ctx, keys...).Result()
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
		_ = s.rdb.Del(ctx, staleKeys...).Err()
	}
	return result, missingAccounts
}

func (s *accountUsageCache) setUsageInfoMemoryCache(accountID int, platform string, info AccountUsageInfo, fetchedAt, expiresAt time.Time) {
	s.mu.Lock()
	s.entries[accountID] = &usageCacheEntry{
		platform:  strings.TrimSpace(platform),
		info:      info,
		fetchedAt: fetchedAt,
		expiresAt: expiresAt,
	}
	s.mu.Unlock()
}

func (s *accountUsageCache) deleteUsageInfoMemoryCache(accountID int) {
	s.mu.Lock()
	delete(s.entries, accountID)
	s.mu.Unlock()
}

func (s *accountUsageCache) getUsageInfoMemoryCache(accountID int) (AccountUsageInfo, bool, bool) {
	now := s.now()
	s.mu.RLock()
	entry, ok := s.entries[accountID]
	if !ok {
		s.mu.RUnlock()
		return AccountUsageInfo{}, false, false
	}
	info := entry.info
	fetchedAt := entry.fetchedAt
	expiresAt := entry.expiresAt
	fresh := now.Before(entry.expiresAt)
	s.mu.RUnlock()
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

func (s *accountUsageCache) deleteUsageCacheKeysForPlatform(platform string) {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	members, err := s.rdb.SMembers(ctx, accountcache.PlatformKey(platform)).Result()
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
		_ = s.rdb.Del(ctx, redisKeys...).Err()
	}
}

func (s *accountUsageCache) deleteAllUsageCacheKeys() {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, accountcache.UsagePattern(), 50).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = s.rdb.Del(ctx, keys...).Err()
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}

func (s *accountUsageService) ensureUsageCacheRefreshForAccounts(platform string, accounts []Account) {
	s.startUsageCacheRefreshForAccountIDs(platform, accountIDsFromAccounts(filterRefreshableUsageAccounts(accounts)))
}

func (s *accountUsageService) isUsageRefreshRunning(cacheKey string) bool {
	cacheKey = usageCachePlatformKey(cacheKey)
	s.refreshMu.Lock()
	_, running := s.refreshing[cacheKey]
	s.refreshMu.Unlock()
	return running
}

func (s *accountUsageService) startUsageCacheRefreshForAccountIDs(platform string, accountIDs []int) {
	accountIDs = normalizeAccountIDs(accountIDs)
	cacheKey := usageCacheAccountIDsRefreshKey(platform, accountIDs)
	if cacheKey == "" {
		return
	}

	s.refreshMu.Lock()
	if _, running := s.refreshing[cacheKey]; running {
		s.refreshMu.Unlock()
		return
	}
	s.refreshing[cacheKey] = struct{}{}
	s.refreshMu.Unlock()

	ids := append([]int(nil), accountIDs...)
	safego.Go("account_usage_cache_refresh", func() {
		s.runUsageCacheRefreshAccountIDsLoop(platform, cacheKey, ids)
	})
}

func (s *accountUsageService) runUsageCacheRefreshAccountIDsLoop(platform, cacheKey string, accountIDs []int) {
	defer func() {
		s.refreshMu.Lock()
		delete(s.refreshing, cacheKey)
		s.refreshMu.Unlock()
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
func (s *accountUsageService) persistRateLimitFromWindows(ctx context.Context, accounts map[string]any) {
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
func (s *accountUsageService) enrichTodayStats(ctx context.Context, merged map[string]any) {
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

func (s *accountUsageCache) loadTodayStatsCache(ctx context.Context, day string, accountIDs []int) (map[int]AccountWindowStats, []int) {
	result := make(map[int]AccountWindowStats, len(accountIDs))
	if s.rdb == nil || len(accountIDs) == 0 {
		return result, accountIDs
	}
	fields := make([]string, 0, len(accountIDs)*5)
	for _, accountID := range accountIDs {
		fields = append(fields, accountcache.TodayStatsFields(accountID)...)
	}
	values, err := s.rdb.HMGet(ctx, accountcache.TodayStatsKey(day), fields...).Result()
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

func (s *accountUsageCache) writeTodayStatsCache(ctx context.Context, day string, accountID int, stats AccountWindowStats) {
	if s.rdb == nil {
		return
	}
	key := accountcache.TodayStatsKey(day)
	pipe := s.rdb.Pipeline()
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "requests"), stats.Requests)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "tokens"), stats.Tokens)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "account_cost"), stats.AccountCost)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "user_cost"), stats.UserCost)
	pipe.HSetNX(ctx, key, accountcache.TodayStatsField(accountID, "updated_at"), s.now().UTC().Format(time.RFC3339))
	pipe.Expire(ctx, key, accountcache.TodayStatsTTL)
	_, _ = pipe.Exec(ctx)
}

func (s *accountUsageCache) loadImageStatsCache(ctx context.Context, day string, accountIDs []int) (map[int]AccountImageStats, []int) {
	result := make(map[int]AccountImageStats, len(accountIDs))
	if s.rdb == nil || len(accountIDs) == 0 {
		return result, accountIDs
	}
	totalKeys := make([]string, 0, len(accountIDs))
	todayKeys := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		totalKeys = append(totalKeys, accountcache.ImageTotalKey(accountID))
		todayKeys = append(todayKeys, accountcache.ImageTodayKey(day, accountID))
	}
	totalValues, totalErr := s.rdb.MGet(ctx, totalKeys...).Result()
	todayValues, todayErr := s.rdb.MGet(ctx, todayKeys...).Result()
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

func (s *accountUsageCache) writeImageStatsCache(ctx context.Context, day string, accountID int, stats AccountImageStats) {
	if s.rdb == nil {
		return
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, accountcache.ImageTotalKey(accountID), stats.TotalCount, accountcache.ImageTotalTTL)
	pipe.Set(ctx, accountcache.ImageTodayKey(day, accountID), stats.TodayCount, accountcache.TodayStatsTTL)
	_, _ = pipe.Exec(ctx)
}

func (s *accountUsageCache) loadAccountProfilesForUsage(ctx context.Context, platform string, accountIDs []int) ([]Account, []int) {
	if s.rdb == nil || len(accountIDs) == 0 {
		return nil, accountIDs
	}
	keys := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		keys = append(keys, accountcache.ProfileKey(accountID))
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, accountIDs
	}

	accounts := make([]Account, 0, len(accountIDs))
	missing := make([]int, 0)
	staleKeys := make([]string, 0)
	platformFilters := splitCommaValues(platform)
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
		if !ok || !accountPlatformMatches(account.Platform, platformFilters) {
			missing = append(missing, accountID)
			continue
		}
		accounts = append(accounts, account)
	}
	if len(staleKeys) > 0 {
		_ = s.rdb.Del(ctx, staleKeys...).Err()
	}
	return accounts, missing
}

func (s *accountUsageCache) cacheAccountProfiles(ctx context.Context, accounts []Account) {
	if s.rdb == nil || len(accounts) == 0 {
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
	if values, err := s.rdb.MGet(ctx, keys...).Result(); err == nil {
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

	pipe := s.rdb.Pipeline()
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
	if item.LastProbeAt != nil {
		payload.LastProbeAt = item.LastProbeAt.UTC().Format(time.RFC3339)
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
	if payload.LastProbeAt != "" {
		parsed := parseAccountProfileCacheTime(payload.LastProbeAt)
		account.LastProbeAt = &parsed
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

func (s *accountUsageCache) forgetAccounts(accountIDs []int) {
	if len(accountIDs) == 0 {
		return
	}
	validIDs := make([]int, 0, len(accountIDs))
	s.mu.Lock()
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		validIDs = append(validIDs, id)
		delete(s.entries, id)
	}
	s.mu.Unlock()
	if s.rdb == nil || len(validIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	profileCmds := make(map[int]*redis.StringCmd, len(validIDs))
	readPipe := s.rdb.Pipeline()
	for _, id := range validIDs {
		profileCmds[id] = readPipe.Get(ctx, accountcache.ProfileKey(id))
	}
	if len(validIDs) == 0 {
		return
	}
	_, _ = readPipe.Exec(ctx)

	day := accountcache.Day(s.now())
	keys := make([]string, 0, len(validIDs)*4)
	todayStatsFields := make([]string, 0, len(validIDs)*5)
	pipe := s.rdb.Pipeline()
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
