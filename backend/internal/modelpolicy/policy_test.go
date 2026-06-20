package modelpolicy

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledAllowsCaseInsensitiveExact(t *testing.T) {
	compiled := Compile(Policy{
		Allow: []string{"GPT-5.4"},
	})

	if !compiled.Restricts() {
		t.Fatal("expected non-empty allow list to restrict models")
	}
	if !compiled.Allows(" ") {
		t.Fatal("empty model should pass policy")
	}
	if !compiled.Allows("gpt-5.4") {
		t.Fatal("expected lower-case model to match mixed-case allow exact")
	}
	if !compiled.Allows("GPT-5.4") {
		t.Fatal("expected original-case model to match allow exact")
	}
	if compiled.Allows("gpt-4o") {
		t.Fatal("unexpected model should not pass non-empty allow list")
	}
}

func TestCompiledAllowsCaseInsensitiveGlobAndDenyPrecedence(t *testing.T) {
	compiled := Compile(Policy{
		Allow: []string{"GPT-5*"},
		Deny:  []string{"gpt-5.4-BLOCKED"},
	})

	if !compiled.Allows("gpt-5.3") {
		t.Fatal("expected lower-case model to match mixed-case allow glob")
	}
	if !compiled.Allows("GPT-5.4") {
		t.Fatal("expected upper-case model to match allow glob")
	}
	if compiled.Allows("GPT-5.4-blocked") {
		t.Fatal("deny exact should take precedence case-insensitively")
	}
}

func TestCompiledWithoutRulesDoesNotRestrict(t *testing.T) {
	compiled := Compile(Policy{})
	if compiled.Restricts() {
		t.Fatal("empty policy should not restrict")
	}
	if !compiled.Allows("anything") {
		t.Fatal("empty policy should allow non-empty model")
	}

	exact, patterns := compilePatterns([]string{" ", "GPT-*"})
	if len(exact) != 0 || len(patterns) != 1 || patterns[0] != "gpt-*" {
		t.Fatalf("compilePatterns with blanks = exact %#v patterns %#v", exact, patterns)
	}
}

func TestClonePreservesOriginalCasing(t *testing.T) {
	policy := Policy{
		Allow: []string{"GPT-5*"},
		Deny:  []string{"O3"},
	}
	cloned := Clone(policy)

	if cloned.Allow[0] != "GPT-5*" || cloned.Deny[0] != "O3" {
		t.Fatalf("Clone changed casing: %+v", cloned)
	}
	cloned.Allow[0] = "mutated"
	if policy.Allow[0] != "GPT-5*" {
		t.Fatalf("Clone shares allow slice with source: %+v", policy)
	}
	if got := cloneStrings(nil); got != nil {
		t.Fatalf("cloneStrings(nil) = %#v", got)
	}
}

func TestCloneMapTrimsKeysAndDeepCopies(t *testing.T) {
	source := map[string]Policy{
		" chat ": {Allow: []string{"GPT-5"}, Deny: []string{"O3"}},
		" ":      {Allow: []string{"ignored"}},
	}

	cloned := CloneMap(source)

	want := map[string]Policy{"chat": {Allow: []string{"GPT-5"}, Deny: []string{"O3"}}}
	if !reflect.DeepEqual(cloned, want) {
		t.Fatalf("CloneMap = %#v, want %#v", cloned, want)
	}
	cloned["chat"].Allow[0] = "mutated"
	if source[" chat "].Allow[0] != "GPT-5" {
		t.Fatalf("CloneMap shares nested slice with source: %#v", source)
	}
	if got := CloneMap(nil); got != nil {
		t.Fatalf("CloneMap(nil) = %#v", got)
	}
}

func TestNormalizeTrimsAndDropsEmptyPatterns(t *testing.T) {
	normalized := Normalize(Policy{
		Allow: []string{" GPT-5* ", "", " \t "},
		Deny:  []string{" O3 "},
	})

	if len(normalized.Allow) != 1 || normalized.Allow[0] != "GPT-5*" {
		t.Fatalf("Allow = %#v, want trimmed single pattern", normalized.Allow)
	}
	if len(normalized.Deny) != 1 || normalized.Deny[0] != "O3" {
		t.Fatalf("Deny = %#v, want trimmed single pattern", normalized.Deny)
	}
	if got := Normalize(Policy{Allow: []string{"", " "}}).Allow; got != nil {
		t.Fatalf("Normalize empty allow = %#v", got)
	}
	if got := normalizePatterns(nil); got != nil {
		t.Fatalf("normalizePatterns(nil) = %#v", got)
	}
}

func TestNormalizeMapTrimsKeys(t *testing.T) {
	normalized := NormalizeMap(map[string]Policy{
		" chat ": {Allow: []string{" GPT-5 "}},
		" ":      {Allow: []string{"ignored"}},
	})

	want := map[string]Policy{"chat": {Allow: []string{"GPT-5"}}}
	if !reflect.DeepEqual(normalized, want) {
		t.Fatalf("NormalizeMap = %#v, want %#v", normalized, want)
	}
	if got := NormalizeMap(nil); got != nil {
		t.Fatalf("NormalizeMap(nil) = %#v", got)
	}
}

func TestValidateAcceptsValidPolicies(t *testing.T) {
	if err := Validate(Policy{Allow: []string{"gpt-*"}, Deny: []string{"o3"}}); err != nil {
		t.Fatalf("Validate valid policy returned error: %v", err)
	}
	if err := validatePatternList("allow", []string{" ", "gpt-*"}); err != nil {
		t.Fatalf("validatePatternList with blank returned error: %v", err)
	}
}

func TestValidateRejectsInvalidGlob(t *testing.T) {
	err := Validate(Policy{Allow: []string{"gpt-["}})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate error = %v, want ErrInvalidPolicy", err)
	}
}

func TestValidateMapWrapsInvalidPolicyWithAccountType(t *testing.T) {
	if err := ValidateMap(map[string]Policy{
		" ":      {Allow: []string{"["}},
		" chat ": {Allow: []string{"["}},
	}); !errors.Is(err, ErrInvalidPolicy) || !strings.Contains(err.Error(), `account type "chat"`) {
		t.Fatalf("ValidateMap error = %v", err)
	}

	if err := ValidateMap(map[string]Policy{"chat": {Allow: []string{"gpt-*"}}}); err != nil {
		t.Fatalf("ValidateMap valid returned error: %v", err)
	}
	if err := ValidateMap(nil); err != nil {
		t.Fatalf("ValidateMap nil returned error: %v", err)
	}
}

func TestValidateRejectsTooManyPatterns(t *testing.T) {
	values := make([]string, MaxPatternsPerPolicy+1)
	for i := range values {
		values[i] = "model"
	}
	err := Validate(Policy{Allow: values})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate error = %v, want ErrInvalidPolicy", err)
	}
}

func TestValidateRejectsTooLongPattern(t *testing.T) {
	err := Validate(Policy{Deny: []string{strings.Repeat("x", MaxPatternLength+1)}})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate error = %v, want ErrInvalidPolicy", err)
	}
}
