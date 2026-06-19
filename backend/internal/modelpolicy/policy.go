package modelpolicy

import (
	"path/filepath"
	"strings"
)

// Policy describes allow/deny model patterns. Deny has precedence; an empty
// allow list means all models are allowed unless denied.
type Policy struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

type Compiled struct {
	allowExact    map[string]struct{}
	allowPatterns []string
	denyExact     map[string]struct{}
	denyPatterns  []string
	hasAllow      bool
}

func Compile(policy Policy) Compiled {
	compiled := Compiled{}
	compiled.allowExact, compiled.allowPatterns = compilePatterns(policy.Allow)
	compiled.denyExact, compiled.denyPatterns = compilePatterns(policy.Deny)
	compiled.hasAllow = len(compiled.allowExact) > 0 || len(compiled.allowPatterns) > 0
	return compiled
}

func (c Compiled) Allows(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	if matchesCompiled(c.denyExact, c.denyPatterns, model) {
		return false
	}
	if !c.hasAllow {
		return true
	}
	return matchesCompiled(c.allowExact, c.allowPatterns, model)
}

func (c Compiled) Restricts() bool {
	return c.hasAllow || len(c.denyExact) > 0 || len(c.denyPatterns) > 0
}

func Clone(policy Policy) Policy {
	return Policy{
		Allow: cloneStrings(policy.Allow),
		Deny:  cloneStrings(policy.Deny),
	}
}

func CloneMap(input map[string]Policy) map[string]Policy {
	if input == nil {
		return nil
	}
	cloned := make(map[string]Policy, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		cloned[key] = Clone(value)
	}
	return cloned
}

func compilePatterns(values []string) (map[string]struct{}, []string) {
	var exact map[string]struct{}
	var patterns []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, "*?[") {
			patterns = append(patterns, value)
			continue
		}
		if exact == nil {
			exact = make(map[string]struct{})
		}
		exact[value] = struct{}{}
	}
	return exact, patterns
}

func matchesCompiled(exact map[string]struct{}, patterns []string, model string) bool {
	if _, ok := exact[model]; ok {
		return true
	}
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, model); matched {
			return true
		}
	}
	return false
}

func cloneStrings(input []string) []string {
	if input == nil {
		return nil
	}
	return append([]string(nil), input...)
}
