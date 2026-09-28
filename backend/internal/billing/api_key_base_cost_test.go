package billing

import (
	"context"
	"math"
	"testing"
)

func TestAPIKeyBaseCostOverrideLeavesUserChargeUnchanged(t *testing.T) {
	for _, actualBase := range []float64{10, 2.5} {
		for _, sellRate := range []float64{0, 1, 3} {
			input := CalculateInput{InputCost: actualBase, BillingRate: 0.2, SellRate: sellRate, AccountRate: 0.7}
			before := NewCalculator().Calculate(input)
			standard := 5.0
			input.APIKeyBaseCostOverride = &standard
			after := NewCalculator().Calculate(input)
			if after.ActualCost != before.ActualCost || after.AccountCost != before.AccountCost || after.TotalCost != before.TotalCost || after.InputCost != before.InputCost {
				t.Fatalf("changed actual billing: before=%+v after=%+v", before, after)
			}
			if !almostEqual(after.BilledCost, standard*input.BillingRate*sellRate) {
				t.Fatalf("key charge=%g", after.BilledCost)
			}
		}
	}
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		got := NewCalculator().Calculate(CalculateInput{InputCost: 10, BillingRate: 0.2, SellRate: 3, APIKeyBaseCostOverride: &invalid})
		if got.ActualCost != 2 || got.BilledCost != 6 {
			t.Fatalf("invalid quote changed billing: %+v", got)
		}
	}
}

func TestAPIKeyBaseCostPreservesImagePricingAndAddon(t *testing.T) {
	standard, addon, fixed := 5.0, 0.4, 0.1
	input := CalculateInput{InputCost: 10, BillingRate: 0.2, SellRate: 3, AccountRate: 0.7, APIKeyBaseCostOverride: &standard, BillingCostAddon: &addon}
	got := NewCalculator().Calculate(input)
	if !almostEqual(got.ActualCost, 2.4) || !almostEqual(got.BilledCost, 4.2) {
		t.Fatalf("addon changed: %+v", got)
	}
	input.BillingCostOverride = &fixed
	got = NewCalculator().Calculate(input)
	if !almostEqual(got.ActualCost, 0.5) || !almostEqual(got.BilledCost, 1.5) {
		t.Fatalf("fixed image price changed: %+v", got)
	}
}

func TestAPIKeyStandardChargePersistsSeparatelyFromUserBalance(t *testing.T) {
	db := openBillingRecorderDB(t, "key-standard-charge")
	defer closeBillingDB(t, db)
	ctx := context.Background()
	user, group, account, key := createBillingFixture(t, ctx, db, "key-standard-charge")
	if err := db.User.UpdateOneID(user.ID).SetBalance(100).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	standard := 5.0
	calc := NewCalculator().Calculate(CalculateInput{InputCost: 10, BillingRate: 0.2, SellRate: 3, AccountRate: 0.7, APIKeyBaseCostOverride: &standard})
	record := billingRecordForFixture("bill_key_standard", user, group, account, key)
	record.InputCost, record.TotalCost, record.ActualCost, record.BilledCost, record.AccountCost = calc.InputCost, calc.TotalCost, calc.ActualCost, calc.BilledCost, calc.AccountCost
	record.ServiceTier = "priority"
	keyBefore, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(db, 1)
	id, err := recorder.RecordSync(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.User.Get(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	k, err := db.APIKey.Get(ctx, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	log, err := db.UsageLog.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if u.Balance != 98 || !almostEqual(k.UsedQuota-keyBefore.UsedQuota, 3) || !almostEqual(k.UsedQuotaActual-keyBefore.UsedQuotaActual, 2) {
		t.Fatalf("user=%g key=%g actual=%g", u.Balance, k.UsedQuota, k.UsedQuotaActual)
	}
	if log.ServiceTier != "priority" || log.ActualCost != 2 || log.BilledCost != 3 {
		t.Fatalf("usage changed tier or charges: %+v", log)
	}
}
