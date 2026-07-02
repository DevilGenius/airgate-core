package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/routegraph"
	"github.com/DevilGenius/airgate-core/internal/scheduler"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	pb "github.com/DevilGenius/airgate-sdk/protocol/proto"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestHostInvokeDispatchesLocalMethods(t *testing.T) {
	ctx := context.Background()
	restoreRouteGraph := routegraph.SetSnapshotForTesting(nil)
	t.Cleanup(restoreRouteGraph)

	db := testdb.OpenMemoryEnt(t, "host_invoke_methods", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	localDir := t.TempDir()
	db.Setting.Create().SetGroup("storage").SetKey("local_storage_dir").SetValue(localDir).SaveX(ctx)

	user := db.User.Create().
		SetEmail("host@example.com").
		SetUsername("host-user").
		SetPasswordHash("hash").
		SetBalance(10).
		SaveX(ctx)
	group := db.Group.Create().
		SetName("primary").
		SetPlatform("openai").
		SetRateMultiplier(1.25).
		SetIsExclusive(true).
		SaveX(ctx)
	apiKey := db.APIKey.Create().
		SetName("host key").
		SetKeyHash("hash").
		SetSellRate(1.75).
		SetUser(user).
		SetGroup(group).
		SaveX(ctx)
	if err := routegraph.RefreshSync(ctx, db); err != nil {
		t.Fatalf("routegraph refresh: %v", err)
	}

	mgr := &Manager{
		pluginDir: localDir,
		instances: map[string]*PluginInstance{
			"gateway-openai": {
				Name:        "gateway-openai",
				DisplayName: "OpenAI",
				Type:        "gateway",
				Platform:    "openai",
			},
			"gateway-openai-copy": {
				Name:        "gateway-openai-copy",
				DisplayName: "OpenAI copy",
				Type:        "gateway",
				Platform:    "openai",
			},
			"studio": {
				Name:        "studio",
				DisplayName: "Studio",
				Type:        "extension",
			},
		},
		modelCache: map[string][]sdk.ModelInfo{
			"openai": {{
				ID:              "gpt-4.1",
				Name:            "GPT-4.1",
				ContextWindow:   128000,
				MaxOutputTokens: 8192,
				Capabilities:    []string{"chat"},
				Metadata:        map[string]string{"tier": "flagship"},
			}},
		},
		frontendPageCache: map[string][]sdk.FrontendPage{},
		devPaths:          map[string]string{},
	}
	host := NewHostService(db, mgr, nil, nil, nil, nil)
	handle := host.NewPluginHandle("studio")
	handle.SetCapabilities(map[sdk.Capability]bool{sdk.CapabilityHostInvoke: true})

	platforms := invokeHostJSON(t, handle, hostMethodPlatformsList, nil, "")
	if got := len(platforms["platforms"].([]interface{})); got != 1 {
		t.Fatalf("platform count = %d, want 1", got)
	}
	models := invokeHostJSON(t, handle, hostMethodModelsList, map[string]interface{}{"platform": "openai"}, "")
	modelItems := models["models"].([]interface{})
	if len(modelItems) != 1 || modelItems[0].(map[string]interface{})["id"] != "gpt-4.1" {
		t.Fatalf("models payload = %+v", models)
	}
	groups := invokeHostJSON(t, handle, hostMethodGroupsList, nil, "")
	groupItems := groups["groups"].([]interface{})
	if len(groupItems) != 1 || groupItems[0].(map[string]interface{})["name"] != "primary" {
		t.Fatalf("groups payload = %+v", groups)
	}
	userInfo := invokeHostJSON(t, handle, hostMethodUsersGet, map[string]interface{}{"user_id": user.ID}, "")
	if userInfo["email"] != "host@example.com" || userInfo["balance"].(float64) != 10 {
		t.Fatalf("user payload = %+v", userInfo)
	}

	if sellRate, err := host.hostForwardSellRate(ctx, hostForwardRequest{UserID: int64(user.ID), APIKeyID: int64(apiKey.ID)}); err != nil || sellRate != 1.75 {
		t.Fatalf("hostForwardSellRate() = %v, %v", sellRate, err)
	}
	if _, err := host.hostForwardSellRate(ctx, hostForwardRequest{UserID: int64(user.ID + 1), APIKeyID: int64(apiKey.ID)}); err == nil {
		t.Fatal("hostForwardSellRate wrong user error = nil")
	}

	stored := invokeHostJSON(t, handle, hostMethodAssetsStore, map[string]interface{}{
		"user_id":        user.ID,
		"purpose":        string(AssetPurposeUpload),
		"content_type":   "image/png",
		"file_extension": ".png",
		"data":           []byte("asset-bytes"),
	}, "")
	objectKey := stored["object_key"].(string)
	if objectKey == "" || stored["content_type"] != "image/png" {
		t.Fatalf("stored asset payload = %+v", stored)
	}
	urlPayload := invokeHostJSON(t, handle, hostMethodAssetsGetURL, map[string]interface{}{"object_key": objectKey}, "")
	if urlPayload["public_url"] == "" {
		t.Fatalf("asset URL payload = %+v", urlPayload)
	}
	bytesPayload := invokeHostJSON(t, handle, hostMethodAssetsGetBytes, map[string]interface{}{"object_key": objectKey}, "")
	if bytesPayload["content_type"] != "image/png" || bytesPayload["data"] == "" {
		t.Fatalf("asset bytes payload = %+v", bytesPayload)
	}

	allowPrivateAssetDownloads(t)
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png; charset=binary")
		_, _ = w.Write([]byte("remote-asset"))
	}))
	t.Cleanup(downloadServer.Close)
	storedURL := invokeHostJSON(t, handle, hostMethodAssetsStoreURL, map[string]interface{}{
		"user_id":    user.ID,
		"purpose":    string(AssetPurposeChat),
		"source_url": downloadServer.URL + "/asset.png",
	}, "")
	if storedURL["content_type"] != "image/png" {
		t.Fatalf("stored URL asset payload = %+v", storedURL)
	}

	task := invokeHostJSON(t, handle, hostMethodTasksCreate, map[string]interface{}{
		"task_type": "image.generate",
		"user_id":   user.ID,
		"input":     map[string]interface{}{"prompt": "draw"},
	}, "idem-1")["task"].(map[string]interface{})
	taskID := int64(task["id"].(float64))
	if task["idempotency_key"] != "idem-1" {
		t.Fatalf("task payload = %+v", task)
	}
	progress := 25
	stage := "rendering"
	invokeHostJSON(t, handle, hostMethodTasksUpdate, map[string]interface{}{
		"task_id":  taskID,
		"status":   "processing",
		"progress": progress,
		"stage":    stage,
	}, "")
	invokeHostJSON(t, handle, hostMethodTasksUpdate, map[string]interface{}{
		"task_id":       taskID,
		"status":        "failed",
		"error_type":    "upstream",
		"error_code":    "boom",
		"error_message": "failed",
	}, "")
	gotTask := invokeHostJSON(t, handle, hostMethodTasksGet, map[string]interface{}{"task_id": taskID, "user_id": user.ID}, "")["task"].(map[string]interface{})
	if gotTask["status"] != "failed" || gotTask["error_code"] != "boom" {
		t.Fatalf("got task payload = %+v", gotTask)
	}
	listed := invokeHostJSON(t, handle, hostMethodTasksList, map[string]interface{}{"user_id": user.ID, "status": "failed"}, "")
	if listed["total"].(float64) != 1 {
		t.Fatalf("listed tasks payload = %+v", listed)
	}
	deleted := invokeHostJSON(t, handle, hostMethodTasksDelete, map[string]interface{}{"task_id": taskID, "user_id": user.ID}, "")
	if deleted["deleted"] != true {
		t.Fatalf("delete task payload = %+v", deleted)
	}
	deletedAsset := invokeHostJSON(t, handle, hostMethodAssetsDelete, map[string]interface{}{"object_key": objectKey}, "")
	if deletedAsset["deleted"] != true {
		t.Fatalf("delete asset payload = %+v", deletedAsset)
	}
}

