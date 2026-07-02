package plugin

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	entapikey "github.com/DevilGenius/airgate-core/ent/apikey"
	entuser "github.com/DevilGenius/airgate-core/ent/user"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestHostServiceSchedulerProbeAndForwardRuntime(t *testing.T) {
	ctx := context.Background()
	restoreRouteGraph := routegraph.SetSnapshotForTesting(nil)
	t.Cleanup(restoreRouteGraph)

	db := testdb.OpenMemoryEnt(t, "host_forward_runtime", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	user := db.User.Create().
		SetEmail("forward@example.com").
		SetUsername("forwarder").
		SetPasswordHash("hash").
		SetBalance(100).
		SaveX(ctx)
	group := db.Group.Create().
		SetName("Forward Group").
		SetPlatform("openai").
		SetRateMultiplier(1.25).
		SetPluginSettings(map[string]map[string]string{"openai": {"image_enabled": "true"}}).
		SaveX(ctx)
	account := db.Account.Create().
		SetName("Forward Account").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-test"}).
		SetRateMultiplier(1.1).
		SetMaxConcurrency(2).
		AddGroupIDs(group.ID).
		SaveX(ctx)
	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatalf("refresh routegraph: %v", err)
	}

	forwardCalls := 0
	client, cleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "gateway-openai",
		platform: "openai",
		forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			forwardCalls++
			if req.Account == nil || req.Account.ID != int64(account.ID) {
				t.Fatalf("gateway account = %+v", req.Account)
			}
			if req.Model != "gpt-4.1" {
				t.Fatalf("gateway model = %q", req.Model)
			}
			if forwardCalls == 3 {
				return sdk.ForwardOutcome{
					Kind:   sdk.OutcomeClientError,
					Reason: "bad request",
					Upstream: sdk.UpstreamResponse{
						StatusCode: http.StatusBadRequest,
						Headers:    http.Header{"Content-Type": {"application/json"}},
						Body:       []byte(`{"error":"bad"}`),
					},
				}, nil
			}
			return sdk.ForwardOutcome{
				Kind: sdk.OutcomeSuccess,
				Upstream: sdk.UpstreamResponse{
					StatusCode: http.StatusAccepted,
					Headers:    http.Header{"X-Gateway": {"runtime"}},
					Body:       []byte(`{"ok":true}`),
				},
				Usage: &sdk.Usage{
					Model:        "gpt-4.1",
					InputTokens:  3,
					OutputTokens: 5,
					InputCost:    0.01,
					OutputCost:   0.02,
				},
			}, nil
		},
	})
	defer cleanup()

	manager := NewManager(t.TempDir(), "debug", "", nil)
	manager.instances["gateway-openai"] = &PluginInstance{
		Name:     "gateway-openai",
		Type:     "gateway",
		Platform: "openai",
		Gateway:  client,
	}
	manager.modelCache = map[string][]sdk.ModelInfo{"openai": {{ID: "gpt-4.1", Name: "GPT-4.1"}}}

	host := NewHostService(
		db,
		manager,
		scheduler.NewScheduler(db, nil),
		scheduler.NewConcurrencyManager(nil),
		billing.NewCalculator(),
		nil,
	)
	handle := host.NewPluginHandle("studio")
	handle.SetCapabilities(map[sdk.Capability]bool{
		sdk.CapabilityForHostMethod(hostMethodSchedulerSelectAccount): true,
		sdk.CapabilityForHostMethod(hostMethodSchedulerReportResult):  true,
	})

	selected := invokeHostJSON(t, handle, hostMethodSchedulerSelectAccount, map[string]interface{}{
		"group_id": group.ID,
		"model":    "gpt-4.1",
		"method":   http.MethodPost,
		"path":     "/v1/chat/completions",
	}, "")
	if int(selected["account_id"].(float64)) != account.ID || selected["platform"] != "openai" {
		t.Fatalf("selected account payload = %+v", selected)
	}
	reported := invokeHostJSON(t, handle, hostMethodSchedulerReportResult, map[string]interface{}{
		"account_id": account.ID,
		"success":    true,
	}, "")
	if reported["ok"] != true {
		t.Fatalf("report payload = %+v", reported)
	}

	if _, err := host.selectAccount(ctx, hostSelectAccountRequest{GroupID: 0}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("selectAccount invalid group error = %v", err)
	}
	missingGroup, err := host.probeForward(ctx, hostProbeForwardRequest{GroupID: int64(group.ID + 1000), Model: "gpt-4.1"})
	if err != nil || missingGroup["success"] != false || missingGroup["error_kind"] != "group_not_found" {
		t.Fatalf("probe missing group = %+v, %v", missingGroup, err)
	}
	probe, err := host.probeForward(ctx, hostProbeForwardRequest{GroupID: int64(group.ID)})
	if err != nil || probe["success"] != true || int(probe["account_id"].(int64)) != account.ID {
		t.Fatalf("probe success = %+v, %v", probe, err)
	}

	routes, email, err := host.hostForwardRoutes(ctx, hostForwardRequest{
		UserID: int64(user.ID),
		Model:  "gpt-4.1",
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Headers: map[string]interface{}{
			"X-Airgate-Platform": "openai",
		},
	})
	if err != nil || email != user.Email || len(routes) == 0 {
		t.Fatalf("hostForwardRoutes platform header routes=%+v email=%q err=%v", routes, email, err)
	}

	forward, err := host.forward(ctx, hostForwardRequest{
		UserID:  int64(user.ID),
		GroupID: int64(group.ID),
		Model:   "gpt-4.1",
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Headers: map[string]interface{}{"Content-Type": "application/json"},
		Body:    map[string]interface{}{"messages": []map[string]string{{"role": "user", "content": "hi"}}},
	})
	if err != nil || forward["status_code"] != http.StatusAccepted || !strings.Contains(forward["body"].(string), `"ok":true`) {
		t.Fatalf("host forward success = %+v, %v", forward, err)
	}
	if forward["usage"] == nil {
		t.Fatalf("host forward should include usage: %+v", forward)
	}

	clientErr, err := host.forward(ctx, hostForwardRequest{
		UserID:  int64(user.ID),
		GroupID: int64(group.ID),
		Model:   "gpt-4.1",
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Body:    map[string]interface{}{"messages": []map[string]string{{"role": "user", "content": "bad"}}},
	})
	if err != nil || clientErr["status_code"] != http.StatusBadRequest || !strings.Contains(clientErr["body"].(string), `"bad"`) {
		t.Fatalf("host forward client error payload = %+v, %v", clientErr, err)
	}
}

