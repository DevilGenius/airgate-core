package billing

import (
	"math"
	"testing"
)

const epsilon = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestCalculate_NoMarkup(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:       0.6,
		OutputCost:      0.3,
		CachedInputCost: 0.1,
		BillingRate:     0.3,
		SellRate:        1,
		AccountRate:     1.0,
	})

	if !almostEqual(res.TotalCost, 1.0) {
		t.Fatalf("TotalCost = %v, want 1.0", res.TotalCost)
	}
	if !almostEqual(res.ActualCost, 0.3) {
		t.Fatalf("ActualCost = %v, want 0.3", res.ActualCost)
	}
	if !almostEqual(res.BilledCost, res.ActualCost) {
		t.Fatalf("BilledCost = %v, want %v (= ActualCost)", res.BilledCost, res.ActualCost)
	}
	// account_rate=1 时 account_cost == total_cost
	if !almostEqual(res.AccountCost, res.TotalCost) {
		t.Fatalf("AccountCost = %v, want %v (= TotalCost)", res.AccountCost, res.TotalCost)
	}
	if !almostEqual(res.RateMultiplier, 0.3) {
		t.Fatalf("RateMultiplier = %v, want 0.3", res.RateMultiplier)
	}
	if res.SellRate != 1 {
		t.Fatalf("SellRate = %v, want 1", res.SellRate)
	}
}

func TestCalculate_ZeroSellRateIsFreeForCustomer(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:   1.0,
		BillingRate: 0.5,
		SellRate:    0,
		AccountRate: 1.0,
	})
	if !almostEqual(res.ActualCost, 0.5) {
		t.Fatalf("ActualCost = %v, want 0.5", res.ActualCost)
	}
	if !almostEqual(res.BilledCost, 0) {
		t.Fatalf("BilledCost = %v, want 0", res.BilledCost)
	}
	if res.SellRate != 0 {
		t.Fatalf("SellRate = %v, want 0", res.SellRate)
	}
}

func TestCalculate_AccountCostIndependent(t *testing.T) {
	// account_rate 既不影响 actual_cost 也不影响 billed_cost
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:   1.0,
		OutputCost:  1.0,
		BillingRate: 0.3,
		SellRate:    0.6,
		AccountRate: 1.5,
	})

	if !almostEqual(res.ActualCost, 0.6) {
		t.Fatalf("ActualCost = %v, want 0.6 (total × billing_rate)", res.ActualCost)
	}
	if !almostEqual(res.BilledCost, 0.36) {
		t.Fatalf("BilledCost = %v, want 0.36 (actual × sell_rate)", res.BilledCost)
	}
	if !almostEqual(res.AccountCost, 3.0) {
		t.Fatalf("AccountCost = %v, want 3.0 (total × account_rate)", res.AccountCost)
	}
	// 三个数字两两独立
	if res.AccountCost == res.ActualCost || res.AccountCost == res.BilledCost {
		t.Fatalf("AccountCost should be independent of actual/billed")
	}
}

func TestCalculate_ZeroAccountRate_DefaultsToOne(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:   2.0,
		BillingRate: 1.0,
		SellRate:    1.0,
		AccountRate: 0,
	})
	if !almostEqual(res.AccountCost, 2.0) {
		t.Fatalf("AccountCost = %v, want 2.0 (account_rate=0 falls back to 1)", res.AccountCost)
	}
	if !almostEqual(res.AccountRateMultiplier, 1) {
		t.Fatalf("AccountRateMultiplier = %v, want 1", res.AccountRateMultiplier)
	}
}

func TestCalculate_WithMarkup(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:       0.6,
		OutputCost:      0.3,
		CachedInputCost: 0.1,
		BillingRate:     1.0,
		SellRate:        1.2, // reseller 卖给客户的倍率
	})

	// 平台真实成本：base × billing_rate
	if !almostEqual(res.ActualCost, 1.0) {
		t.Fatalf("ActualCost = %v, want 1.0", res.ActualCost)
	}
	// 客户账面消耗：actual × sell_rate
	if !almostEqual(res.BilledCost, 1.2) {
		t.Fatalf("BilledCost = %v, want 1.2", res.BilledCost)
	}
	// 利润 = billed - actual = $0.20
	profit := res.BilledCost - res.ActualCost
	if !almostEqual(profit, 0.2) {
		t.Fatalf("profit = %v, want 0.2", profit)
	}
}

