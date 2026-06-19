package dispatchresolver

import (
	"strings"
	"sync"

	"github.com/DevilGenius/airgate-core/internal/forwardpath"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// CompiledResolver 是不可变的请求调度 DSL 解析器。
type CompiledResolver struct {
	rules []compiledRule
}

type compiledRule struct {
	id             string
	when           compiledWhen
	stripSuffix    string
	operation      string
	timeoutProfile string
	gate           sdk.DispatchGate
	candidates     []sdk.DispatchCandidate
}

type compiledWhen struct {
	methods       map[string]struct{}
	paths         map[string]struct{}
	pathPrefixes  []string
	models        map[string]struct{}
	modelPrefixes []string
	modelSuffixes []string
}

var (
	mu        sync.RWMutex
	resolvers = map[string]*CompiledResolver{}
	cacheMu   sync.RWMutex
	cached    = map[string]*CompiledResolver{}
)

// RegisterPlatformDSL 注册平台级默认 DSL。
func RegisterPlatformDSL(platform string, dsl sdk.DispatchDSL) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		return
	}
	compiled := Compile(dsl)
	mu.Lock()
	defer mu.Unlock()
	if compiled == nil {
		delete(resolvers, platform)
		return
	}
	resolvers[platform] = compiled
}

// UnregisterPlatformDSL 移除平台级默认 DSL。
func UnregisterPlatformDSL(platform string) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		return
	}
	mu.Lock()
	delete(resolvers, platform)
	mu.Unlock()
}

func platformResolver(platform string) *CompiledResolver {
	platform = strings.ToLower(strings.TrimSpace(platform))
	mu.RLock()
	resolver := resolvers[platform]
	mu.RUnlock()
	return resolver
}

// Compile 把 DispatchDSL 编译为高效匹配结构。
func Compile(dsl sdk.DispatchDSL) *CompiledResolver {
	if len(dsl.Rules) == 0 {
		return nil
	}
	rules := make([]compiledRule, 0, len(dsl.Rules))
	for _, rule := range dsl.Rules {
		if len(rule.Candidates) == 0 {
			continue
		}
		rules = append(rules, compiledRule{
			id:             strings.TrimSpace(rule.ID),
			when:           compileWhen(rule.When),
			stripSuffix:    strings.TrimSpace(rule.Model.StripSuffix),
			operation:      strings.TrimSpace(rule.Operation),
			timeoutProfile: strings.TrimSpace(rule.TimeoutProfile),
			gate:           rule.Gate,
			candidates:     append([]sdk.DispatchCandidate(nil), rule.Candidates...),
		})
	}
	if len(rules) == 0 {
		return nil
	}
	return &CompiledResolver{rules: rules}
}

// CompileCached returns a cached compiled resolver for a stable cache key.
func CompileCached(cacheKey string, dsl sdk.DispatchDSL) *CompiledResolver {
	cacheKey = strings.TrimSpace(cacheKey)
	if cacheKey == "" {
		return Compile(dsl)
	}
	cacheMu.RLock()
	resolver := cached[cacheKey]
	cacheMu.RUnlock()
	if resolver != nil {
		return resolver
	}
	resolver = Compile(dsl)
	cacheMu.Lock()
	cached[cacheKey] = resolver
	cacheMu.Unlock()
	return resolver
}

// ResolveDispatchPlans 先尝试 group 级 DSL，再回退到平台默认 DSL，最后回退为 identity。
func ResolveDispatchPlans(platform string, groupResolver *CompiledResolver, method, path, clientModel string) []sdk.DispatchPlan {
	if plans := groupResolver.ResolveDispatchPlans(method, path, clientModel); len(plans) > 0 {
		return plans
	}
	if resolver := platformResolver(platform); resolver != nil {
		if plans := resolver.ResolveDispatchPlans(method, path, clientModel); len(plans) > 0 {
			return plans
		}
	}
	return identityPlan(clientModel)
}

// ResolveDispatchPlans 用编译后的 resolver 解析一次请求。
func (r *CompiledResolver) ResolveDispatchPlans(method, path, clientModel string) []sdk.DispatchPlan {
	if r == nil || len(r.rules) == 0 {
		return nil
	}
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	normalizedPath := forwardpath.Normalize(path)
	model := strings.TrimSpace(clientModel)

	for _, rule := range r.rules {
		if !rule.when.matches(normalizedMethod, normalizedPath, model) {
			continue
		}
		if plans := rule.renderPlans(model); len(plans) > 0 {
			return plans
		}
	}
	return nil
}