func TestHostForwardRejectsInactivePrincipalsAndExclusiveBypass(t *testing.T) {
	ctx := context.Background()
	restoreRouteGraph := routegraph.SetSnapshotForTesting(nil)
	t.Cleanup(restoreRouteGraph)

	db := testdb.OpenMemoryEnt(t, "host_forward_authz_boundaries", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	activeUser := db.User.Create().
		SetEmail("active@example.com").
		SetUsername("active").
		SetPasswordHash("hash").
		SetBalance(100).
		SaveX(ctx)
	disabledUser := db.User.Create().
		SetEmail("disabled@example.com").
		SetUsername("disabled").
		SetPasswordHash("hash").
		SetBalance(100).
		SetStatus(entuser.StatusDisabled).
		SaveX(ctx)
	publicGroup := db.Group.Create().
		SetName("public").
		SetPlatform("openai").
		SaveX(ctx)
	exclusiveGroup := db.Group.Create().
		SetName("exclusive").
		SetPlatform("openai").
		SetIsExclusive(true).
		SaveX(ctx)
	db.Account.Create().
		SetName("public account").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-public"}).
		AddGroupIDs(publicGroup.ID).
		SaveX(ctx)
	db.Account.Create().
		SetName("exclusive account").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-exclusive"}).
		AddGroupIDs(exclusiveGroup.ID).
		SaveX(ctx)

	activeKey := db.APIKey.Create().
		SetName("active").
		SetKeyHash("active").
		SetSellRate(1.5).
		SetUser(activeUser).
		SaveX(ctx)
	disabledKey := db.APIKey.Create().
		SetName("disabled").
		SetKeyHash("disabled").
		SetStatus(entapikey.StatusDisabled).
		SetUser(activeUser).
		SaveX(ctx)
	expiredKey := db.APIKey.Create().
		SetName("expired").
		SetKeyHash("expired").
		SetExpiresAt(time.Now().Add(-time.Hour)).
		SetUser(activeUser).
		SaveX(ctx)
	exhaustedKey := db.APIKey.Create().
		SetName("exhausted").
		SetKeyHash("exhausted").
		SetQuotaUsd(1).
		SetUsedQuota(1).
		SetUser(activeUser).
		SaveX(ctx)

	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatalf("refresh routegraph: %v", err)
	}
	host := NewHostService(db, &Manager{}, nil, nil, nil, nil)

	_, _, err := host.hostForwardRoutes(ctx, hostForwardRequest{
		UserID: int64(disabledUser.ID),
		Model:  "gpt-4.1",
		Headers: map[string]interface{}{
			"X-Airgate-Platform": "openai",
		},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("disabled user route error = %v", err)
	}

	_, _, err = host.hostForwardRoutes(ctx, hostForwardRequest{
		UserID:  int64(activeUser.ID),
		GroupID: int64(exclusiveGroup.ID),
		Model:   "gpt-4.1",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("exclusive group route error = %v", err)
	}

	if err := host.checkHostForwardAPIKey(hostForwardRequest{UserID: int64(activeUser.ID), APIKeyID: int64(activeKey.ID)}); err != nil {
		t.Fatalf("active api key rejected: %v", err)
	}
	if sellRate, err := host.hostForwardSellRate(ctx, hostForwardRequest{UserID: int64(activeUser.ID), APIKeyID: int64(activeKey.ID)}); err != nil || sellRate != 1.5 {
		t.Fatalf("active sell rate = %v, %v", sellRate, err)
	}

	for _, tt := range []struct {
		name string
		key  int
		code codes.Code
	}{
		{name: "disabled", key: disabledKey.ID, code: codes.PermissionDenied},
		{name: "expired", key: expiredKey.ID, code: codes.PermissionDenied},
		{name: "exhausted", key: exhaustedKey.ID, code: codes.ResourceExhausted},
	} {
		err := host.checkHostForwardAPIKey(hostForwardRequest{UserID: int64(activeUser.ID), APIKeyID: int64(tt.key)})
		if status.Code(err) != tt.code {
			t.Fatalf("%s key error = %v, want %v", tt.name, err, tt.code)
		}
		if _, err := host.hostForwardSellRate(ctx, hostForwardRequest{UserID: int64(activeUser.ID), APIKeyID: int64(tt.key)}); err == nil {
			t.Fatalf("%s key sell rate error = nil", tt.name)
		}
	}
}

func TestHostServiceHeaderAndSmallHelperEdges(t *testing.T) {
	headers := protoHeadersToHTTPHost(map[string]interface{}{
		"X-String-Slice": []string{"a", "b"},
		"X-Interface":    []interface{}{"c", 4},
		"X-Nested-List":  map[string]interface{}{"values": []interface{}{"d", "e"}},
		"X-Nested-Text":  map[string]interface{}{"values": "f"},
		"X-Number":       7,
		"X-Nil":          nil,
	})
	if got := headers.Values("X-String-Slice"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("string slice header = %v", got)
	}
	if got := headers.Get("X-Interface"); got != "c" {
		t.Fatalf("interface header first value = %q", got)
	}
	if got := headers.Values("X-Nested-List"); len(got) != 2 || got[1] != "e" {
		t.Fatalf("nested list header = %v", got)
	}
	if headers.Get("X-Nested-Text") != "f" || headers.Get("X-Number") != "7" || headers.Get("X-Nil") != "" {
		t.Fatalf("headers = %v", headers)
	}

	if got := truncateProbeErr("short"); got != "short" {
		t.Fatalf("truncateProbeErr short = %q", got)
	}
	if got := truncateProbeErr(strings.Repeat("x", 600)); len(got) != 515 || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncateProbeErr long len = %d", len(got))
	}
}
