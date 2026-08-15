package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/DevilGenius/airgate-core/internal/accountimportdsl"
	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appsettings "github.com/DevilGenius/airgate-core/internal/app/settings"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

// AccountHandler 上游账号管理 Handler。
//
// scheduler 用来读家族级限流冷却（Redis 侧的瞬态状态，不在 DB 里），
// 后台账号列表/详情会带上 family_cooldowns 字段。允许 nil 退化为不展示冷却信息。
type AccountHandler struct {
	service         *appaccount.Service
	scheduler       *scheduler.Scheduler
	settingsService *appsettings.Service
}

type configuredImportError struct {
	client bool
	err    error
}

func (e *configuredImportError) Error() string { return e.err.Error() }
func (e *configuredImportError) Unwrap() error { return e.err }

func isConfiguredImportClientError(err error) bool {
	var target *configuredImportError
	return errors.As(err, &target) && target.client
}

// NewAccountHandler 创建 AccountHandler。sched 可为 nil（旧测试入口），
// 此时 family_cooldowns 字段会缺省为空。
func NewAccountHandler(service *appaccount.Service, sched *scheduler.Scheduler, settings ...*appsettings.Service) *AccountHandler {
	handler := &AccountHandler{service: service, scheduler: sched}
	if len(settings) > 0 {
		handler.settingsService = settings[0]
	}
	return handler
}

func (h *AccountHandler) accountImportDSL(ctx context.Context) (string, error) {
	if h.settingsService == nil {
		return accountimportdsl.DefaultConfigJSON, nil
	}
	items, err := h.settingsService.List(ctx, accountimportdsl.SettingGroup)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.Key == accountimportdsl.SettingKey {
			if strings.TrimSpace(item.Value) == "" {
				return accountimportdsl.DefaultConfigJSON, nil
			}
			return item.Value, nil
		}
	}
	return accountimportdsl.DefaultConfigJSON, nil
}

func (h *AccountHandler) applyConfiguredImport(
	ctx context.Context,
	inputs []appaccount.CreateInput,
) ([]appaccount.CreateInput, error) {
	rawConfig, err := h.accountImportDSL(ctx)
	if err != nil {
		return nil, &configuredImportError{err: fmt.Errorf("读取导入配置失败: %w", err)}
	}
	config, err := accountimportdsl.Parse(rawConfig)
	if err != nil {
		return nil, &configuredImportError{err: fmt.Errorf("导入配置无效: %w", err)}
	}
	var occupiedPriorities map[int]int
	if config.UsesPrioritySequence() {
		occupiedPriorities, err = h.service.OccupiedPriorities(ctx)
		if err != nil {
			return nil, &configuredImportError{err: fmt.Errorf("读取已占用优先级失败: %w", err)}
		}
	}
	inputs, err = config.ApplyWithOccupiedPriorities(inputs, occupiedPriorities)
	if err != nil {
		return nil, &configuredImportError{client: true, err: fmt.Errorf("应用导入配置失败: %w", err)}
	}
	return inputs, nil
}

func (h *AccountHandler) importConfiguredAccounts(
	ctx context.Context,
	inputs []appaccount.CreateInput,
) appaccount.ImportSummary {
	summary := h.service.ImportConfigured(ctx, inputs)
	if len(summary.SuccessIDs) > 0 {
		h.activateCreatedAccounts(ctx, summary.SuccessIDs)
	}
	return summary
}

// familyCooldownsFor 拉取指定账号在 Redis 上仍生效的家族冷却，转成 DTO。
// scheduler 为 nil 或没有冷却时返回 nil；不阻断主响应。
func (h *AccountHandler) familyCooldownsFor(ctx context.Context, accountID int) []dto.FamilyCooldownDTO {
	if h.scheduler == nil {
		return nil
	}
	entries := h.scheduler.ListFamilyCooldowns(ctx, accountID)
	return familyCooldownDTOs(entries)
}

// familyCooldownsForAccounts 批量拉取当前页账号的 Redis 家族冷却，避免账号列表自动刷新时逐行访问 Redis。
func (h *AccountHandler) familyCooldownsForAccounts(ctx context.Context, accountIDs []int) map[int][]dto.FamilyCooldownDTO {
	if h.scheduler == nil || len(accountIDs) == 0 {
		return nil
	}
	entriesByAccount := h.scheduler.ListFamilyCooldownsBatch(ctx, accountIDs)
	if len(entriesByAccount) == 0 {
		return nil
	}
	out := make(map[int][]dto.FamilyCooldownDTO, len(entriesByAccount))
	for accountID, entries := range entriesByAccount {
		if dtos := familyCooldownDTOs(entries); len(dtos) > 0 {
			out[accountID] = dtos
		}
	}
	return out
}

