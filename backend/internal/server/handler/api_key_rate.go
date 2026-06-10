package handler

import "github.com/DevilGenius/airgate-core/internal/pkg/ratevalue"

func customerAPIKeyRate(groupRate, sellRate float64) float64 {
	groupRate = ratevalue.NormalizeMultiplier(groupRate, 1)
	if sellRate <= 0 {
		sellRate = 1
	}
	return ratevalue.SafeMulNonNegative(groupRate, sellRate)
}
