package handler

func customerAPIKeyRate(groupRate, sellRate float64) float64 {
	if groupRate <= 0 {
		groupRate = 1
	}
	if sellRate <= 0 {
		sellRate = 1
	}
	return groupRate * sellRate
}
