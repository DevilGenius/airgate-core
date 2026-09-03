package account

import (
	"testing"
	"time"
)

func TestNormalizePlanType(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "plus alias", raw: "ChatGPT Plus", want: "plus"},
		{name: "pro alias", raw: "Builder Id Pro", want: "pro"},
		{name: "professional alias", raw: "Professional", want: "pro"},
		{name: "team", raw: "Team", want: "team"},
		{name: "k12 stays distinct", raw: "K12", want: "k12"},
		{name: "self serve business prolite stays distinct", raw: "Self_serve_business_prolite", want: "prolite"},
		{name: "prolite alias", raw: "ChatGPT ProLite", want: "prolite"},
		{name: "free", raw: "free", want: "free"},
		{name: "enterprise", raw: "Enterprise", want: "enterprise"},
		{name: "unknown", raw: "Custom Plan", want: "custom plan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizePlanType(tt.raw); got != tt.want {
				t.Fatalf("NormalizePlanType(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestEffectivePlanType(t *testing.T) {
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	if got := EffectivePlanType("plus", "2026-09-03T00:00:00Z", now); got != "free" {
		t.Fatalf("expired Plus effective plan = %q, want free", got)
	}
	if got := EffectivePlanType("Self_serve_business_prolite", "2026-09-03T00:00:00Z", now); got != "prolite" {
		t.Fatalf("expired ProLite effective plan = %q, want prolite", got)
	}
	for _, plan := range []string{"team", "k12"} {
		if got := EffectivePlanType(plan, "2026-09-03T00:00:00Z", now); got != plan {
			t.Fatalf("%s effective plan = %q, want unchanged", plan, got)
		}
	}
	if got := EffectivePlanType("pro", "2026-09-03T00:00:00Z", now); got != "free" {
		t.Fatalf("expired Pro effective plan = %q, want free", got)
	}
	if got := EffectivePlanType("team", "not-a-time", now); got != "team" {
		t.Fatalf("Team with invalid expiry effective plan = %q, want team", got)
	}
}
