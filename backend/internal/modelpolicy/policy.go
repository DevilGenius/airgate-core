package modelpolicy

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	MaxPatternsPerPolicy = 200
	MaxPatternLength     = 256
)

var ErrInvalidPolicy = errors.New("模型策略无效")

// Policy describes allow/deny model patterns. Deny has precedence; an empty
// allow list means all models are allowed unless denied. Stored values keep
// their original casing, but compiled matching is case-insensitive.
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
	model = normalizeModelName(model)
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

func Normalize(policy Policy) Policy {
	return Policy{
		Allow: normalizePatterns(policy.Allow),
		Deny:  normalizePatterns(policy.Deny),
	}
}

func NormalizeMap(input map[string]Policy) map[string]Policy {
	if input == nil {
		return nil
	}
	normalized := make(map[string]Policy, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		normalized[key] = Normalize(value)
	}
	return normalized
}

func Validate(policy Policy) error {
	policy = Normalize(policy)
	if total := len(policy.Allow) + len(policy.Deny); total > MaxPatternsPerPolicy {
		return fmt.Errorf("%w: pattern count %d exceeds %d", ErrInvalidPolicy, total, MaxPatternsPerPolicy)
	}
	if err := validatePatternList("allow", policy.Allow); err != nil {
		return err
	}
	if err := validatePatternList("deny", policy.Deny); err != nil {
		return err
	}
	return nil
}

func ValidateMap(input map[string]Policy) error {
	for key, policy := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := Validate(policy); err != nil {
			return fmt.Errorf("%w: account type %q: %v", ErrInvalidPolicy, key, err)
		}
	}
	return nil
}

func compilePatterns(values []string) (map[string]struct{}, []string) {
	var exact map[string]struct{}
	var patterns []string
	for _, value := range values {
		value = normalizeModelName(value)
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

func validatePatternList(label string, values []string) error {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > MaxPatternLength {
			return fmt.Errorf("%w: %s pattern %q exceeds %d characters", ErrInvalidPolicy, label, value, MaxPatternLength)
		}
		if strings.ContainsAny(value, "*?[") {
			if _, err := filepath.Match(normalizeModelName(value), ""); err != nil {
				return fmt.Errorf("%w: invalid %s pattern %q: %v", ErrInvalidPolicy, label, value, err)
			}
		}
	}
	return nil
}

func normalizePatterns(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeModelName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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
