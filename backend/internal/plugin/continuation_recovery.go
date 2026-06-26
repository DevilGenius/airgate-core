package plugin

import (
	"encoding/json"
	"errors"
	"strings"
)

var errContinuationRecoveryContextTooLarge = errors.New("续链恢复完整上下文过长")

func recoverContinuationAffinityMissing(state *forwardState) (bool, error) {
	return recoverContinuationAffinityMissingWithManager(nil, state)
}

func recoverContinuationAffinityMissingWithManager(manager *Manager, state *forwardState) (bool, error) {
	_ = manager
	if state == nil || state.continuationRecoveryApplied {
		return false, nil
	}
	parsed, changed, err := parseContinuationRecoveryBody(state.body)
	if err != nil {
		return false, err
	}
	if requestHasUnrecoverableContinuationInput(parsed) {
		return false, nil
	}
	if !changed && strings.TrimSpace(state.previousResponseID) == "" {
		return false, nil
	}

	state.previousResponseID = ""
	state.requireContinuationAffinity = false
	state.continuationRecoveryApplied = true
	if parsed.ReasoningEffort != "" {
		state.reasoningEffort = parsed.ReasoningEffort
	}
	return true, nil
}

func parseContinuationRecoveryBody(body []byte) (parsedRequest, bool, error) {
	var reqData map[string]any
	if err := json.Unmarshal(body, &reqData); err != nil {
		return parsedRequest{}, false, err
	}
	parsed := parseBody(body, "application/json")
	_, hasPrevious := reqData["previous_response_id"]
	return parsed, hasPrevious || parsed.HasEncryptedContent, nil
}

func requestHasUnrecoverableContinuationInput(parsed parsedRequest) bool {
	return parsed.HasToolOutput && !parsed.HasToolCallContext && !parsed.HasCompactionReplay
}
