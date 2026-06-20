package account

import (
	"context"
	"testing"
)

func TestGetSingleAccountUsageSkipsAPIKeyProbe(t *testing.T) {
	service := NewService(stubRepository{
		findByID: func(_ context.Context, id int, opts LoadOptions) (Account, error) {
			if id != 42 {
				t.Fatalf("FindByID id = %d, want 42", id)
			}
			if opts.WithGroups || opts.WithProxy {
				t.Fatalf("FindByID opts = %+v, want empty", opts)
			}
			return Account{ID: id, Platform: "custom", Type: "apikey", State: "active"}, nil
		},
	}, nil, nil, nil)

	got, err := service.GetSingleAccountUsage(t.Context(), 42)
	if err != nil {
		t.Fatalf("GetSingleAccountUsage() error = %v", err)
	}
	if _, ok := got["today_stats"]; !ok || len(got) != 1 {
		t.Fatalf("GetSingleAccountUsage() = %#v, want only zero today_stats", got)
	}
}

func TestFetchSingleAccountUsageDedupReturnsFalseWithoutPluginCatalog(t *testing.T) {
	service := NewService(stubRepository{}, nil, nil, nil)
	info, usageErrors, ok := service.fetchSingleAccountUsageDedup(t.Context(), Account{ID: 7, Platform: "custom"})
	if ok || len(usageErrors) != 0 || len(info.Windows) != 0 || info.Credits != nil {
		t.Fatalf("dedup result info=%+v errors=%+v ok=%v", info, usageErrors, ok)
	}
}
