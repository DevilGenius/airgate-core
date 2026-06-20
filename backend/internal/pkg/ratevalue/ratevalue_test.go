package ratevalue

import (
	"errors"
	"math"
	"testing"
)

func TestValidateMultiplier(t *testing.T) {
	valid := []float64{MinPositiveMultiplier, 1, MaxMultiplier}
	for _, value := range valid {
		if err := ValidateMultiplier(value); err != nil {
			t.Fatalf("ValidateMultiplier(%v) returned error: %v", value, err)
		}
		if !IsValidMultiplier(value) {
			t.Fatalf("IsValidMultiplier(%v) = false", value)
		}
	}

	invalid := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, 0, MinPositiveMultiplier - 0.001, MaxMultiplier + 0.001}
	for _, value := range invalid {
		if err := ValidateMultiplier(value); !errors.Is(err, ErrInvalidMultiplier) {
			t.Fatalf("ValidateMultiplier(%v) error = %v, want ErrInvalidMultiplier", value, err)
		}
		if IsValidMultiplier(value) {
			t.Fatalf("IsValidMultiplier(%v) = true", value)
		}
	}
}

func TestNormalizeMultiplier(t *testing.T) {
	if got := NormalizeMultiplier(2.5, 1); got != 2.5 {
		t.Fatalf("NormalizeMultiplier valid = %v, want 2.5", got)
	}
	if got := NormalizeMultiplier(0, 1.25); got != 1.25 {
		t.Fatalf("NormalizeMultiplier invalid = %v, want fallback", got)
	}
}

func TestValidateSellMultiplier(t *testing.T) {
	valid := []float64{0, MinPositiveMultiplier, 1, MaxMultiplier}
	for _, value := range valid {
		if err := ValidateSellMultiplier(value); err != nil {
			t.Fatalf("ValidateSellMultiplier(%v) returned error: %v", value, err)
		}
		if !IsValidSellMultiplier(value) {
			t.Fatalf("IsValidSellMultiplier(%v) = false", value)
		}
	}

	invalid := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, MinPositiveMultiplier - 0.001, MaxMultiplier + 0.001}
	for _, value := range invalid {
		if err := ValidateSellMultiplier(value); !errors.Is(err, ErrInvalidSellMultiplier) {
			t.Fatalf("ValidateSellMultiplier(%v) error = %v, want ErrInvalidSellMultiplier", value, err)
		}
		if IsValidSellMultiplier(value) {
			t.Fatalf("IsValidSellMultiplier(%v) = true", value)
		}
	}
}

func TestNormalizeSellMultiplier(t *testing.T) {
	if got := NormalizeSellMultiplier(0, 1); got != 0 {
		t.Fatalf("NormalizeSellMultiplier free = %v, want 0", got)
	}
	if got := NormalizeSellMultiplier(3, 1); got != 3 {
		t.Fatalf("NormalizeSellMultiplier valid = %v, want 3", got)
	}
	if got := NormalizeSellMultiplier(math.NaN(), 1.5); got != 1.5 {
		t.Fatalf("NormalizeSellMultiplier invalid = %v, want fallback", got)
	}
}

func TestSafeAddNonNegative(t *testing.T) {
	if got := SafeAddNonNegative(1, -2, math.NaN(), 3); got != 4 {
		t.Fatalf("SafeAddNonNegative mixed = %v, want 4", got)
	}
	if got := SafeAddNonNegative(math.Inf(1), 1); got != math.MaxFloat64 {
		t.Fatalf("SafeAddNonNegative +Inf = %v, want MaxFloat64", got)
	}
	if got := SafeAddNonNegative(math.MaxFloat64, math.MaxFloat64); got != math.MaxFloat64 {
		t.Fatalf("SafeAddNonNegative overflow = %v, want MaxFloat64", got)
	}
}

func TestSafeMulNonNegative(t *testing.T) {
	if got := SafeMulNonNegative(2, 3); got != 6 {
		t.Fatalf("SafeMulNonNegative normal = %v, want 6", got)
	}
	if got := SafeMulNonNegative(-1, 3); got != 0 {
		t.Fatalf("SafeMulNonNegative normalized zero = %v, want 0", got)
	}
	if got := SafeMulNonNegative(math.MaxFloat64, 2); got != math.MaxFloat64 {
		t.Fatalf("SafeMulNonNegative overflow = %v, want MaxFloat64", got)
	}
}

func TestNormalizeNonNegative(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  float64
	}{
		{name: "nan", value: math.NaN(), want: 0},
		{name: "negative infinity", value: math.Inf(-1), want: 0},
		{name: "negative", value: -0.01, want: 0},
		{name: "positive infinity", value: math.Inf(1), want: math.MaxFloat64},
		{name: "normal", value: 2.25, want: 2.25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeNonNegative(tt.value); got != tt.want {
				t.Fatalf("NormalizeNonNegative(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
