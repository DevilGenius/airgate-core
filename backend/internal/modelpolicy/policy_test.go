package modelpolicy

import "testing"

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
