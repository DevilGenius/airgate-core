package modelpolicy

import (
	"errors"
	"strings"
	"testing"
)

func TestCompiledAllowsCaseInsensitiveExact(t *testing.T) {
	compiled := Compile(Policy{
		Allow: []string{"GPT-5.4"},
	})

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

func TestClonePreservesOriginalCasing(t *testing.T) {
	policy := Policy{
		Allow: []string{"GPT-5*"},
		Deny:  []string{"O3"},
	}
	cloned := Clone(policy)

	if cloned.Allow[0] != "GPT-5*" || cloned.Deny[0] != "O3" {
		t.Fatalf("Clone changed casing: %+v", cloned)
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
}

func TestValidateRejectsInvalidGlob(t *testing.T) {
	err := Validate(Policy{Allow: []string{"gpt-["}})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate error = %v, want ErrInvalidPolicy", err)
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
