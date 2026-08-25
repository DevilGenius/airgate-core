package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appdashboard "github.com/DevilGenius/airgate-core/internal/app/dashboard"
	appmonitor "github.com/DevilGenius/airgate-core/internal/app/monitor"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

const credentialAccountsOverviewSchemaVersion = 2

// CredentialAccountHandler 提供凭证管理 API Key 的只读账号概览。
//
// 该 Handler 同时挂在 /credentials 和 /admin 路由组，不参与模型转发热路径。
type CredentialAccountHandler struct {
	accounts  *appaccount.Service
	dashboard *appdashboard.Service
	runtime   *appmonitor.RuntimeSampler
}

func NewCredentialAccountHandler(
	accounts *appaccount.Service,
	dashboard *appdashboard.Service,
	runtime *appmonitor.RuntimeSampler,
) *CredentialAccountHandler {
	return &CredentialAccountHandler{
		accounts:  accounts,
		dashboard: dashboard,
		runtime:   runtime,
	}
}

// GetOverview 返回账号状态、全局流量和调度容量的精简快照。
// RPM/TPM 复用 Dashboard Service 的 1 分钟和 10 分钟统计口径；全局运行态容量复用
// RuntimeSampler 的内存快照。账号列表的当前并发只做一次批量运行态读取，
// 这些操作均不在请求转发热路径中执行。
func (h *CredentialAccountHandler) GetOverview(c *gin.Context) {
	if h == nil || h.accounts == nil || h.dashboard == nil {
		response.Error(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, "凭证账号概览服务不可用")
		return
	}

	var request dto.CredentialAccountsOverviewReq
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BindError(c, err)
		return
	}

	platform := strings.ToLower(strings.TrimSpace(request.Platform))
	if platform == "" {
		response.BadRequest(c, "缺少 platform")
		return
	}
	accountType := strings.TrimSpace(request.AccountType)
	page, pageSize := appaccount.NormalizePage(request.Page, request.PageSize)
	if pageSize > 200 {
		pageSize = 200
	}

	stats, err := h.dashboard.Stats(c.Request.Context(), 0, "")
	if err != nil {
		response.InternalError(c, "查询当前流量统计失败")
		return
	}

	accounts, err := h.accounts.ListAll(c.Request.Context(), appaccount.ListFilter{
		Platform:    platform,
		AccountType: accountType,
	})
	if err != nil {
		response.InternalError(c, "查询账号状态失败")
		return
	}

	accountIDs := make([]int, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	currentCounts := h.accounts.GetCapacity(c.Request.Context(), accountIDs)

	now := time.Now().UTC()
	result := dto.CredentialAccountsOverviewResp{
		SchemaVersion: credentialAccountsOverviewSchemaVersion,
		GeneratedAt:   now.Format(time.RFC3339),
		Traffic: dto.CredentialTrafficResp{
			Source:                  "dashboard_stats",
			RPM1M:                   stats.RPM1M,
			TPM1M:                   stats.TPM1M,
			RPM10M:                  stats.RPM10M,
			TPM10M:                  stats.TPM10M,
			TPMPerRPMCoefficient1M:  stats.TPMPerRPMCoefficient1M,
			TPMPerRPMCoefficient10M: stats.TPMPerRPMCoefficient10M,
		},
		UsageEstimate: credentialUsageEstimateResp(stats),
		AccountSummary: dto.CredentialAccountSummaryResp{
			Platform:    platform,
			AccountType: accountType,
			Total:       len(accounts),
			ByState: map[string]int{
				"active":       0,
				"rate_limited": 0,
				"degraded":     0,
				"disabled":     0,
			},
		},
		Accounts: dto.CredentialAccountsPageResp{
			Total:    len(accounts),
			Page:     page,
			PageSize: pageSize,
			List:     make([]dto.CredentialAccountResp, 0),
		},
	}

	if h.runtime != nil {
		snapshot := h.runtime.Snapshot()
		result.Scheduling = dto.CredentialSchedulingResp{
			Scope: "global",
			Capacity: dto.CredentialCapacityResp{
				InUse:           snapshot.Capacity.AccountInUse,
				Total:           snapshot.Capacity.AccountCapacity,
				Available:       maxInt(snapshot.Capacity.AccountCapacity - snapshot.Capacity.AccountInUse),
				WorkingAccounts: snapshot.Capacity.WorkingAccounts,
			},
			Queue: dto.CredentialQueueResp{
				Waiters:           snapshot.Capacity.MessageWaiters,
				WaitingAccounts:   snapshot.Capacity.WaitingAccounts,
				MaxAccountWaiters: snapshot.Capacity.MaxAccountWaiters,
			},
		}
		if !snapshot.SampledAt.IsZero() {
			result.Scheduling.SampledAt = snapshot.SampledAt.UTC().Format(time.RFC3339)
		}
	}

	for _, account := range accounts {
		current := currentCounts[account.ID]
		if account.State == "active" && account.MaxConcurrency > 0 {
			result.AccountSummary.ConfiguredCapacity += account.MaxConcurrency
		}
		result.AccountSummary.CurrentConcurrency += current
		result.AccountSummary.ByState[account.State]++
	}
	start := len(accounts)
	totalPages := (len(accounts) + pageSize - 1) / pageSize
	if page <= totalPages {
		start = (page - 1) * pageSize
	}
	if start < len(accounts) {
		end := start + pageSize
		if end > len(accounts) {
			end = len(accounts)
		}
		result.Accounts.List = make([]dto.CredentialAccountResp, 0, end-start)
		for _, account := range accounts[start:end] {
			result.Accounts.List = append(result.Accounts.List, credentialAccountResp(account, currentCounts[account.ID]))
		}
	}

	response.Success(c, result)
}

