package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/DevilGenius/airgate-core/internal/app/pluginadmin"
	appuser "github.com/DevilGenius/airgate-core/internal/app/user"
	"github.com/DevilGenius/airgate-core/internal/infra/store"
	"github.com/DevilGenius/airgate-core/internal/plugin"
	"github.com/DevilGenius/airgate-core/internal/testdb"
	"github.com/DevilGenius/airgate-core/internal/upgrade"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

func TestPluginHandlerRoutesWithFakeAdminService(t *testing.T) {
	hash := strings.Repeat("a", 64)
	manager := &pluginHandlerManagerStub{
		allMeta: []plugin.PluginMeta{{
			Name:          "gateway-openai",
			DisplayName:   "OpenAI",
			Version:       "0.1.0",
			Author:        "AirGate",
			Type:          "gateway",
			Platform:      "openai",
			AccountTypes:  []sdk.AccountType{{Key: "oauth", Label: "OAuth", Description: "OAuth account"}},
			FrontendPages: []sdk.FrontendPage{{Path: "/plugins/openai", Title: "OpenAI", Icon: "plug", Audience: "admin"}},
			ConfigSchema:  []sdk.ConfigField{{Key: "api_key", Label: "API Key", Type: "password", Required: true}},
			Metadata:      map[string]string{"tier": "core"},
			HasWebAssets:  true,
			IsDev:         false,
			BinarySHA256:  hash,
		}},
		config:  map[string]string{"api_key": "secret"},
		loading: true,
		isDev:   true,
	}
	marketplace := &pluginHandlerMarketplaceStub{
		available: []plugin.MarketplacePlugin{{
			Name:        "gateway-openai",
			Version:     "0.2.0",
			Description: "OpenAI gateway",
			Author:      "AirGate",
			Type:        "gateway",
			GithubRepo:  "DevilGenius/airgate-openai",
			SHA256:      strings.Repeat("b", 64),
			CommitSHA:   strings.Repeat("c", 40),
		}},
	}
	handler := NewPluginHandler(pluginadmin.NewService(manager, marketplace))

	successCases := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		fn     func(*gin.Context)
		want   string
	}{
		{name: "list plugins", method: http.MethodGet, target: "/plugins", fn: handler.ListPlugins, want: `"loading":true`},
		{name: "list menu", method: http.MethodGet, target: "/plugins/menu", fn: handler.ListPluginMenu, want: `"/plugins/openai"`},
		{name: "get config", method: http.MethodGet, target: "/plugins/gateway-openai/config", params: gin.Params{{Key: "name", Value: "gateway-openai"}}, fn: handler.GetPluginConfig, want: `"api_key":"secret"`},
		{name: "update config", method: http.MethodPut, target: "/plugins/gateway-openai/config", params: gin.Params{{Key: "name", Value: "gateway-openai"}}, body: `{"config":{"api_key":"updated"}}`, fn: handler.UpdatePluginConfig},
		{name: "install github", method: http.MethodPost, target: "/plugins/github", body: `{"repo":"DevilGenius/airgate-openai","version":" v0.2.0 ","trust_frontend":true}`, fn: handler.InstallFromGithub},
		{name: "uninstall", method: http.MethodDelete, target: "/plugins/gateway-openai", params: gin.Params{{Key: "name", Value: "gateway-openai"}}, fn: handler.UninstallPlugin},
		{name: "reload", method: http.MethodPost, target: "/plugins/gateway-openai/reload", params: gin.Params{{Key: "name", Value: "gateway-openai"}}, fn: handler.ReloadPlugin},
		{name: "refresh marketplace", method: http.MethodPost, target: "/plugins/marketplace/refresh", fn: handler.RefreshMarketplace},
		{name: "list marketplace", method: http.MethodGet, target: "/plugins/marketplace", fn: handler.ListMarketplace, want: `"has_update":true`},
	}
	for _, tt := range successCases {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, nil, tt.fn)
			requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
			if tt.want != "" && !strings.Contains(w.Body.String(), tt.want) {
				t.Fatalf("%s body = %s, want %q", tt.name, w.Body.String(), tt.want)
			}
		})
	}
	if manager.updatedConfig["api_key"] != "updated" || !manager.reloadedInstance {
		t.Fatalf("update config did not update and reload: manager=%+v", manager)
	}
	if manager.githubRepo != "DevilGenius/airgate-openai" || manager.githubVersion != "v0.2.0" {
		t.Fatalf("github install args = %q %q", manager.githubRepo, manager.githubVersion)
	}
	if manager.uninstalled != "gateway-openai" || manager.reloadedDev != "gateway-openai" || !marketplace.synced {
		t.Fatalf("delegation state manager=%+v marketplace=%+v", manager, marketplace)
	}

	uploadBody := &bytes.Buffer{}
	writer := multipart.NewWriter(uploadBody)
	part, err := writer.CreateFormFile("file", "gateway-openai.exe")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("plugin-binary")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.WriteField("sha256", strings.Repeat("d", 64)); err != nil {
		t.Fatalf("write sha field: %v", err)
	}
	if err := writer.WriteField("trust_frontend", "true"); err != nil {
		t.Fatalf("write trust field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	w := invokeMultipartPluginHandler(http.MethodPost, "/plugins/upload", uploadBody, writer.FormDataContentType(), nil, handler.UploadPlugin)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if manager.installedName != "gateway-openai" || manager.installedHash != strings.Repeat("d", 64) || manager.installedSize == 0 {
		t.Fatalf("upload delegation manager=%+v", manager)
	}

	untrustedBody := &bytes.Buffer{}
	untrustedWriter := multipart.NewWriter(untrustedBody)
	untrustedPart, err := untrustedWriter.CreateFormFile("file", "gateway-openai.exe")
	if err != nil {
		t.Fatalf("create untrusted multipart file: %v", err)
	}
	if _, err := untrustedPart.Write([]byte("plugin-binary")); err != nil {
		t.Fatalf("write untrusted multipart file: %v", err)
	}
	if err := untrustedWriter.Close(); err != nil {
		t.Fatalf("close untrusted multipart writer: %v", err)
	}
	w = invokeMultipartPluginHandler(http.MethodPost, "/plugins/upload", untrustedBody, untrustedWriter.FormDataContentType(), nil, handler.UploadPlugin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("untrusted upload status = %d body=%s", w.Code, w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodPost, "/plugins/github", `{"repo":"DevilGenius/airgate-openai"}`, nil, nil, handler.InstallFromGithub)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("untrusted github status = %d body=%s", w.Code, w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodPost, "/plugins/gateway-openai/proxy/admin/ping?dry_run=true", "payload", gin.Params{
		{Key: "name", Value: "gateway-openai"},
		{Key: "action", Value: "/admin/ping"},
	}, nil, handler.ProxyRequest)
	if w.Code != http.StatusNotFound {
		t.Fatalf("proxy unavailable status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestPluginHandlerErrorRoutesWithFakeAdminService(t *testing.T) {
	wantErr := errors.New("plugin failed")
	manager := &pluginHandlerManagerStub{
		configErr:        wantErr,
		updateErr:        wantErr,
		installGithubErr: wantErr,
		uninstallErr:     wantErr,
		reloadDevErr:     wantErr,
		isDev:            true,
	}
	marketplace := &pluginHandlerMarketplaceStub{syncErr: wantErr, listErr: wantErr}
	handler := NewPluginHandler(pluginadmin.NewService(manager, marketplace))

	errorCases := []struct {
		name   string
		method string
		target string
		body   string
		params gin.Params
		fn     func(*gin.Context)
		status int
	}{
		{name: "get config error", method: http.MethodGet, target: "/plugins/p/config", params: gin.Params{{Key: "name", Value: "p"}}, fn: handler.GetPluginConfig, status: http.StatusInternalServerError},
		{name: "update config error", method: http.MethodPut, target: "/plugins/p/config", params: gin.Params{{Key: "name", Value: "p"}}, body: `{"config":{}}`, fn: handler.UpdatePluginConfig, status: http.StatusInternalServerError},
		{name: "install github error", method: http.MethodPost, target: "/plugins/github", body: `{"repo":"owner/repo","trust_frontend":true}`, fn: handler.InstallFromGithub, status: http.StatusInternalServerError},
		{name: "uninstall error", method: http.MethodDelete, target: "/plugins/p", params: gin.Params{{Key: "name", Value: "p"}}, fn: handler.UninstallPlugin, status: http.StatusInternalServerError},
		{name: "reload error", method: http.MethodPost, target: "/plugins/p/reload", params: gin.Params{{Key: "name", Value: "p"}}, fn: handler.ReloadPlugin, status: http.StatusInternalServerError},
		{name: "refresh marketplace error", method: http.MethodPost, target: "/plugins/marketplace/refresh", fn: handler.RefreshMarketplace, status: http.StatusInternalServerError},
		{name: "list marketplace error", method: http.MethodGet, target: "/plugins/marketplace", fn: handler.ListMarketplace, status: http.StatusInternalServerError},
	}
	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			w := invokeHandlerForValidation(tt.method, tt.target, tt.body, tt.params, nil, tt.fn)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	nonDevHandler := NewPluginHandler(pluginadmin.NewService(&pluginHandlerManagerStub{}, nil))
	w := invokeHandlerForValidation(http.MethodPost, "/plugins/p/reload", "", gin.Params{{Key: "name", Value: "p"}}, nil, nonDevHandler.ReloadPlugin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-dev reload status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestUserGroupRateOverrideRoutesWithSQLite(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenMemoryEnt(t, "handler_user_group_rate_routes", schema.WithGlobalUniqueID(false))
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	}()

	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userEnt, err := db.User.Create().
		SetEmail("group-rate@example.com").
		SetPasswordHash(string(hash)).
		SetUsername("rate-user").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	groupEnt, err := db.Group.Create().
		SetName("Rate Group").
		SetPlatform("openai").
		SetSubscriptionType("standard").
		Save(ctx)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	handler := NewUserHandler(appuser.NewService(store.NewUserStore(db)), nil, nil)
	params := gin.Params{
		{Key: "id", Value: "1"},
		{Key: "userId", Value: "1"},
	}
	params[0].Value = intToString(groupEnt.ID)
	params[1].Value = intToString(userEnt.ID)

	w := invokeHandlerForValidation(http.MethodPut, "/groups/1/rate-overrides/1", `{"rate":1.25}`, params, nil, handler.SetGroupRateOverride)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"rate":1.25`) || !strings.Contains(w.Body.String(), "group-rate@example.com") {
		t.Fatalf("set group rate body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/groups/1/rate-overrides", "", gin.Params{{Key: "id", Value: intToString(groupEnt.ID)}}, nil, handler.ListGroupRateOverrides)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"rate":1.25`) {
		t.Fatalf("list group rate body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodDelete, "/groups/1/rate-overrides/1", "", params, nil, handler.DeleteGroupRateOverride)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))

	w = invokeHandlerForValidation(http.MethodGet, "/groups/1/rate-overrides", "", gin.Params{{Key: "id", Value: intToString(groupEnt.ID)}}, nil, handler.ListGroupRateOverrides)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if strings.Contains(w.Body.String(), `"rate":1.25`) {
		t.Fatalf("rate override was not deleted: %s", w.Body.String())
	}
}

func TestUpgradeHandlerInfoAndStatus(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = handlerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Fatalf("unexpected GitHub request: method=%s accept=%q", req.Method, req.Header.Get("Accept"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v999.0.0","html_url":"https://example.test/release","body":"notes"}`)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	handler := NewUpgradeHandler(upgrade.NewService(upgrade.ModeDocker, nil))

	w := invokeHandlerForValidation(http.MethodGet, "/upgrade/status", "", nil, nil, handler.GetStatus)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"state":"idle"`) {
		t.Fatalf("status body = %s", w.Body.String())
	}

	w = invokeHandlerForValidation(http.MethodGet, "/upgrade/info", "", nil, nil, handler.GetInfo)
	requireOKResponse(t, asResponseView(w.Code, w.Body.String()))
	if !strings.Contains(w.Body.String(), `"mode":"docker"`) || !strings.Contains(w.Body.String(), `"latest":"v999.0.0"`) {
		t.Fatalf("info body = %s", w.Body.String())
	}
}

func intToString(id int) string {
	return strconv.Itoa(id)
}

func invokeMultipartPluginHandler(method, target string, body *bytes.Buffer, contentType string, params gin.Params, fn func(*gin.Context)) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = params
	c.Request = httptest.NewRequest(method, target, body)
	c.Request.Header.Set("Content-Type", contentType)
	fn(c)
	return w
}

type handlerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f handlerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type pluginHandlerManagerStub struct {
	allMeta          []plugin.PluginMeta
	config           map[string]string
	updatedConfig    map[string]string
	loading          bool
	isDev            bool
	reloadedInstance bool
	githubRepo       string
	githubVersion    string
	uninstalled      string
	reloadedDev      string
	installedName    string
	installedHash    string
	installedSize    int
	configErr        error
	updateErr        error
	installGithubErr error
	uninstallErr     error
	reloadDevErr     error
	instance         *plugin.PluginInstance
}

func (s *pluginHandlerManagerStub) GetAllPluginMeta() []plugin.PluginMeta {
	return append([]plugin.PluginMeta(nil), s.allMeta...)
}

func (s *pluginHandlerManagerStub) InstallFromBinary(context.Context, string, []byte) error {
	return nil
}

func (s *pluginHandlerManagerStub) InstallFromBinaryWithSHA256(_ context.Context, name string, binary []byte, hash string) error {
	s.installedName = name
	s.installedHash = hash
	s.installedSize = len(binary)
	return nil
}

func (s *pluginHandlerManagerStub) InstallFromGithub(_ context.Context, repo, version string) error {
	s.githubRepo = repo
	s.githubVersion = version
	return s.installGithubErr
}

func (s *pluginHandlerManagerStub) Uninstall(_ context.Context, name string) error {
	s.uninstalled = name
	return s.uninstallErr
}

func (s *pluginHandlerManagerStub) ReloadDev(_ context.Context, name string) error {
	s.reloadedDev = name
	return s.reloadDevErr
}

func (s *pluginHandlerManagerStub) ReloadInstance(context.Context, string) error {
	s.reloadedInstance = true
	return nil
}

func (s *pluginHandlerManagerStub) IsDev(string) bool {
	return s.isDev
}

func (s *pluginHandlerManagerStub) IsLoading() bool {
	return s.loading
}

func (s *pluginHandlerManagerStub) GetInstance(string) *plugin.PluginInstance {
	return s.instance
}

func (s *pluginHandlerManagerStub) GetPluginConfig(context.Context, string) (map[string]string, error) {
	if s.configErr != nil {
		return nil, s.configErr
	}
	return s.config, nil
}

func (s *pluginHandlerManagerStub) UpdatePluginConfig(_ context.Context, _ string, config map[string]string) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updatedConfig = config
	return nil
}

type pluginHandlerMarketplaceStub struct {
	available []plugin.MarketplacePlugin
	synced    bool
	syncErr   error
	listErr   error
}

func (s *pluginHandlerMarketplaceStub) ListAvailable(context.Context) ([]plugin.MarketplacePlugin, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]plugin.MarketplacePlugin(nil), s.available...), nil
}

func (s *pluginHandlerMarketplaceStub) SyncFromGithub(context.Context) error {
	s.synced = true
	return s.syncErr
}
