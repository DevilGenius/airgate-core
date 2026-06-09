package modelresolver

import (
	"strings"
	"sync"
)

// Resolver 将客户端请求模型解析为调度层使用的模型候选。
type Resolver interface {
	ResolveSchedulingModels(path, clientModel string) []string
}

type defaultResolver struct{}

var (
	mu        sync.RWMutex
	resolvers          = map[string]Resolver{}
	fallback  Resolver = defaultResolver{}
)

func Register(platform string, resolver Resolver) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" || resolver == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	resolvers[platform] = resolver
}

func ForPlatform(platform string) Resolver {
	platform = strings.ToLower(strings.TrimSpace(platform))
	mu.RLock()
	resolver := resolvers[platform]
	mu.RUnlock()
	if resolver != nil {
		return resolver
	}
	return fallback
}

func ResolveSchedulingModels(platform, path, clientModel string) []string {
	return ForPlatform(platform).ResolveSchedulingModels(path, clientModel)
}

func (defaultResolver) ResolveSchedulingModels(_ string, clientModel string) []string {
	return compactUniqueModels(clientModel)
}

func compactUniqueModels(models ...string) []string {
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
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
	return out
}
