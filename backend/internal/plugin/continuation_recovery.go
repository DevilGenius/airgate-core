package plugin

import (
	"encoding/json"
	"errors"
	"strings"

	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

const (
	continuationRecoveryBytesPerToken = 6
	continuationRecoveryMinBodyBytes  = 512 << 10
	continuationRecoveryMaxBodyBytes  = defaultGatewayBodyLimit
	continuationRecoveryDefaultWindow = 272000
)

var errContinuationRecoveryContextTooLarge = errors.New("续链恢复完整上下文过长")

func recoverContinuationAffinityMissing(state *forwardState) (bool, error) {
	return recoverContinuationAffinityMissingWithManager(nil, state)
}

func recoverContinuationAffinityMissingWithManager(manager *Manager, state *forwardState) (bool, error) {
	if state == nil || state.continuationRecoveryApplied {
		return false, nil
	}
	nextBody, parsed, changed, err := buildContinuationRecoveryBody(state.body)
	if err != nil {
		return false, err
	}
	if requestRequiresContinuationAffinity(parsed) {
		return false, nil
	}
	if !changed && strings.TrimSpace(state.previousResponseID) == "" {
		return false, nil
	}
	if continuationRecoveryBodyTooLarge(manager, state, parsed, nextBody) {
		return false, errContinuationRecoveryContextTooLarge
	}

	state.body = nextBody
	state.previousResponseID = ""
	state.requireContinuationAffinity = false
	state.continuationRecoveryApplied = true
	if parsed.ReasoningEffort != "" {
		state.reasoningEffort = parsed.ReasoningEffort
	}
	return true, nil
}

func continuationRecoveryBodyTooLarge(manager *Manager, state *forwardState, parsed parsedRequest, body []byte) bool {
	return len(body) > continuationRecoveryMaxBytes(manager, state, parsed)
}

func continuationRecoveryMaxBytes(manager *Manager, state *forwardState, parsed parsedRequest) int {
	if parsed.HasCompactionReplay {
		return continuationRecoveryMaxBodyBytes
	}
	contextWindow := continuationRecoveryContextWindow(manager, state, parsed)
	limit := contextWindow * continuationRecoveryBytesPerToken
	if limit < continuationRecoveryMinBodyBytes {
		return continuationRecoveryMinBodyBytes
	}
	if limit > continuationRecoveryMaxBodyBytes {
		return continuationRecoveryMaxBodyBytes
	}
	return limit
}

func continuationRecoveryContextWindow(manager *Manager, state *forwardState, parsed parsedRequest) int {
	if manager != nil {
		for _, platform := range continuationRecoveryPlatforms(state) {
			models := manager.GetModels(platform)
			if contextWindow := findContinuationModelContextWindow(models, continuationRecoveryModelCandidates(state, parsed)); contextWindow > 0 {
				return contextWindow
			}
		}
	}
	return continuationRecoveryDefaultWindow
}

func continuationRecoveryPlatforms(state *forwardState) []string {
	if state == nil {
		return nil
	}
	seen := map[string]bool{}
	var platforms []string
	add := func(platform string) {
		platform = strings.TrimSpace(platform)
		if platform == "" || seen[platform] {
			return
		}
		seen[platform] = true
		platforms = append(platforms, platform)
	}
	add(state.requestedPlatform)
	add(state.selectedRoute.Platform)
	if state.plugin != nil {
		add(state.plugin.Platform)
	}
	return platforms
}

func continuationRecoveryModelCandidates(state *forwardState, parsed parsedRequest) []string {
	seen := map[string]bool{}
	var models []string
	add := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			return
		}
		seen[model] = true
		models = append(models, model)
	}
	add(parsed.Model)
	if state != nil {
		add(state.dispatchPlan.SchedulingModel)
		for _, plan := range state.dispatch.Plans() {
			add(plan.SchedulingModel)
			add(plan.UpstreamModel())
		}
		add(state.model)
	}
	return models
}

func findContinuationModelContextWindow(models []sdk.ModelInfo, candidates []string) int {
	if len(models) == 0 || len(candidates) == 0 {
		return 0
	}
	byID := make(map[string]int, len(models))
	for _, model := range models {
		id := strings.ToLower(strings.TrimSpace(model.ID))
		if id != "" && model.ContextWindow > 0 {
			byID[id] = model.ContextWindow
		}
	}
	for _, candidate := range candidates {
		if contextWindow := byID[strings.ToLower(strings.TrimSpace(candidate))]; contextWindow > 0 {
			return contextWindow
		}
	}
	return 0
}

func buildContinuationRecoveryBody(body []byte) ([]byte, parsedRequest, bool, error) {
	var reqData map[string]any
	if err := json.Unmarshal(body, &reqData); err != nil {
		return nil, parsedRequest{}, false, err
	}

	changed := false
	if _, ok := reqData["previous_response_id"]; ok {
		delete(reqData, "previous_response_id")
		changed = true
	}
	if trimEncryptedReasoningItems(reqData) {
		changed = true
	}
	if !changed {
		return body, parseBody(body, "application/json"), false, nil
	}

	nextBody, err := json.Marshal(reqData)
	if err != nil {
		return nil, parsedRequest{}, false, err
	}
	return nextBody, parseBody(nextBody, "application/json"), true, nil
}

func trimEncryptedReasoningItems(reqData map[string]any) bool {
	if len(reqData) == 0 {
		return false
	}
	input, ok := reqData["input"]
	if !ok {
		return false
	}

	switch v := input.(type) {
	case []any:
		filtered := v[:0]
		changed := false
		for _, item := range v {
			next, itemChanged, keep := sanitizeEncryptedReasoningInputItem(item)
			if itemChanged {
				changed = true
			}
			if keep {
				filtered = append(filtered, next)
			}
		}
		if !changed {
			return false
		}
		if len(filtered) == 0 {
			delete(reqData, "input")
		} else {
			reqData["input"] = filtered
		}
		return true
	case map[string]any:
		next, changed, keep := sanitizeEncryptedReasoningInputItem(v)
		if !changed {
			return false
		}
		if !keep {
			delete(reqData, "input")
		} else {
			reqData["input"] = next
		}
		return true
	default:
		return false
	}
}

func sanitizeEncryptedReasoningInputItem(item any) (next any, changed bool, keep bool) {
	itemMap, ok := item.(map[string]any)
	if !ok {
		return item, false, true
	}
	if strings.TrimSpace(asString(itemMap["type"])) != "reasoning" {
		return item, false, true
	}
	if strings.TrimSpace(asString(itemMap["encrypted_content"])) == "" {
		return item, false, true
	}

	delete(itemMap, "encrypted_content")
	if len(itemMap) == 1 {
		return nil, true, false
	}
	return itemMap, true, true
}
