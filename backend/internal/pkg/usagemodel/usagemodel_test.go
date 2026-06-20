package usagemodel

import "testing"

func TestIsImageGen(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "plain prefix", model: "gpt-image-1", want: true},
		{name: "case and spaces", model: "  GPT-IMAGE-mini  ", want: true},
		{name: "exact prefix", model: ImagePrefix, want: true},
		{name: "chat model", model: "gpt-4.1", want: false},
		{name: "empty", model: " ", want: false},
		{name: "prefix not at start", model: "openai-gpt-image", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsImageGen(tt.model); got != tt.want {
				t.Fatalf("IsImageGen(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}