func credentialUsageEstimateResp(stats appdashboard.Stats) dto.CredentialUsageEstimateResp {
	insufficient := func() dto.CredentialUsageAvailabilityResp {
		return dto.CredentialUsageAvailabilityResp{Status: "insufficient"}
	}
	result := dto.CredentialUsageEstimateResp{
		StandardCostPerMinute1M:  stats.AccountCostPerMinute1M,
		StandardCostPerMinute10M: stats.AccountCostPerMinute10M,
		Plus5h:                   insufficient(),
		Pro5h:                    insufficient(),
		Plus7d:                   insufficient(),
		Pro7d:                    insufficient(),
	}
	for _, estimate := range stats.UsageEstimates {
		for _, window := range estimate.Windows {
			availability := dto.CredentialUsageAvailabilityResp{
				Status:                window.Status,
				AvailableMinutes:      window.RemainingMinutes,
				AvailableStandardCost: window.RemainingCost,
			}
			switch {
			case estimate.Plan == "plus" && window.Window == "5h":
				result.Plus5h = availability
			case estimate.Plan == "pro" && window.Window == "5h":
				result.Pro5h = availability
			case estimate.Plan == "plus" && window.Window == "7d":
				result.Plus7d = availability
			case estimate.Plan == "pro" && window.Window == "7d":
				result.Pro7d = availability
			}
		}
	}
	return result
}

func credentialAccountResp(account appaccount.Account, current int) dto.CredentialAccountResp {
	result := dto.CredentialAccountResp{
		ID:                 int64(account.ID),
		Name:               account.Name,
		Platform:           account.Platform,
		Type:               account.Type,
		PlanType:           appaccount.NormalizePlanType(account.Credentials["plan_type"]),
		State:              account.State,
		Priority:           account.Priority,
		MaxConcurrency:     account.MaxConcurrency,
		CurrentConcurrency: current,
		StateReason:        truncateCredentialStateReason(account.ErrorMsg),
	}
	if account.Email != nil {
		result.Email = *account.Email
	}
	if account.StateUntil != nil {
		result.StateUntil = account.StateUntil.UTC().Format(time.RFC3339)
	}
	if account.LastUsedAt != nil {
		result.LastUsedAt = account.LastUsedAt.UTC().Format(time.RFC3339)
	}
	if account.LastProbeAt != nil {
		result.LastProbeAt = account.LastProbeAt.UTC().Format(time.RFC3339)
	}
	return result
}

func truncateCredentialStateReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) <= 256 {
		return reason
	}
	return reason[:256]
}

func maxInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
