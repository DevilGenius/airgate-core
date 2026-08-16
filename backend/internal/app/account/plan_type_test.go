package account

import "testing"

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
