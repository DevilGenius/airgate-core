package billing

import (
	"testing"

	"github.com/DouDOU-start/airgate-core/internal/auth"
)

func TestResolveBillingRate_PriorityChain(t *testing.T) {
	tests := []struct {
		name string
		info *auth.APIKeyInfo
		want float64
	}{
		{
			name: "nil keyInfo defaults to 1.0",
			info: nil,
			want: 1.0,
		},
		{
			name: "user.group_rates wins over group.rate_multiplier",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.5,
				UserGroupRates:      map[int64]float64{5: 0.2},
			},
			want: 0.2,
		},
		{
			name: "user.group_rates miss falls back to group.rate_multiplier",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.5,
				UserGroupRates:      map[int64]float64{6: 0.2}, // 不同 group
			},
			want: 0.5,
		},
		{
			name: "no overrides falls back to group.rate_multiplier",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.7,
			},
			want: 0.7,
		},
		{
			name: "everything zero defaults to 1.0",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0,
			},
			want: 1.0,
		},
		{
			name: "sell_rate is NOT in priority chain — should be ignored",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.3,
				SellRate:            0.99, // 不应影响 billing rate
			},
			want: 0.3,
		},
		{
			name: "user.group_rates with non-positive value falls through",
			info: &auth.APIKeyInfo{
				GroupID:             5,
				GroupRateMultiplier: 0.4,
				UserGroupRates:      map[int64]float64{5: 0}, // 显式 0 视为未设置
			},
			want: 0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBillingRate(tt.info)
			if got != tt.want {
				t.Errorf("ResolveBillingRate() = %v, want %v", got, tt.want)
			}
		})
	}
}