func TestHostInvokeValidationErrors(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "host_invoke_validation", schema.WithGlobalUniqueID(false))
	t.Cleanup(func() { _ = db.Close() })
	host := NewHostService(db, &Manager{}, nil, nil, nil, nil)
	handle := host.NewPluginHandle("plugin")
	handle.SetCapabilities(map[sdk.Capability]bool{sdk.CapabilityHostInvoke: true})

	if _, err := handle.Invoke(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil Invoke error = %v", err)
	}
	if _, err := handle.Invoke(ctx, &pb.HostInvokeRequest{Method: hostMethodModelsList, Payload: []byte("{")}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad JSON Invoke error = %v", err)
	}
	if _, err := handle.Invoke(ctx, &pb.HostInvokeRequest{Method: "unknown.method"}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("unknown Invoke error = %v", err)
	}
	if _, err := handle.Invoke(ctx, &pb.HostInvokeRequest{Method: hostMethodModelsList, Payload: mustHostInvokePayload(t, map[string]interface{}{})}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty platform models error = %v", err)
	}
	if _, err := host.storeAsset(ctx, hostStoreAssetRequest{UserID: 0}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("storeAsset invalid user error = %v", err)
	}
	if _, err := host.storeAsset(ctx, hostStoreAssetRequest{UserID: 1, Purpose: string(AssetPurposeUpload)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("storeAsset missing data error = %v", err)
	}
	if _, err := host.storeAssetFromURL(ctx, hostStoreAssetFromURLRequest{UserID: 1, Purpose: string(AssetPurposeUpload)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("storeAssetFromURL missing source error = %v", err)
	}
	if _, err := host.deleteAsset(ctx, hostDeleteAssetRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("deleteAsset empty object key error = %v", err)
	}
}

func TestHostServicePureForwardHelpers(t *testing.T) {
	mgr := &Manager{modelCache: map[string][]sdk.ModelInfo{"openai": {{ID: "gpt-4.1"}}}}
	host := &HostService{manager: mgr}
	if got := host.resolveHostModel("openai", "explicit"); got != "explicit" {
		t.Fatalf("resolveHostModel explicit = %q", got)
	}
	if got := host.resolveHostModel("openai", ""); got != "gpt-4.1" {
		t.Fatalf("resolveHostModel fallback = %q", got)
	}
	if got := host.resolveHostModel("missing", ""); got != "" {
		t.Fatalf("resolveHostModel missing = %q", got)
	}
	if got := hostForwardMethodFromString(""); got != http.MethodPost {
		t.Fatalf("hostForwardMethodFromString empty = %q", got)
	}
	if got := dispatchSchedulingModels(nil, "fallback"); len(got) != 1 || got[0] != "fallback" {
		t.Fatalf("dispatchSchedulingModels fallback = %v", got)
	}
	models := dispatchSchedulingModels([]sdk.DispatchPlan{{SchedulingModel: "gpt"}, {SchedulingModel: "gpt"}, {}}, "")
	if len(models) != 1 || models[0] != "gpt" {
		t.Fatalf("dispatchSchedulingModels unique = %v", models)
	}
	if hostForwardMaxConcurrency(nil) != scheduler.DefaultAccountMaxConcurrency {
		t.Fatal("nil account should use default max concurrency")
	}
	acc := &ent.Account{ID: 7, Name: "acc", Platform: "openai", Type: "apikey", Credentials: map[string]string{"api_key": "sk"}, MaxConcurrency: 3}
	if hostForwardMaxConcurrency(acc) != 3 {
		t.Fatal("account max concurrency not respected")
	}
	sdkAcc := hostSDKAccount(acc)
	sdkAcc.Credentials["api_key"] = "mutated"
	if acc.Credentials["api_key"] != "sk" {
		t.Fatal("hostSDKAccount should clone credentials")
	}
}

func invokeHostJSON(t *testing.T, handle *pluginHostHandle, method string, payload map[string]interface{}, idempotencyKey string) map[string]interface{} {
	t.Helper()
	resp, err := handle.Invoke(context.Background(), &pb.HostInvokeRequest{
		Method:         method,
		Payload:        mustHostInvokePayload(t, payload),
		IdempotencyKey: idempotencyKey,
		Metadata:       map[string]string{"trace": "test"},
	})
	if err != nil {
		t.Fatalf("Invoke(%s) error = %v", method, err)
	}
	if resp.Status != "ok" || resp.Metadata["method"] != method {
		t.Fatalf("Invoke(%s) response = %+v", method, resp)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("decode Invoke(%s) payload: %v", method, err)
	}
	return out
}

func mustHostInvokePayload(t *testing.T, payload map[string]interface{}) []byte {
	t.Helper()
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal host payload: %v", err)
	}
	return data
}