func compileWhen(when sdk.DispatchWhen) compiledWhen {
	return compiledWhen{
		methods:       compileSet(when.Methods, strings.ToUpper),
		paths:         compilePathSet(when.Paths),
		pathPrefixes:  compilePathPrefixes(when.PathPrefixes),
		models:        compileSet(when.Models, strings.ToLower),
		modelPrefixes: compileList(when.ModelPrefixes, strings.ToLower),
		modelSuffixes: compileList(when.ModelSuffixes, strings.ToLower),
	}
}

func compileSet(values []string, normalize func(string) string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalize(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func compilePathSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = forwardpath.Normalize(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func compilePathPrefixes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = forwardpath.Normalize(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func compileList(values []string, normalize func(string) string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalize(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (w compiledWhen) matches(method, path, model string) bool {
	if len(w.methods) > 0 {
		if _, ok := w.methods[method]; !ok {
			return false
		}
	}
	if len(w.paths) > 0 || len(w.pathPrefixes) > 0 {
		if _, ok := w.paths[path]; !ok {
			matchedPrefix := false
			for _, prefix := range w.pathPrefixes {
				if pathHasAPIPrefix(path, prefix) {
					matchedPrefix = true
					break
				}
			}
			if !matchedPrefix {
				return false
			}
		}
	}
	if len(w.models) > 0 || len(w.modelPrefixes) > 0 || len(w.modelSuffixes) > 0 {
		modelKey := strings.ToLower(strings.TrimSpace(model))
		if _, ok := w.models[modelKey]; !ok {
			matched := false
			for _, prefix := range w.modelPrefixes {
				if strings.HasPrefix(modelKey, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				for _, suffix := range w.modelSuffixes {
					if strings.HasSuffix(modelKey, suffix) {
						matched = true
						break
					}
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}

func (r compiledRule) renderPlans(clientModel string) []sdk.DispatchPlan {
	clientModel = strings.TrimSpace(clientModel)
	modelBase := clientModel
	if suffix := strings.TrimSpace(r.stripSuffix); suffix != "" {
		modelKey := strings.ToLower(clientModel)
		suffixKey := strings.ToLower(suffix)
		if strings.HasSuffix(modelKey, suffixKey) && len(clientModel) > len(suffix) {
			modelBase = strings.TrimSpace(clientModel[:len(clientModel)-len(suffix)])
		}
	}
	out := make([]sdk.DispatchPlan, 0, len(r.candidates))
	seen := make(map[string]struct{}, len(r.candidates))
	for _, candidate := range r.candidates {
		scheduling := renderTemplate(candidate.Scheduling, clientModel, modelBase, "")
		if scheduling == "" {
			continue
		}
		wire := renderTemplate(candidate.Wire, clientModel, modelBase, scheduling)
		if wire == "" {
			wire = scheduling
		}
		key := strings.ToLower(scheduling) + "\x00" + strings.ToLower(wire)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sdk.DispatchPlan{
			ClientModel:     clientModel,
			SchedulingModel: scheduling,
			WireModel:       wire,
			RuleID:          r.id,
			Operation:       r.operation,
			TimeoutProfile:  r.timeoutProfile,
			Gate:            r.gate,
		})
	}
	return out
}

func renderTemplate(tpl, model, modelBase, scheduling string) string {
	out := strings.TrimSpace(tpl)
	if out == "" {
		return ""
	}
	out = strings.ReplaceAll(out, "${model.base}", modelBase)
	out = strings.ReplaceAll(out, "${model}", model)
	out = strings.ReplaceAll(out, "${scheduling}", scheduling)
	return strings.TrimSpace(out)
}

func identityPlan(clientModel string) []sdk.DispatchPlan {
	model := strings.TrimSpace(clientModel)
	if model == "" {
		return nil
	}
	return []sdk.DispatchPlan{{
		ClientModel:     model,
		SchedulingModel: model,
		WireModel:       model,
	}}
}

func pathHasAPIPrefix(path, prefix string) bool {
	if prefix == "" || !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/'
}