func TestCalculate_ZeroBillingRate_DefaultsToOne(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:   1.0,
		BillingRate: 0,
		SellRate:    1,
	})
	if !almostEqual(res.ActualCost, 1) {
		t.Fatalf("ActualCost = %v, want 1", res.ActualCost)
	}
	if !almostEqual(res.RateMultiplier, 1) {
		t.Fatalf("RateMultiplier = %v, want 1", res.RateMultiplier)
	}
}

func TestCalculate_InvalidBillingRate_DefaultsToOne(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:   1.0,
		BillingRate: -1,
	})
	if !almostEqual(res.ActualCost, 1.0) {
		t.Fatalf("ActualCost = %v, want 1.0", res.ActualCost)
	}
	if !almostEqual(res.RateMultiplier, 1.0) {
		t.Fatalf("RateMultiplier = %v, want 1.0", res.RateMultiplier)
	}
}

func TestCalculate_OverflowClampsCosts(t *testing.T) {
	c := NewCalculator()
	res := c.Calculate(CalculateInput{
		InputCost:   math.MaxFloat64,
		OutputCost:  1,
		BillingRate: math.MaxFloat64,
		SellRate:    math.MaxFloat64,
		AccountRate: math.MaxFloat64,
	})
	if math.IsInf(res.TotalCost, 0) || math.IsInf(res.ActualCost, 0) || math.IsInf(res.BilledCost, 0) || math.IsInf(res.AccountCost, 0) {
		t.Fatalf("costs must stay finite: %+v", res)
	}
}

func TestCalculate_MarkupStacksOnBillingRate(t *testing.T) {
	// 关键不变量：sell_rate 不影响 actual_cost，但 billed_cost 必须叠加 actual_cost。
	c := NewCalculator()

	base := CalculateInput{
		InputCost:   1.0,
		OutputCost:  1.0,
		BillingRate: 0.3,
		SellRate:    1.2,
	}
	res1 := c.Calculate(base)

	// 改变 billing_rate
	base2 := base
	base2.BillingRate = 0.5
	res2 := c.Calculate(base2)

	if !almostEqual(res1.BilledCost, 0.72) {
		t.Fatalf("BilledCost = %v, want 0.72", res1.BilledCost)
	}
	if !almostEqual(res2.BilledCost, 1.2) {
		t.Fatalf("BilledCost = %v, want 1.2", res2.BilledCost)
	}
	if almostEqual(res1.ActualCost, res2.ActualCost) {
		t.Fatalf("ActualCost should depend on BillingRate but didn't change")
	}

	// 改变 sell_rate
	base3 := base
	base3.SellRate = 1.5
	res3 := c.Calculate(base3)

	if !almostEqual(res1.ActualCost, res3.ActualCost) {
		t.Fatalf("ActualCost should not depend on SellRate: %v vs %v", res1.ActualCost, res3.ActualCost)
	}
	if almostEqual(res1.BilledCost, res3.BilledCost) {
		t.Fatalf("BilledCost should depend on SellRate but didn't change")
	}
}

func TestCalculate_BillingCostOverride(t *testing.T) {
	c := NewCalculator()
	override := 0.08
	res := c.Calculate(CalculateInput{
		InputCost:           0.10,
		OutputCost:          0.40,
		BillingRate:         0.50,
		SellRate:            0.90,
		BillingCostOverride: &override,
		AccountRate:         1.25,
	})

	if !almostEqual(res.TotalCost, 0.50) {
		t.Fatalf("TotalCost = %v, want 0.50", res.TotalCost)
	}
	if !almostEqual(res.ActualCost, 0.08) {
		t.Fatalf("ActualCost = %v, want 0.08", res.ActualCost)
	}
	if !almostEqual(res.BilledCost, 0.072) {
		t.Fatalf("BilledCost = %v, want 0.072", res.BilledCost)
	}
	if !almostEqual(res.AccountCost, 0.625) {
		t.Fatalf("AccountCost = %v, want 0.625", res.AccountCost)
	}
	if !almostEqual(res.RateMultiplier, 0.50) {
		t.Fatalf("RateMultiplier = %v, want original billing rate 0.50", res.RateMultiplier)
	}
}
