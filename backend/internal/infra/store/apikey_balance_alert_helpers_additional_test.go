package store

import (
	"testing"

	"github.com/DevilGenius/airgate-core/ent"
	appapikey "github.com/DevilGenius/airgate-core/internal/app/apikey"
)

func TestAPIKeyBalanceAlertResetHelpers(t *testing.T) {
	current := &ent.APIKey{
		QuotaUsd:              10,
		BalanceAlertEnabled:   true,
		BalanceAlertEmail:     " alert@example.com ",
		BalanceAlertThreshold: 2,
	}

	higherQuota := 11.0
	if !shouldResetBalanceAlertNotified(current, appapikey.Mutation{QuotaUSD: &higherQuota}) {
		t.Fatal("higher quota should reset balance alert notification")
	}

	lowerQuota := 9.0
	if shouldResetBalanceAlertNotified(current, appapikey.Mutation{QuotaUSD: &lowerQuota}) {
		t.Fatal("lower quota alone should not reset balance alert notification")
	}

	disabled := false
	if shouldResetBalanceAlertNotified(current, appapikey.Mutation{BalanceAlertEnabled: &disabled}) {
		t.Fatal("disabled alert should not reset notification")
	}

	blankEmail := " "
	if shouldResetBalanceAlertNotified(current, appapikey.Mutation{BalanceAlertEmail: &blankEmail}) {
		t.Fatal("blank alert email should not reset notification")
	}

	zeroThreshold := 0.0
	if shouldResetBalanceAlertNotified(current, appapikey.Mutation{BalanceAlertThreshold: &zeroThreshold}) {
		t.Fatal("zero threshold should not reset notification")
	}

	changedEmail := "new-alert@example.com"
	if !shouldResetBalanceAlertNotified(current, appapikey.Mutation{BalanceAlertEmail: &changedEmail}) {
		t.Fatal("changed email should reset notification")
	}

	changedThreshold := 3.0
	if !shouldResetBalanceAlertNotified(current, appapikey.Mutation{BalanceAlertThreshold: &changedThreshold}) {
		t.Fatal("changed threshold should reset notification")
	}

	enabledCurrent := *current
	enabledCurrent.BalanceAlertEnabled = false
	enabled := true
	if !shouldResetBalanceAlertNotified(&enabledCurrent, appapikey.Mutation{BalanceAlertEnabled: &enabled}) {
		t.Fatal("enabling an otherwise configured alert should reset notification")
	}

	sameEmail := "alert@example.com"
	sameThreshold := 2.0
	if balanceAlertConfigChanged(current, appapikey.Mutation{BalanceAlertEmail: &sameEmail, BalanceAlertThreshold: &sameThreshold}) {
		t.Fatal("trimmed same email and same threshold should not count as config change")
	}
}
