package plugin

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/routing"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// forwardState 一次转发请求在 Core 内的上下文。
// 跨 failover attempt 稳定的字段（body / model / keyInfo / plugin）+ 每次 attempt 会被覆盖的字段（account / requestID）。
type forwardState struct {
	startedAt   time.Time
	requestPath string
	requestID   string

	body  []byte
	model string
	// dispatchPlans 是请求命中的调度候选。它同时携带客户端模型、调度模型、
	// 上游 wire model、operation 和分组开关要求。
	dispatchPlans               []sdk.DispatchPlan
	dispatchPlan                sdk.DispatchPlan
	requirements                routing.Requirements
	stream                      bool
	realtime                    bool
	sessionID                   string
	previousResponseID          string
	requireContinuationAffinity bool
	continuationRecoveryApplied bool

	// 推理强度档位快照。
	reasoningEffort string

	requestedPlatform string
	selectedRoute     routing.Candidate

	keyInfo *auth.APIKeyInfo
	plugin  *PluginInstance
	account *ent.Account
}

// forwardExecution 一次 plugin.Forward 调用的结果。
// err 仅表示"插件自身崩了"；业务判决全在 outcome.Kind。
type forwardExecution struct {
	outcome  sdk.ForwardOutcome
	err      error
	duration time.Duration
}

// parsedRequest 从 JSON body 提取的请求元信息。
type parsedRequest struct {
	Model               string
	Stream              bool
	SessionID           string
	ConversationID      string
	PromptCacheKey      string
	PreviousResponseID  string
	HasToolOutput       bool
	HasToolCallContext  bool
	HasEncryptedContent bool
	HasCompactionReplay bool
	ReasoningEffort     string // 推理强度档位
}

// requestFields 一次性 Unmarshal 的 JSON 字段结构。
type requestFields struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Metadata struct {
		UserID string `json:"user_id"`
	} `json:"metadata"`
	PromptCacheKey     string          `json:"prompt_cache_key"`
	ConversationID     string          `json:"conversation_id"`
	PreviousResponseID string          `json:"previous_response_id"`
	Input              json.RawMessage `json:"input"`
	Messages           json.RawMessage `json:"messages"`
	ReasoningEffort    string          `json:"reasoning_effort"`
	Reasoning          *struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
	OutputConfig *struct {
		Effort string `json:"effort"`
	} `json:"output_config"`
	Thinking *struct{} `json:"thinking"`
}

func (s *forwardState) schedulingModelCandidates() []string {
	if s == nil {
		return nil
	}
	if len(s.dispatchPlans) > 0 {
		out := make([]string, 0, len(s.dispatchPlans))
		seen := make(map[string]struct{}, len(s.dispatchPlans))
		for _, plan := range s.dispatchPlans {
			model := strings.TrimSpace(plan.SchedulingModel)
			if model == "" {
				continue
			}
			key := strings.ToLower(model)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, model)
		}
		if len(out) > 0 {
			return out
		}
	}
	if s.model == "" {
		return nil
	}
	return []string{s.model}
}

func (s *forwardState) modelForScheduling() string {
	if s == nil {
		return ""
	}
	if s.dispatchPlan.SchedulingModel != "" {
		return s.dispatchPlan.SchedulingModel
	}
	if len(s.dispatchPlans) > 0 {
		return s.dispatchPlans[0].SchedulingModel
	}
	return s.model
}
