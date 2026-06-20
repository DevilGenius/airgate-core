package plugin

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DevilGenius/airgate-core/ent"
	enttask "github.com/DevilGenius/airgate-core/ent/task"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/billing"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/routing"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestContinuationRecoveryAdditionalEdges(t *testing.T) {
	t.Parallel()

	if recovered, err := recoverContinuationAffinityMissing(nil); recovered || err != nil {
		t.Fatalf("nil recover = %v/%v, want false nil", recovered, err)
	}
	if recovered, err := recoverContinuationAffinityMissing(&forwardState{continuationRecoveryApplied: true}); recovered || err != nil {
		t.Fatalf("already-applied recover = %v/%v, want false nil", recovered, err)
	}
	if recovered, err := recoverContinuationAffinityMissing(&forwardState{body: []byte(`{"model":`)}); recovered || err == nil {
		t.Fatalf("invalid JSON recover = %v/%v, want false error", recovered, err)
	}
	if recovered, err := recoverContinuationAffinityMissing(&forwardState{body: []byte(`{"model":"gpt-4.1"}`)}); recovered || err != nil {
		t.Fatalf("unchanged recover = %v/%v, want false nil", recovered, err)
	}

	state := &forwardState{
		body:               []byte(`{"model":"gpt-4.1","previous_response_id":"resp_old","reasoning_effort":"x-high"}`),
		previousResponseID: "resp_old",
	}
	recovered, err := recoverContinuationAffinityMissing(state)
	if err != nil || !recovered || state.reasoningEffort != "xhigh" {
		t.Fatalf("reasoning recover = recovered %v effort %q err %v", recovered, state.reasoningEffort, err)
	}

	if got := continuationRecoveryMaxBytes(&Manager{modelCache: map[string][]sdk.ModelInfo{
		"openai": {{ID: "tiny", ContextWindow: 10}},
	}}, &forwardState{requestedPlatform: "openai"}, parsedRequest{Model: "tiny"}); got != continuationRecoveryMinBodyBytes {
		t.Fatalf("tiny model max bytes = %d, want min %d", got, continuationRecoveryMinBodyBytes)
	}
	if got := continuationRecoveryMaxBytes(&Manager{modelCache: map[string][]sdk.ModelInfo{
		"openai": {{ID: "huge", ContextWindow: 1 << 30}},
	}}, &forwardState{requestedPlatform: "openai"}, parsedRequest{Model: "huge"}); got != continuationRecoveryMaxBodyBytes {
		t.Fatalf("huge model max bytes = %d, want cap %d", got, continuationRecoveryMaxBodyBytes)
	}
	if got := continuationRecoveryMaxBytes(nil, nil, parsedRequest{HasCompactionReplay: true}); got != continuationRecoveryMaxBodyBytes {
		t.Fatalf("compaction max bytes = %d, want %d", got, continuationRecoveryMaxBodyBytes)
	}
	if got := continuationRecoveryPlatforms(nil); got != nil {
		t.Fatalf("nil platforms = %#v, want nil", got)
	}
	platforms := continuationRecoveryPlatforms(&forwardState{
		requestedPlatform: " openai ",
		selectedRoute:     routing.Candidate{Platform: "openai"},
		plugin:            &PluginInstance{Platform: "anthropic"},
	})
	if len(platforms) != 2 || platforms[0] != "openai" || platforms[1] != "anthropic" {
		t.Fatalf("platforms = %#v", platforms)
	}
	if got := continuationRecoveryModelCandidates(nil, parsedRequest{}); got != nil {
		t.Fatalf("nil model candidates = %#v, want nil", got)
	}
	if got := findContinuationModelContextWindow(nil, []string{"gpt"}); got != 0 {
		t.Fatalf("empty model window = %d, want 0", got)
	}
	if got := findContinuationModelContextWindow([]sdk.ModelInfo{{ID: "gpt", ContextWindow: 0}}, []string{"missing"}); got != 0 {
		t.Fatalf("missing model window = %d, want 0", got)
	}
}

