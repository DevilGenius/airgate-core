package plugin

import (
	"net/http"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/ent/usagelog"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routing"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestUsageRecordsPreserveOriginalReasoningEffort(t *testing.T) {
	ctx := t.Context()
	db := testdb.OpenMemoryEnt(t, "usage_original_reasoning_effort", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	user, err := db.User.Create().SetEmail("original-effort@example.com").SetPasswordHash("hash").SetBalance(100).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	group, err := db.Group.Create().SetName("original-effort").SetPlatform("openai").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.Account.Create().SetName("original-effort").SetPlatform("openai").SetType("apikey").SetRateMultiplier(1).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	recorder := billing.NewRecorder(db, 1)
	// Keep the unstarted queue full so normal forwarding persists synchronously.
	if err := recorder.Record(billing.UsageRecord{BillingEventID: "queued-effort-fixture", UserID: user.ID, AccountID: account.ID, GroupID: group.ID, Platform: "openai", Model: "fixture"}); err != nil {
		t.Fatal(err)
	}
	forwarder := &Forwarder{scheduler: scheduler.NewScheduler(db, nil), calculator: billing.NewCalculator(), recorder: recorder}
	host := &HostService{scheduler: forwarder.scheduler, calculator: forwarder.calculator, recorder: recorder}
	cases := []struct {
		name, path, body, want string
	}{
		{"responses max", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"max"}}`, "max"},
		{"responses ultra", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"ultra"}}`, "ultra"},
		{"chat ultra", "/v1/chat/completions", `{"model":"gpt-6-astra","reasoning_effort":"ultra","messages":[]}`, "ultra"},
		{"anthropic maximum", "/v1/messages", `{"model":"claude-opus-4-6","output_config":{"effort":"maximum"}}`, "max"},
		{"normalized alias", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"x-high"}}`, "xhigh"},
		{"case and whitespace", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":" ULTRA "}}`, "ultra"},
		{"unknown value", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"not-a-valid-effort"}}`, ""},
		{"minimal", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"minimal"}}`, "minimal"},
		{"none", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"none"}}`, "none"},
		{"missing", "/v1/responses", `{"model":"gpt-6-astra","input":"hello"}`, ""},
		{"empty", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":""}}`, ""},
		{"thinking only", "/v1/messages", `{"model":"claude-opus-4-6","thinking":{"type":"enabled","budget_tokens":32768}}`, ""},
		{"nested wins", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"ultra"},"reasoning_effort":"low","output_config":{"effort":"high"}}`, "ultra"},
		{"blank nested falls back", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"  "},"reasoning_effort":"max"}`, "max"},
		{"summary preserves flat effort", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"summary":"auto"},"reasoning_effort":"high"}`, "high"},
		{"output fallback", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{},"reasoning_effort":" ","output_config":{"effort":"normal"}}`, "medium"},
		{"unknown nested does not select lower priority", "/v1/responses", `{"model":"gpt-6-astra","reasoning":{"effort":"future-effort"},"reasoning_effort":"high"}`, ""},
	}
	for _, transport := range []string{"oauth", "basispoints"} {
		for _, entry := range []string{"forwarder", "host"} {
			for _, tc := range cases {
				t.Run(transport+"/"+entry+"/"+tc.name, func(t *testing.T) {
					before, err := db.UsageLog.Query().Count(ctx)
					if err != nil {
						t.Fatal(err)
					}
					metadata := map[string]string{"service_tier": "priority"}
					if transport == "basispoints" {
						metadata["openai.oauth_transport"] = transport
					}
					usage := &sdk.Usage{Model: "gpt-6-astra", InputTokens: 10, OutputTokens: 3, InputCost: 0.25, OutputCost: 0.75, ReasoningEffort: "xhigh", Metadata: metadata}
					outcome := sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess, Usage: usage}
					if entry == "forwarder" {
						parsed := parseBody([]byte(tc.body), "application/json")
						state := &forwardState{
							requestPath: tc.path, model: parsed.Model, reasoningEffort: parsed.ReasoningEffort,
							// A later conversion must not change the captured request value.
							body:   []byte(`{"reasoning":{"effort":"xhigh"}}`),
							plugin: &PluginInstance{Name: "openai", Platform: "openai"}, account: account,
							keyInfo: &auth.APIKeyInfo{UserID: user.ID, UserEmail: user.Email, GroupID: group.ID, GroupPlatform: "openai", GroupRateMultiplier: 1, SellRate: 1},
						}
						c, _ := pluginTestContext(http.MethodPost, tc.path)
						forwarder.recordUsage(c, state, forwardExecution{outcome: outcome, duration: time.Millisecond})
					} else {
						req := hostForwardRequest{UserID: int64(user.ID), Path: tc.path, Body: []byte(tc.body), Headers: map[string]interface{}{"Content-Type": "application/json"}}
						if _, err := host.recordHostForwardUsage(ctx, req, routing.Candidate{GroupID: group.ID, EffectiveRate: 1}, account.ID, "openai", "gpt-6-astra", account, user.Email, outcome, time.Millisecond); err != nil {
							t.Fatal(err)
						}
					}
					after, err := db.UsageLog.Query().Count(ctx)
					if err != nil || after != before+1 {
						t.Fatalf("usage row not persisted: before=%d after=%d err=%v", before, after, err)
					}
					row, err := db.UsageLog.Query().Order(ent.Desc(usagelog.FieldID)).First(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if row.ReasoningEffort != tc.want {
						t.Fatalf("stored effort=%q, want original %q (plugin reported %q)", row.ReasoningEffort, tc.want, usage.ReasoningEffort)
					}
					if row.InputTokens != 10 || row.OutputTokens != 3 || row.ServiceTier != "priority" || row.TotalCost != 1 || usage.ReasoningEffort != "xhigh" {
						t.Fatalf("effort recording changed usage: input=%d output=%d tier=%q cost=%v reported_effort=%q", row.InputTokens, row.OutputTokens, row.ServiceTier, row.TotalCost, usage.ReasoningEffort)
					}
				})
			}
		}
	}

}

func TestNormalizeRequestedReasoningEffort(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{" MAX ", "max"}, {"\tULTRA\n", "ultra"}, {"maximum", "max"},
		{"x-high", "xhigh"}, {"extra_high", "xhigh"}, {"very high", "xhigh"},
		{"min", "minimal"}, {"off", "none"}, {"disabled", "none"},
		{"low", "low"}, {"medium", "medium"}, {"high", "high"},
		{"unexpected-value", ""}, {"max<script>", ""}, {"", ""}, {"   ", ""},
	} {
		if got := normalizeRequestedReasoningEffort(tc.input); got != tc.want {
			t.Errorf("normalizeRequestedReasoningEffort(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestContinuationRecoveryPreservesOriginalReasoningEffort(t *testing.T) {
	for _, original := range []string{"max", "ultra", ""} {
		t.Run("original="+original, func(t *testing.T) {
			state := &forwardState{
				body:               []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_old","reasoning":{"effort":"xhigh"},"input":"hello"}`),
				previousResponseID: "resp_old", requireContinuationAffinity: true, reasoningEffort: original,
			}
			recovered, err := recoverContinuationAffinityMissing(state)
			if err != nil || !recovered {
				t.Fatalf("recovery failed: recovered=%t err=%v", recovered, err)
			}
			if state.reasoningEffort != original {
				t.Fatalf("recovery replaced original effort %q with %q", original, state.reasoningEffort)
			}
		})
	}
}
