package usage

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseModelFilter(t *testing.T) {
	tests := []struct {
		raw          string
		wantIncludes []string
		wantExcludes []string
	}{
		{raw: ""},
		{raw: "   "},
		{raw: "!"},
		{raw: " !  "},
		{raw: "gpt-5.4-mini", wantIncludes: []string{"gpt-5.4-mini"}},
		{raw: " gpt-5.4-mini ", wantIncludes: []string{"gpt-5.4-mini"}},
		{raw: "gpt-5.4 gpt-5.5", wantIncludes: []string{"gpt-5.4", "gpt-5.5"}},
		{raw: "gpt-5.4\t!gpt-5.4-mini\n!gpt-5.5-mini", wantIncludes: []string{"gpt-5.4"}, wantExcludes: []string{"gpt-5.4-mini", "gpt-5.5-mini"}},
		{raw: "!gpt-5.4-mini", wantExcludes: []string{"gpt-5.4-mini"}},
		{raw: " ! gpt-5.4-mini ", wantExcludes: []string{"gpt-5.4-mini"}},
		{raw: "gpt!mini", wantIncludes: []string{"gpt!mini"}},
		{raw: "!!foo", wantExcludes: []string{"!foo"}},
		{
			raw:          "gpt-5.4 gpt-5.4 !gpt-5.4-mini !gpt-5.4-mini",
			wantIncludes: []string{"gpt-5.4"},
			wantExcludes: []string{"gpt-5.4-mini"},
		},
		{
			raw:          "gpt-5.4 !gpt-5.4",
			wantIncludes: []string{"gpt-5.4"},
			wantExcludes: []string{"gpt-5.4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			includes, excludes := ParseModelFilter(tt.raw)
			if !slices.Equal(includes, tt.wantIncludes) || !slices.Equal(excludes, tt.wantExcludes) {
				t.Fatalf("ParseModelFilter(%q) = includes %q, excludes %q; want includes %q, excludes %q", tt.raw, includes, excludes, tt.wantIncludes, tt.wantExcludes)
			}
		})
	}
}

func TestValidateModelFilterLimitsRawLengthAndTotalTerms(t *testing.T) {
	if err := ValidateModelFilter(strings.Repeat("a", maxModelFilterLength)); err != nil {
		t.Fatalf("ValidateModelFilter(max length) error = %v", err)
	}
	if err := ValidateModelFilter(strings.Repeat("a", maxModelFilterLength+1)); !errors.Is(err, ErrInvalidModelFilter) {
		t.Fatalf("ValidateModelFilter(over length) error = %v, want ErrInvalidModelFilter", err)
	}

	atLimit := strings.TrimSpace(strings.Repeat("gpt-5.4 ", maxModelFilterTerms))
	if err := ValidateModelFilter(atLimit); err != nil {
		t.Fatalf("ValidateModelFilter(max terms) error = %v", err)
	}
	overLimit := strings.TrimSpace(strings.Repeat("gpt-5.4 ", maxModelFilterTerms+1))
	if err := ValidateModelFilter(overLimit); !errors.Is(err, ErrInvalidModelFilter) {
		t.Fatalf("ValidateModelFilter(over terms) error = %v, want ErrInvalidModelFilter", err)
	}
}