func TestContinuationRecoveryTrimAndSanitizeEdges(t *testing.T) {
	t.Parallel()

	if trimEncryptedReasoningItems(nil) {
		t.Fatal("nil request data should not change")
	}
	noInput := map[string]any{"model": "gpt"}
	if trimEncryptedReasoningItems(noInput) {
		t.Fatal("request without input should not change")
	}
	textInput := map[string]any{"input": "plain"}
	if trimEncryptedReasoningItems(textInput) {
		t.Fatal("plain input should not change")
	}
	noChange := map[string]any{"input": map[string]any{"type": "message", "content": "hi"}}
	if trimEncryptedReasoningItems(noChange) {
		t.Fatal("non-reasoning object should not change")
	}
	kept := map[string]any{"input": map[string]any{"type": "reasoning", "id": "rs_1", "encrypted_content": "sealed"}}
	if !trimEncryptedReasoningItems(kept) {
		t.Fatal("reasoning object with id should change")
	}
	if input := kept["input"].(map[string]any); input["encrypted_content"] != nil || input["id"] != "rs_1" {
		t.Fatalf("kept input = %#v", input)
	}
	dropped := map[string]any{"input": map[string]any{"type": "reasoning", "encrypted_content": "sealed"}}
	if !trimEncryptedReasoningItems(dropped) {
		t.Fatal("reasoning-only object should change")
	}
	if _, ok := dropped["input"]; ok {
		t.Fatalf("reasoning-only input should be removed: %#v", dropped)
	}
	listDropped := map[string]any{"input": []any{map[string]any{"type": "reasoning", "encrypted_content": "sealed"}}}
	if !trimEncryptedReasoningItems(listDropped) {
		t.Fatal("reasoning-only list should change")
	}
	if _, ok := listDropped["input"]; ok {
		t.Fatalf("reasoning-only list should remove input: %#v", listDropped)
	}

	if next, changed, keep := sanitizeEncryptedReasoningInputItem("text"); next != "text" || changed || !keep {
		t.Fatalf("string sanitize = %#v/%v/%v", next, changed, keep)
	}
	if _, changed, keep := sanitizeEncryptedReasoningInputItem(map[string]any{"type": "reasoning"}); changed || !keep {
		t.Fatalf("reasoning without encrypted content = changed %v keep %v", changed, keep)
	}
}

func TestDispatchChainBoundaryBranches(t *testing.T) {
	t.Parallel()

	var nilChain *dispatchChain
	if got := nilChain.Plans(); got != nil {
		t.Fatalf("nil Plans = %#v, want nil", got)
	}
	if got := nilChain.StartIndex(); got != 0 {
		t.Fatalf("nil StartIndex = %d, want 0", got)
	}
	if _, ok := nilChain.Advance(); ok {
		t.Fatal("nil Advance should fail")
	}
	if _, ok := nilChain.next(-5); ok {
		t.Fatal("nil next should fail")
	}
	if got := nilChain.candidateAt(0); got.SchedulingModel != "" {
		t.Fatalf("nil candidate = %+v", got)
	}

	chain := newDispatchChain([]sdk.DispatchPlan{{}, {SchedulingModel: " gpt-a "}})
	chain.floor = -1
	if got := chain.StartIndex(); got != 0 {
		t.Fatalf("negative floor StartIndex = %d, want 0", got)
	}
	chain.floor = 10
	if got := chain.StartIndex(); got != 2 {
		t.Fatalf("large floor StartIndex = %d, want len", got)
	}
	if _, ok := chain.Advance(); ok {
		t.Fatal("unselected Advance should fail")
	}
	blank := chain.Select(0)
	if blank.SchedulingModel != "" || chain.selected {
		t.Fatalf("blank Select = %+v selected=%v", blank, chain.selected)
	}
	chain.floor = 0
	if next, ok := chain.next(-5); !ok || next.Index != 1 {
		t.Fatalf("next from negative = %+v/%v, want index 1", next, ok)
	}
	chain.selected = true
	chain.current = -1
	chain.floor = 2
	if _, ok := chain.Advance(); ok {
		t.Fatal("Advance beyond candidates should fail")
	}
}