func familyCooldownDTOs(entries []scheduler.FamilyCooldownEntry) []dto.FamilyCooldownDTO {
	if len(entries) == 0 {
		return nil
	}
	out := make([]dto.FamilyCooldownDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto.FamilyCooldownDTO{
			Family: e.Family,
			Until:  e.Until.UTC().Format(time.RFC3339),
			Reason: e.Reason,
		})
	}
	return out
}

// modelDemotionsFor 读取账号当前 30 分钟桶内因成功率低于阈值被降级的调度模型（内存态）。
// scheduler 为 nil、未配置阈值或没有降级时返回 nil；不阻断主响应。
func (h *AccountHandler) modelDemotionsFor(account appaccount.Account) []dto.ModelDemotionDTO {
	if h.scheduler == nil || account.ModelDowngradeThreshold <= 0 {
		return nil
	}
	demotions := h.scheduler.ListModelDemotions(account.ID, account.ModelDowngradeThreshold)
	if len(demotions) == 0 {
		return nil
	}
	out := make([]dto.ModelDemotionDTO, 0, len(demotions))
	for _, item := range demotions {
		out = append(out, dto.ModelDemotionDTO{
			Model:         item.Model,
			SuccessRate:   item.SuccessRate,
			ValidRequests: item.ValidRequests,
		})
	}
	return out
}

func parseAccountID(raw string) (int, error) {
	return strconv.Atoi(raw)
}

func parseOptionalInt(raw string) *int {
	if raw == "" {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &value
}

func parseOptionalBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// parseIDList 解析逗号分隔的整数列表（如 "1,2,3"），忽略空项与非法项。
func parseIDList(raw string) []int {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, err := strconv.Atoi(p); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}

func (h *AccountHandler) handleError(logMessage, publicMessage string, err error) (int, string) {
	switch {
	case errors.Is(err, appaccount.ErrAccountNotFound):
		return 404, err.Error()
	case errors.Is(err, appaccount.ErrAccountEmailExists):
		return 409, err.Error()
	case errors.Is(err, appaccount.ErrPluginNotFound):
		return 500, err.Error()
	case errors.Is(err, appaccount.ErrReauthRequired):
		// 这里的"需要重新授权"说的是**上游账号**（OAuth）的凭证失效，不是当前
		// 登录用户的 session。绝对不能返回 401——前端 HTTP 客户端有全局拦截，
		// 看到 401 会把当前管理员踹出登录页。用 422 语义最贴切：请求合法但
		// 因账号状态无法处理。
		return 422, err.Error()
	case errors.Is(err, appaccount.ErrModelRequired),
		errors.Is(err, appaccount.ErrTokenRefreshUnsupported),
		errors.Is(err, appaccount.ErrInvalidDateRange),
		errors.Is(err, appaccount.ErrInvalidState),
		errors.Is(err, appaccount.ErrInvalidRateMultiplier),
		errors.Is(err, appaccount.ErrInvalidModelDowngradeThreshold),
		errors.Is(err, appaccount.ErrInvalidAccountEmail),
		errors.Is(err, appaccount.ErrAccountEmailMismatch),
		errors.Is(err, appaccount.ErrInvalidModelPolicy):
		return 400, err.Error()
	default:
		slog.Error(logMessage, "error", err)
		return 500, publicMessage
	}
}

func (h *AccountHandler) refreshRouteGraphAccount(ctx context.Context, accountID int) {
	if h.scheduler != nil {
		h.scheduler.RefreshRouteGraphAccount(ctx, accountID)
	}
}

func (h *AccountHandler) refreshRouteGraphAccounts(ctx context.Context, accountIDs []int) {
	if h.scheduler == nil {
		return
	}
	for _, accountID := range accountIDs {
		h.scheduler.RefreshRouteGraphAccount(ctx, accountID)
	}
}

func (h *AccountHandler) activateCreatedAccount(ctx context.Context, accountID int) {
	if h.scheduler != nil {
		h.scheduler.ClearRateLimitMarkers(ctx, accountID)
	}
	h.refreshRouteGraphAccount(ctx, accountID)
}

func (h *AccountHandler) activateCreatedAccounts(ctx context.Context, accountIDs []int) {
	for _, accountID := range accountIDs {
		h.activateCreatedAccount(ctx, accountID)
	}
}

func (h *AccountHandler) removeRouteGraphAccount(accountID int) {
	if h.scheduler != nil {
		h.scheduler.RemoveRouteGraphAccount(accountID)
	}
}

func (h *AccountHandler) removeRouteGraphAccounts(accountIDs []int) {
	if h.scheduler == nil {
		return
	}
	for _, accountID := range accountIDs {
		h.scheduler.RemoveRouteGraphAccount(accountID)
	}
}
