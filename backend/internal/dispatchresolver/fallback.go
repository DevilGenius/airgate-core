package dispatchresolver

import (
	"github.com/DevilGenius/airgate-core/internal/forwardpath"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
	"strings"
)

// FallbackTarget keeps fallback policy in Core. Execution plans sent to plugins
// contain only the selected model; plugins report conditions, never targets.
func FallbackTarget(platform string, group *CompiledResolver, method, path string, plan sdk.DispatchPlan, outcome sdk.ForwardOutcome) (string, bool) {
	if !outcome.RequestsModelFallback() {
		return "", false
	}
	rule := group.matchRule(method, path, plan.ClientModel)
	if rule == nil {
		rule = platformResolver(platform).matchRule(method, path, plan.ClientModel)
	}
	if rule == nil {
		return "", false
	}
	target := renderTemplate(rule.contextWindowFallback, plan.ClientModel, rule.baseModel(plan.ClientModel), plan.SchedulingModel)
	if target == "" || strings.EqualFold(strings.TrimSpace(plan.ClientModel), target) {
		return "", false
	}
	return target, true
}

func (r *CompiledResolver) matchRule(method, path, clientModel string) *compiledRule {
	if r == nil {
		return nil
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = forwardpath.Normalize(path)
	clientModel = strings.TrimSpace(clientModel)
	for i := range r.rules {
		rule := &r.rules[i]
		if rule.when.matches(method, path, clientModel) && len(rule.renderPlans(clientModel)) > 0 {
			return rule
		}
	}
	return nil
}

func (r compiledRule) baseModel(clientModel string) string {
	clientModel = strings.TrimSpace(clientModel)
	suffix := strings.TrimSpace(r.stripSuffix)
	if suffix != "" && strings.HasSuffix(strings.ToLower(clientModel), strings.ToLower(suffix)) && len(clientModel) > len(suffix) {
		return strings.TrimSpace(clientModel[:len(clientModel)-len(suffix)])
	}
	return clientModel
}