func TestImagePricingAdditionalBranches(t *testing.T) {
	t.Parallel()

	applyImageBillingCostPolicy(nil, &sdk.Usage{}, nil, "/v1/responses")
	input := &billing.CalculateInput{}
	applyImageBillingCostPolicy(input, &sdk.Usage{}, nil, "/v1/responses")
	if input.BillingCostAddon != nil || input.BillingCostOverride != nil {
		t.Fatalf("empty usage should not set image cost: %+v", input)
	}

	for _, usage := range []*sdk.Usage{
		{Metadata: map[string]string{"openai.image.size": "bad", "openai.image.count": "1"}},
		{Metadata: map[string]string{"openai.image.size": "1024x1024", "openai.image.count": "1"}},
		{Metadata: map[string]string{"openai.image.size": "1024x1024", "openai.image.count": "0"}},
	} {
		if cost, ok := imageBillingCostFromSettings(usage, nil); ok || cost != 0 {
			t.Fatalf("imageBillingCostFromSettings(%+v) = %v/%v, want 0 false", usage, cost, ok)
		}
	}

	settings := map[string]map[string]string{
		"other":  {imagePrice1KKey: "1"},
		"OpenAI": {"ignored": "1", imagePrice1KKey: " "},
	}
	if price, ok := imageTierPriceFromSettings(settings, "1k"); ok || price != 0 {
		t.Fatalf("blank price = %v/%v, want 0 false", price, ok)
	}
	settings["OpenAI"][imagePrice1KKey] = "-1"
	if price, ok := imageTierPriceFromSettings(settings, "1k"); ok || price != 0 {
		t.Fatalf("negative price = %v/%v, want 0 false", price, ok)
	}
	settings["OpenAI"][imagePrice1KKey] = "bad"
	if price, ok := imageTierPriceFromSettings(settings, "1k"); ok || price != 0 {
		t.Fatalf("bad price = %v/%v, want 0 false", price, ok)
	}
	if key := imageTierPriceKey("unknown"); key != "" {
		t.Fatalf("unknown tier key = %q, want empty", key)
	}
	if shouldForwardPluginSetting("other", imagePrice1KKey) != true {
		t.Fatal("non-openai image price setting should be forwarded")
	}
	if shouldForwardPluginSetting("openai", imagePrice1KKey) != false {
		t.Fatal("openai image price setting should not be forwarded")
	}

	for _, raw := range []string{"1024", "badx1024", "0x1024", "1024xbad", "1024x0"} {
		if w, h, ok := parseImageSizeForBilling(raw); ok || w != 0 || h != 0 {
			t.Fatalf("parseImageSizeForBilling(%q) = %d/%d/%v, want zeros false", raw, w, h, ok)
		}
	}
	if tier, _, ok := imageTierForSize("bad"); ok || tier != "" {
		t.Fatalf("bad tier = %q/%v, want empty false", tier, ok)
	}
}

func TestTaskInputAssetAdditionalEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage := newTestAssetStorage(t)
	largePayload := strings.Repeat("A", taskInputAssetThreshold)
	noComma := "data:image/png;base64" + largePayload
	if got, key, err := maybeStoreDataURI(ctx, storage, 1, noComma); err != nil || got != noComma || key != "" {
		t.Fatalf("no comma = %q/%q/%v", got[:20], key, err)
	}
	nonBase64 := "data:image/png;name=test," + largePayload
	if got, key, err := maybeStoreDataURI(ctx, storage, 1, nonBase64); err != nil || got != nonBase64 || key != "" {
		t.Fatalf("non-base64 = %q/%q/%v", got[:20], key, err)
	}

	rawData := []byte(strings.Repeat("r", taskInputAssetThreshold+1))
	rawRef := "data:image/png;base64," + base64.RawStdEncoding.EncodeToString(rawData)
	got, key, err := maybeStoreDataURI(ctx, storage, 7, rawRef)
	if err != nil || !strings.HasPrefix(got, "/assets-runtime/") || key == "" {
		t.Fatalf("raw base64 store = %q/%q/%v", got, key, err)
	}

	invalid := "data:image/png;base64," + strings.Repeat("@", taskInputAssetThreshold)
	if _, _, err := maybeStoreDataURI(ctx, storage, 1, invalid); err == nil {
		t.Fatal("invalid base64 should fail")
	}
	input := map[string]any{"bad": invalid}
	if _, err := normalizeTaskInputAssets(ctx, storage, 1, input); err == nil || !strings.Contains(err.Error(), "input[bad]") {
		t.Fatalf("map invalid error = %v", err)
	}
	nested := map[string]any{"items": []any{map[string]any{"bad": invalid}}}
	if _, err := normalizeTaskInputAssets(ctx, storage, 1, nested); err == nil || !strings.Contains(err.Error(), "input[bad]") {
		t.Fatalf("nested invalid error = %v", err)
	}
}

func TestTaskServiceValidationPayloadAndRouteEdges(t *testing.T) {
	t.Parallel()

	if _, err := validateTaskStatus("not-a-status"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid status error = %v", err)
	}
	if err := validateTaskTransition(enttask.StatusPending, enttask.StatusPending); err != nil {
		t.Fatalf("same transition error = %v", err)
	}
	if err := validateTaskTransition(enttask.StatusCompleted, enttask.StatusPending); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("terminal transition error = %v", err)
	}
	if err := validateTaskTransition(enttask.StatusPending, enttask.StatusCompleted); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("invalid transition error = %v", err)
	}

	now := time.Now().UTC()
	usageID := 123
	publicID := "pub-1"
	idempotencyKey := "idem-1"
	payload := taskToPayload(&ent.Task{
		ID:                1,
		PluginID:          "plugin-a",
		TaskType:          "image",
		Status:            enttask.StatusCompleted,
		UserID:            2,
		Input:             map[string]interface{}{"prompt": "hi"},
		Output:            map[string]interface{}{"url": "x"},
		Attributes:        map[string]interface{}{"kind": "test"},
		Execution:         map[string]interface{}{"phase": "done"},
		UsageID:           &usageID,
		PublicTaskID:      &publicID,
		IdempotencyKey:    &idempotencyKey,
		CreatedAt:         now,
		UpdatedAt:         now,
		StartedAt:         &now,
		CompletedAt:       &now,
		CancelRequestedAt: &now,
		ExpiresAt:         &now,
	})
	for _, key := range []string{"input", "output", "attributes", "execution", "usage_id", "idempotency_key", "cancel_requested_at", "expires_at"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("payload missing %s: %+v", key, payload)
		}
	}
}

