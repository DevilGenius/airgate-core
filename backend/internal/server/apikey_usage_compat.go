package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent/apikey"
	"github.com/DevilGenius/airgate-core/internal/auth"
)

// API Key 用量兼容端点
//
// cc-switch 使用 GET /v1/usage 查询 Key 剩余额度；new-api 的普通
// OpenAI/自定义渠道使用两个旧版 OpenAI 账单端点计算渠道余额：
//
//	GET /v1/dashboard/billing/subscription
//	GET /v1/dashboard/billing/usage
//
// 三个端点都使用 sk-xxx Bearer Key 自鉴权。故意不复用 middleware.APIKeyAuth：
//   - APIKeyAuth 在额度耗尽时返回 402，而额度耗尽恰恰是用户最需要在 cc-switch
//     UI 上看到的状态；
//   - APIKeyAuth 要求绑定 group，但查询余额本身不需要走计费链路。

type apiKeyUsageSnapshot struct {
	Active          bool
	Balance         float64
	UsedQuota       float64
	HardLimit       float64
	APIKeyQuota     float64
	APIKeyUnlimited bool
	AccessUntil     int64
	InactiveReason  string
}

func (s *Server) loadAPIKeyUsageSnapshot(c *gin.Context) (*apiKeyUsageSnapshot, bool) {
	key := extractBearerAPIKey(c)
	if key == "" || !strings.HasPrefix(key, "sk-") {
		writeAPIKeyUsageAuthError(c, "missing or invalid api key")
		return nil, false
	}

	ak, err := s.db.APIKey.Query().
		Where(
			apikey.KeyHash(auth.HashAPIKey(key)),
			apikey.StatusEQ(apikey.StatusActive),
		).
		Only(c.Request.Context())
	if err != nil {
		writeAPIKeyUsageAuthError(c, "invalid api key")
		return nil, false
	}

	usedQuota := ak.UsedQuota
	if usedQuota < 0 {
		usedQuota = 0
	}
	balance := 1_000_000.0
	if ak.QuotaUsd > 0 {
		balance = ak.QuotaUsd - usedQuota
		if balance < 0 {
			balance = 0
		}
	}
	accessUntil := int64(0)
	inactiveReason := ""
	if ak.ExpiresAt != nil {
		accessUntil = ak.ExpiresAt.Unix()
		if ak.ExpiresAt.Before(time.Now()) {
			balance = 0
			inactiveReason = "api key expired"
		}
	}

	return &apiKeyUsageSnapshot{
		Active:          inactiveReason == "" && balance > 0,
		Balance:         balance,
		UsedQuota:       usedQuota,
		HardLimit:       usedQuota + balance,
		APIKeyQuota:     ak.QuotaUsd,
		APIKeyUnlimited: ak.QuotaUsd <= 0,
		AccessUntil:     accessUntil,
		InactiveReason:  inactiveReason,
	}, true
}

func writeAPIKeyUsageAuthError(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"is_active": false,
		"balance":   0,
		"message":   message,
	})
}

// handleAPIKeyUsage 响应 GET /v1/usage。
// balance / remaining / quota.remaining 均为 API Key 剩余额度（USD）；同时返回
// OpenAI credit_summary 字段，供支持该格式的管理端直接读取。
func (s *Server) handleAPIKeyUsage(c *gin.Context) {
	usage, ok := s.loadAPIKeyUsageSnapshot(c)
	if !ok {
		return
	}

	response := gin.H{
		"object":          "credit_summary",
		"is_active":       usage.Active,
		"balance":         usage.Balance,
		"remaining":       usage.Balance,
		"unit":            "USD",
		"total_granted":   usage.HardLimit,
		"total_used":      usage.UsedQuota,
		"total_available": usage.Balance,
		"quota": gin.H{
			"remaining": usage.Balance,
			"total":     usage.APIKeyQuota,
			"used":      usage.UsedQuota,
			"unlimited": usage.APIKeyUnlimited,
		},
	}
	if usage.InactiveReason != "" {
		response["message"] = usage.InactiveReason
	}
	c.JSON(http.StatusOK, response)
}

// handleNewAPICompatSubscription 响应 new-api 查询渠道总额度的兼容请求。
// HardLimit 使用“累计已用 + Key 剩余额度”，使 new-api 与 usage 相减后得到 Key 余额。
func (s *Server) handleNewAPICompatSubscription(c *gin.Context) {
	usage, ok := s.loadAPIKeyUsageSnapshot(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"object":                "billing_subscription",
		"has_payment_method":    true,
		"soft_limit_usd":        usage.HardLimit,
		"hard_limit_usd":        usage.HardLimit,
		"system_hard_limit_usd": usage.HardLimit,
		"access_until":          usage.AccessUntil,
	})
}

// handleNewAPICompatUsage 响应 new-api 查询累计已用额度的兼容请求。
// new-api 按美分解释 total_usage，因此 AirGate 的 USD 用量需要乘以 100。
func (s *Server) handleNewAPICompatUsage(c *gin.Context) {
	usage, ok := s.loadAPIKeyUsageSnapshot(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"object":      "list",
		"total_usage": usage.UsedQuota * 100,
	})
}

// extractBearerAPIKey 从 Authorization 头提取 Bearer token。
// 独立于 middleware.extractBearerToken（后者不导出）。
func extractBearerAPIKey(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
