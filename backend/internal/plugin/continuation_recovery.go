package plugin

import (
	"encoding/json"
	"strings"
)

func recoverContinuationAffinityMissing(state *forwardState) (bool, error) {
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

	state.body = nextBody
	state.previousResponseID = ""
	state.requireContinuationAffinity = false
	state.continuationRecoveryApplied = true
	if parsed.ReasoningEffort != "" {
		state.reasoningEffort = parsed.ReasoningEffort
	}
	return true, nil
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
