package ratevalue

import (
	"errors"
	"math"
)

const (
	MinPositiveMultiplier = 0.01
	MaxMultiplier         = 100
)

var (
	ErrInvalidMultiplier     = errors.New("倍率必须是有限正数，范围为 0.01 到 100")
	ErrInvalidSellMultiplier = errors.New("销售倍率必须是有限非负数；0 表示免费，正数范围为 0.01 到 100")
)

func ValidateMultiplier(value float64) error {
	if !IsValidMultiplier(value) {
		return ErrInvalidMultiplier
	}
	return nil
}

func IsValidMultiplier(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return false
	}
	return value >= MinPositiveMultiplier && value <= MaxMultiplier
}

func NormalizeMultiplier(value, fallback float64) float64 {
	if IsValidMultiplier(value) {
		return value
	}
	return fallback
}

func ValidateSellMultiplier(value float64) error {
	if !IsValidSellMultiplier(value) {
		return ErrInvalidSellMultiplier
	}
	return nil
}

func IsValidSellMultiplier(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return false
	}
	return value == 0 || (value >= MinPositiveMultiplier && value <= MaxMultiplier)
}

func NormalizeSellMultiplier(value, fallback float64) float64 {
	if IsValidSellMultiplier(value) {
		return value
	}
	return fallback
}

func SafeAddNonNegative(values ...float64) float64 {
	sum := 0.0
	for _, value := range values {
		value = NormalizeNonNegative(value)
		if math.MaxFloat64-sum < value {
			return math.MaxFloat64
		}
		sum += value
	}
	return sum
}

func SafeMulNonNegative(left, right float64) float64 {
	left = NormalizeNonNegative(left)
	right = NormalizeNonNegative(right)
	if left == 0 || right == 0 {
		return 0
	}
	if left > math.MaxFloat64/right {
		return math.MaxFloat64
	}
	return left * right
}

func NormalizeNonNegative(value float64) float64 {
	switch {
	case math.IsNaN(value), math.IsInf(value, -1), value < 0:
		return 0
	case math.IsInf(value, 1):
		return math.MaxFloat64
	default:
		return value
	}
}