func TestTaskServiceSQLiteAdditionalBranches(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "plugin_task_service_edges", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	t.Setenv("ASSETS_DIR", t.TempDir())
	host := &HostService{db: db}

	if _, err := host.createTask(ctx, "plugin-a", hostCreateTaskRequest{UserID: 1}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("create missing task type error = %v", err)
	}
	if _, err := host.createTask(ctx, "plugin-a", hostCreateTaskRequest{TaskType: "image"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("create missing user error = %v", err)
	}

	created, err := host.createTask(ctx, "plugin-a", hostCreateTaskRequest{
		PluginID:       "plugin-b",
		UserID:         7,
		TaskType:       "image",
		Input:          map[string]interface{}{"prompt": "first"},
		Execution:      map[string]interface{}{"phase": "queued"},
		IdempotencyKey: "idem",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskPayload := created["task"].(map[string]interface{})
	taskID := int(taskPayload["id"].(int64))
	if taskPayload["plugin_id"] != "plugin-b" || taskPayload["max_attempts"] != 3 {
		t.Fatalf("created payload = %+v", taskPayload)
	}
	again, err := host.createTask(ctx, "plugin-b", hostCreateTaskRequest{
		UserID:         7,
		TaskType:       "image",
		Input:          map[string]interface{}{"prompt": "second"},
		IdempotencyKey: "idem",
	})
	if err != nil {
		t.Fatalf("idempotent create: %v", err)
	}
	if again["task"].(map[string]interface{})["id"] != taskPayload["id"] {
		t.Fatalf("idempotent create returned different task: %+v", again)
	}

	if _, err := host.updateTask(ctx, "plugin-b", hostUpdateTaskRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("update missing id error = %v", err)
	}
	if _, err := host.updateTask(ctx, "plugin-b", hostUpdateTaskRequest{TaskID: int64(taskID + 1000)}); status.Code(err) != codes.NotFound {
		t.Fatalf("update not found error = %v", err)
	}
	if _, err := host.updateTask(ctx, "plugin-b", hostUpdateTaskRequest{TaskID: int64(taskID), Status: "not-a-status"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("update invalid status error = %v", err)
	}
	if _, err := host.updateTask(ctx, "plugin-b", hostUpdateTaskRequest{TaskID: int64(taskID), Status: enttask.StatusCompleted.String()}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("update invalid transition error = %v", err)
	}

	progress := 42
	stage := "running"
	usageID := 99
	updated, err := host.updateTask(ctx, "plugin-b", hostUpdateTaskRequest{
		TaskID:       int64(taskID),
		Status:       enttask.StatusProcessing.String(),
		Progress:     &progress,
		Stage:        &stage,
		Output:       map[string]interface{}{"partial": true},
		Attributes:   map[string]interface{}{"kind": "edge"},
		Execution:    map[string]interface{}{"phase": "run"},
		ErrorType:    "temporary",
		ErrorCode:    "warming_up",
		ErrorMessage: "warmup",
		UsageID:      &usageID,
	})
	if err != nil {
		t.Fatalf("update processing task: %v", err)
	}
	updatedTask := updated["task"].(map[string]interface{})
	if updatedTask["stage"] != stage || updatedTask["progress"] != progress || updatedTask["started_at"] == nil || updatedTask["usage_id"] != usageID {
		t.Fatalf("updated processing task = %+v", updatedTask)
	}
	if _, err := host.updateTask(ctx, "plugin-b", hostUpdateTaskRequest{TaskID: int64(taskID), Status: enttask.StatusCompleted.String()}); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	completed, err := db.Task.Get(ctx, taskID)
	if err != nil {
		t.Fatalf("get completed task: %v", err)
	}
	if completed.CompletedAt == nil {
		t.Fatal("completed task should set completed_at")
	}

	if _, err := host.getTask(ctx, "plugin-b", hostGetTaskRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("get missing id error = %v", err)
	}
	if _, err := host.getTask(ctx, "plugin-b", hostGetTaskRequest{TaskID: int64(taskID + 2000)}); status.Code(err) != codes.NotFound {
		t.Fatalf("get not found error = %v", err)
	}
	if _, err := host.listTasks(ctx, "plugin-b", hostListTasksRequest{Status: "not-a-status"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("list invalid status error = %v", err)
	}
	listed, err := host.listTasks(ctx, "plugin-b", hostListTasksRequest{UserID: 7, TaskType: "image", Status: enttask.StatusCompleted.String(), Limit: 200})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if listed["total"].(int) != 1 {
		t.Fatalf("listed total = %+v", listed)
	}

	running, err := host.createTask(ctx, "plugin-b", hostCreateTaskRequest{UserID: 7, TaskType: "video"})
	if err != nil {
		t.Fatalf("create running task: %v", err)
	}
	runningID := int(running["task"].(map[string]interface{})["id"].(int64))
	if _, err := host.deleteTask(ctx, "plugin-b", hostDeleteTaskRequest{TaskID: int64(runningID)}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("delete running task error = %v", err)
	}
	if _, err := host.deleteTask(ctx, "plugin-b", hostDeleteTaskRequest{PluginID: "plugin-b", TaskID: int64(taskID + 3000)}); status.Code(err) != codes.NotFound {
		t.Fatalf("delete not found error = %v", err)
	}
	if _, err := host.deleteTask(ctx, "plugin-a", hostDeleteTaskRequest{PluginID: "plugin-b", TaskID: int64(taskID), UserID: 7}); err != nil {
		t.Fatalf("delete completed task with plugin override: %v", err)
	}
}

func TestForwarderForwardSuccessRuntime(t *testing.T) {
	ctx := context.Background()
	restoreRouteGraph := routegraph.SetSnapshotForTesting(nil)
	t.Cleanup(restoreRouteGraph)

	db := testdb.OpenMemoryEnt(t, "plugin_forwarder_forward_success", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	user := db.User.Create().
		SetEmail("forward-runtime@example.com").
		SetUsername("forward-runtime").
		SetPasswordHash("hash").
		SetBalance(100).
		SaveX(ctx)
	group := db.Group.Create().
		SetName("Forward Runtime").
		SetPlatform("openai").
		SetRateMultiplier(1.2).
		SetPluginSettings(map[string]map[string]string{"openai": {"image_enabled": "true"}}).
		SaveX(ctx)
	account := db.Account.Create().
		SetName("Forward Runtime Account").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-forward"}).
		SetMaxConcurrency(2).
		AddGroupIDs(group.ID).
		SaveX(ctx)
	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatalf("refresh routegraph: %v", err)
	}

	var seen *sdk.ForwardRequest
	client, cleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "gateway-openai",
		platform: "openai",
		forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			seen = req
			return sdk.ForwardOutcome{
				Kind: sdk.OutcomeSuccess,
				Upstream: sdk.UpstreamResponse{
					StatusCode: http.StatusCreated,
					Headers:    http.Header{"Content-Type": {"application/json"}, "X-Gateway": {"forwarder"}},
					Body:       []byte(`{"id":"resp_forward_runtime","ok":true}`),
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
	manager.routeCache["gateway-openai"] = []sdk.RouteDefinition{{Path: "/v1"}}
	manager.modelCache = map[string][]sdk.ModelInfo{"openai": {{ID: "gpt-4.1", Name: "GPT-4.1"}}}

	forwarder := NewForwarder(
		db,
		manager,
		scheduler.NewScheduler(db, nil),
		scheduler.NewConcurrencyManager(nil),
		billing.NewCalculator(),
		nil,
	)
	c, recorder := pluginTestContext(http.MethodPost, "/v1/chat/completions")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?trace=1", strings.NewReader(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(sdk.WithRequestID(req.Context(), "forward-runtime-request"))
	c.Request = req
	c.Set(middleware.CtxKeyRequestID, "gin-forward-runtime-request")
	c.Set(middleware.CtxKeyKeyInfo, &auth.APIKeyInfo{
		KeyID:               301,
		KeyName:             "forward-key",
		UserID:              user.ID,
		UserEmail:           user.Email,
		UserBalance:         100,
		UserMaxConcurrency:  1,
		KeyMaxConcurrency:   1,
		GroupID:             group.ID,
		GroupName:           group.Name,
		GroupPlatform:       "openai",
		GroupRateMultiplier: group.RateMultiplier,
		SellRate:            1,
	})

	forwarder.Forward(c)

	if recorder.Code != http.StatusCreated || recorder.Body.String() != `{"id":"resp_forward_runtime","ok":true}` {
		t.Fatalf("forward response status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Gateway") != "forwarder" {
		t.Fatalf("gateway header = %q", recorder.Header().Get("X-Gateway"))
	}
	if seen == nil || seen.Account == nil || seen.Account.ID != int64(account.ID) || seen.DispatchPlan.SchedulingModel != "gpt-4.1" {
		t.Fatalf("seen request = %+v", seen)
	}
	if seen.Headers.Get("X-Forwarded-Path") != "/v1/chat/completions" || seen.Headers.Get("X-Forwarded-Query") != "trace=1" {
		t.Fatalf("forwarded headers = %v", seen.Headers)
	}
	if accountID, ok := c.Get(ginCtxKeyAccountID); !ok || accountID != account.ID {
		t.Fatalf("gin account id = %#v/%v", accountID, ok)
	}
	if attempts, ok := c.Get(ginCtxKeyAttempts); !ok || attempts != 1 {
		t.Fatalf("gin attempts = %#v/%v", attempts, ok)
	}
}

func TestHostServiceForwardStreamRuntime(t *testing.T) {
	ctx := context.Background()
	restoreRouteGraph := routegraph.SetSnapshotForTesting(nil)
	t.Cleanup(restoreRouteGraph)

	db := testdb.OpenMemoryEnt(t, "plugin_host_forward_stream_runtime", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })

	user := db.User.Create().
		SetEmail("host-stream@example.com").
		SetUsername("host-stream").
		SetPasswordHash("hash").
		SetBalance(100).
		SaveX(ctx)
	group := db.Group.Create().
		SetName("Host Stream").
		SetPlatform("openai").
		SaveX(ctx)
	account := db.Account.Create().
		SetName("Host Stream Account").
		SetPlatform("openai").
		SetType("apikey").
		SetCredentials(map[string]string{"api_key": "sk-stream"}).
		SetMaxConcurrency(2).
		AddGroupIDs(group.ID).
		SaveX(ctx)
	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatalf("refresh routegraph: %v", err)
	}

	client, cleanup := newGatewayRuntimeClient(t, &pluginRuntimeGateway{
		id:       "gateway-openai",
		platform: "openai",
		forward: func(_ context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
			if !req.Stream || req.Writer == nil || req.Account.ID != int64(account.ID) {
				t.Fatalf("stream request = %+v", req)
			}
			req.Writer.Header().Set("Content-Type", "text/event-stream")
			req.Writer.WriteHeader(http.StatusOK)
			if _, err := req.Writer.Write([]byte("data: one\n\n")); err != nil {
				t.Fatalf("write stream chunk: %v", err)
			}
			if flusher, ok := req.Writer.(http.Flusher); ok {
				flusher.Flush()
			}
			return sdk.ForwardOutcome{Kind: sdk.OutcomeSuccess}, nil
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

	stream := &captureHostInvokeStream{ctx: ctx}
	err := host.forwardStream(ctx, hostForwardRequest{
		UserID:  int64(user.ID),
		GroupID: int64(group.ID),
		Model:   "gpt-4.1",
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Headers: map[string]interface{}{"Content-Type": "application/json"},
		Body:    map[string]interface{}{"model": "gpt-4.1", "messages": []map[string]string{{"role": "user", "content": "hi"}}},
	}, stream)
	if err != nil {
		t.Fatalf("forwardStream error: %v", err)
	}
	if len(stream.sent) < 3 {
		t.Fatalf("stream frames = %d, want headers/chunk/done", len(stream.sent))
	}
	if stream.sent[0].Event != "headers" || !strings.Contains(string(stream.sent[0].Payload), "status_code") {
		t.Fatalf("headers frame = %+v", stream.sent[0])
	}
	if stream.sent[1].Event != "chunk" || !strings.Contains(string(stream.sent[1].Payload), "data: one") {
		t.Fatalf("chunk frame = %+v", stream.sent[1])
	}
	done := stream.sent[len(stream.sent)-1]
	if done.Event != "done" || !done.Done || done.Status != "ok" {
		t.Fatalf("done frame = %+v", done)
	}
}
