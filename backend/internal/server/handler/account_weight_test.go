package handler

import (
	"encoding/json"
	"testing"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
)

func TestAccountSchedulingWeightExportAndValidation(t *testing.T) {
	account := appaccount.Account{SchedulingWeight: 0}
	if got := toAccountResp(account).SchedulingWeight; got != 0 {
		t.Fatalf("response weight = %d", got)
	}
	data, err := json.Marshal(toAccountExportItem(account))
	if err != nil {
		t.Fatal(err)
	}
	var imported dto.AccountExportItem
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatal(err)
	}
	if imported.SchedulingWeight == nil || *imported.SchedulingWeight != 0 {
		t.Fatalf("imported = %+v", imported)
	}
	h := &AccountHandler{}
	status, _ := h.handleError("invalid weight", "failed", appaccount.ErrInvalidSchedulingWeight)
	if status != 400 {
		t.Fatalf("invalid weight status = %d", status)
	}
}
