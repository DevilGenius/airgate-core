package plantype

import "testing"

func TestNormalizeProLiteAliases(t *testing.T) {
	for _, raw := range []string{
		"prolite",
		"ProLite",
		"pro_lite",
		"pro lite",
		"ChatGPT ProLite",
		"Self_serve_business_prolite",
		"SELF_SERVE_BUSINESS_PRO_LITE",
	} {
		if got := Normalize(raw); got != ProLite {
			t.Fatalf("Normalize(%q) = %q, want %q", raw, got, ProLite)
		}
	}
}

func TestPlanClassifications(t *testing.T) {
	for _, raw := range []string{"team", "K12", "Self_serve_business_pro_lite"} {
		if got := RoutingCategory(raw); got != Team {
			t.Fatalf("RoutingCategory(%q) = %q, want team", raw, got)
		}
	}
	for _, test := range []struct {
		plan string
		pool string
	}{
		{plan: Plus, pool: EstimatePoolPlus},
		{plan: Team, pool: EstimatePoolPro},
		{plan: K12, pool: EstimatePoolPro},
		{plan: Pro, pool: EstimatePoolPro},
		{plan: ProLite, pool: EstimatePoolPro},
	} {
		if got := EstimatePool(test.plan); got != test.pool {
			t.Fatalf("EstimatePool(%q) = %q, want %q", test.plan, got, test.pool)
		}
	}
}
