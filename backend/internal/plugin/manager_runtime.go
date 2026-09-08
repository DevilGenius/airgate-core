package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/DevilGenius/airgate-sdk/protocol/proto"
	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
	pluginent "github.com/DevilGenius/airgate-core/ent/plugin"
	settingent "github.com/DevilGenius/airgate-core/ent/setting"
	"github.com/DevilGenius/airgate-core/internal/dispatchresolver"
)

// pluginGRPCMaxMessageBytes 是与插件之间 gRPC 单条消息的最大字节数（收/发同值）。
// 默认值 4 MB 经常被大段 LLM 响应或翻译后的 SSE 事件击穿，统一抬到 64 MB。
const pluginGRPCMaxMessageBytes = 64 * 1024 * 1024

// pluginStartTimeout 限制插件子进程握手与 Start RPC 的最长耗时，避免坏插件把 core
// 的启动或后台加载协程长期卡死。
const pluginStartTimeout = 30 * time.Second

const pluginRetirementTimeout = 30 * time.Minute

// newPluginClientConfig 构造与插件子进程通信的 go-plugin ClientConfig。
//
// forwardOutput=true 时把插件的 stdout/stderr 透传到 core 自身（用于正常运行的插件），
// false 时丢弃（用于一次性的探测客户端，避免污染日志）。
//
// 抽出这个 helper 是为了让 manager_install.go / manager_runtime.go 共用同一份握手 +
// gRPC 上限配置，避免改一处忘另一处。
//
// hostHandle 参数：
//   - 非 nil 时作为本次 spawn 的 CoreInvokeService 实现，注册到所有 PluginType 的 GRPCPlugin
//     的 CoreInvokeImpl 字段；spawn 后 manager 会调 hostHandle.SetCapabilities 写入权限
//   - nil 时（探测式 spawn / 没装 host service 的部署）走软失败路径，插件 ctx.Host()==nil
func sdkCapabilitiesToStrings(capabilities []sdk.Capability) []string {
	if len(capabilities) == 0 {
		return nil
	}
	out := make([]string, len(capabilities))
	for i, capability := range capabilities {
		out[i] = string(capability)
	}
	return out
}

func (m *Manager) newPluginClientConfig(cmd *exec.Cmd, forwardOutput bool, hostHandle *pluginHostHandle) *goplugin.ClientConfig {
	// hostHandle 通过 PluginSet 注入到 GatewayGRPCPlugin / ExtensionGRPCPlugin / MiddlewareGRPCPlugin。
	// 插件 Dispense 时，sdk-grpc 的 GRPCClient 钩子会通过 GRPCBroker 启一条
	// 反向 stream 注册 CoreInvokeService，把 stream id 通过 InitRequest.host_broker_id
	// 透传到插件子进程。hostHandle 为 nil 时，stream 不创建，插件以软失败方式运行。
	//
	// 注意：MiddlewareGRPCPlugin 也注册到 PluginSet，让 middleware 类型插件可以被
	// dispense（与 gateway/extension 平行）。
	var hostImpl pb.CoreInvokeServiceServer
	if hostHandle != nil {
		hostImpl = hostHandle
	}
	cfg := &goplugin.ClientConfig{
		HandshakeConfig: sdkgrpc.Handshake,
		Plugins: goplugin.PluginSet{
			sdkgrpc.PluginKeyGateway:    &sdkgrpc.GatewayGRPCPlugin{CoreInvokeImpl: hostImpl},
			sdkgrpc.PluginKeyExtension:  &sdkgrpc.ExtensionGRPCPlugin{CoreInvokeImpl: hostImpl},
			sdkgrpc.PluginKeyMiddleware: &sdkgrpc.MiddlewareGRPCPlugin{CoreInvokeImpl: hostImpl},
		},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		GRPCDialOptions: []grpc.DialOption{
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(pluginGRPCMaxMessageBytes),
				grpc.MaxCallSendMsgSize(pluginGRPCMaxMessageBytes),
			),
		},
		StartTimeout: pluginStartTimeout,
	}
	if forwardOutput {
		cfg.SyncStdout = os.Stdout
		cfg.SyncStderr = os.Stderr
	}
	return cfg
}

// Candidate preparation fails if its configuration or database provisioning is
// unavailable; optional isolated storage can remain disabled, as before.
func (m *Manager) buildInitConfig(ctx context.Context, name string) (map[string]interface{}, error) {
	cfg := map[string]interface{}{sdk.ConfigKeyLogLevel: m.logLevel}
	if m.coreDSN != "" {
		cfg["db_dsn"] = m.coreDSN
	}
	if m.pluginDB != nil {
		dsn, err := m.pluginDB.EnsureFor(ctx, name)
		if err != nil {
			var denied *pq.Error
			if !errors.As(err, &denied) || denied.Code != "42501" {
				return nil, err
			}
			// Do not substitute the Core DSN. PluginDSNAware receives an empty
			// DSN; optional private storage keeps its established disabled mode.
			cfg[sdk.PluginDSNConfigKey] = ""
			slog.Warn("plugin_private_storage_disabled", "plugin", name, "reason", "provisioning permission unavailable")
		} else {
			cfg[sdk.PluginDSNConfigKey] = dsn
		}
	}
	if m.db == nil {
		return cfg, nil
	}
	row, err := m.db.Plugin.Query().Where(pluginent.NameEQ(name)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if row != nil {
		for key, value := range row.Config {
			if _, exists := cfg[key]; !exists {
				cfg[key] = value
			}
		}
	}
	if err := m.injectGlobalStorageConfig(ctx, name, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (m *Manager) injectGlobalStorageConfig(ctx context.Context, name string, cfg map[string]interface{}) error {
	items, err := m.db.Setting.Query().Where(settingent.GroupEQ("storage")).All(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		cfg[item.Key] = item.Value
	}
	return nil
}

// Updates serialize per canonical plugin. A retiring process blocks another
// update, so each plugin has at most two live generations.
const pluginBuildTimeout = 3 * time.Minute

var ErrPluginUpdateBusy = errors.New("插件正在更新或旧进程仍在完成请求，请稍后重试")

type pluginUpdate struct {
	done     chan struct{}
	cancel   context.CancelFunc
	previous *PluginInstance
}
type preparedPlugin struct {
	detachPreparation func() bool
	instance          *PluginInstance
	info              sdk.PluginInfo
	models            []sdk.ModelInfo
	routes            []sdk.RouteDefinition
}
type lifecycleClient interface {
	DescribeRuntime(context.Context) (sdk.RuntimeSpec, error)
	BeginDrain(context.Context) error
	ApplyRuntimeSettings(context.Context, map[string]string) error
	HandleRuntimeCallback(context.Context, sdk.CallbackRequest) (sdk.CallbackResponse, error)
	InfoContext(context.Context) (sdk.PluginInfo, error)
	InitContext(context.Context, sdk.PluginContext) error
	Start(context.Context) error
	Stop(context.Context) error
	HealthCheck(context.Context) error
	WebAssetsContext(context.Context) (map[string][]byte, error)
}

func (m *Manager) beginUpdate(parent context.Context, name string) (context.Context, *pluginUpdate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = m.resolveNameLocked(name)
	if !pluginIDPattern.MatchString(name) {
		return nil, nil, errors.New("invalid plugin name")
	}
	if m.closing {
		return nil, nil, errors.New("plugin manager is stopping")
	}
	if m.activeUpdates >= 8 || m.updates[name] != nil || m.retiring[name] != nil {
		return nil, nil, ErrPluginUpdateBusy
	}
	ctx, cancel := context.WithTimeout(parent, pluginBuildTimeout)
	op := &pluginUpdate{done: make(chan struct{}), cancel: cancel, previous: m.instances[name]}
	if m.updates == nil {
		m.updates = make(map[string]*pluginUpdate)
	}
	m.updates[name] = op
	m.activeUpdates++
	return ctx, op, nil
}
func (m *Manager) bindUpdate(op *pluginUpdate, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return errors.New("plugin manager is stopping")
	}
	if op.previous != nil && op.previous.Name != name {
		return errors.New("replacement plugin ID changed")
	}
	if other := m.updates[name]; other != nil && other != op {
		return ErrPluginUpdateBusy
	}
	if m.retiring[name] != nil {
		return ErrPluginUpdateBusy
	}
	if op.previous == nil {
		op.previous = m.instances[name]
	}
	m.updates[name] = op
	return nil
}
func (m *Manager) finishUpdate(op *pluginUpdate) {
	op.cancel()
	m.mu.Lock()
	for key, value := range m.updates {
		if value == op {
			delete(m.updates, key)
		}
	}
	close(op.done)
	m.activeUpdates--
	m.mu.Unlock()
}

// updatePlugin is the single installation/reload pipeline in both environments.
func (m *Manager) updatePlugin(parent context.Context, name string, binary []byte, source string, meta *installMetadata, installed *pluginArtifact) error {
	m.artifactMu.RLock()
	defer m.artifactMu.RUnlock()
	if int64(len(binary)) > MaxPluginBinarySize {
		return pluginBinaryTooLargeError(int64(len(binary)))
	}
	ctx, op, err := m.beginUpdate(parent, name)
	if err != nil {
		return err
	}
	defer m.finishUpdate(op)
	a := installed
	if a == nil {
		a, err = m.prepareArtifact(ctx, binary, source, meta)
		if err != nil {
			return err
		}
	}
	published := false
	defer func() {
		if !published && installed == nil {
			m.removeArtifact(a.Generation)
		}
	}()
	prepCtx, cancel := context.WithTimeout(ctx, pluginStartTimeout)
	defer cancel()
	p, err := m.preparePlugin(prepCtx, op, name, a)
	if err != nil {
		return err
	}
	defer p.detachPreparation()
	defer func() {
		if !published {
			m.stopRuntime(p.instance, context.Background())
		}
	}()
	if installed != nil && p.instance.Name != name {
		return errors.New("installed manifest plugin ID mismatch")
	}
	if err = m.publishPlugin(prepCtx, op, p); err != nil {
		return err
	}
	published = true
	return nil
}
func (m *Manager) preparePlugin(ctx context.Context, op *pluginUpdate, requested string, a *pluginArtifact) (_ *preparedPlugin, err error) {
	var host *pluginHostHandle
	if m.hostFactory != nil {
		host = m.hostFactory.NewPluginHandle(requested)
	}
	client := goplugin.NewClient(m.newPluginClientConfig(exec.Command(m.artifactBinary(a)), true, host))
	var killOnce sync.Once
	kill := func() { killOnce.Do(client.Kill) }
	stopCancel := context.AfterFunc(ctx, kill)
	defer func() {
		if err != nil {
			stopCancel()
			kill()
		}
	}()
	rpc, err := client.Client()
	if err != nil {
		return nil, fmt.Errorf("连接候选插件失败: %w", err)
	}
	raw, err := rpc.Dispense(sdkgrpc.PluginKeyGateway)
	if err != nil {
		return nil, err
	}
	probe := raw.(*sdkgrpc.GatewayGRPCClient)
	info, err := probe.InfoContext(ctx)
	if err != nil {
		return nil, err
	}
	if !pluginIDPattern.MatchString(info.ID) {
		return nil, errors.New("plugin must declare a valid canonical ID")
	}
	if err := m.bindUpdate(op, info.ID); err != nil {
		return nil, err
	}
	if host != nil {
		host.pluginName = info.ID
		caps := make(map[sdk.Capability]bool, len(info.Capabilities))
		for _, cap := range info.Capabilities {
			caps[cap] = true
		}
		host.SetCapabilities(caps)
	}
	inst := &PluginInstance{Name: info.ID, SourceName: requested, Generation: a.Generation, Artifact: a,
		DisplayName: info.Name, Version: info.Version, Author: info.Author, Type: string(info.Type),
		InstructionPresets: info.InstructionPresets, ConfigSchema: cloneConfigSchema(info.ConfigSchema), Metadata: cloneMetadata(info.Metadata),
		Capabilities: sdkCapabilitiesToStrings(info.Capabilities), Priority: info.Priority, Client: client, stopped: make(chan struct{})}
	p := &preparedPlugin{instance: inst, info: info, detachPreparation: stopCancel}
	var lifecycle lifecycleClient
	switch info.Type {
	case sdk.PluginTypeGateway:
		inst.Gateway, lifecycle = probe, probe
	case sdk.PluginTypeExtension:
		raw, err := rpc.Dispense(sdkgrpc.PluginKeyExtension)
		if err != nil {
			return nil, err
		}
		inst.Extension = raw.(*sdkgrpc.ExtensionGRPCClient)
		lifecycle = inst.Extension
	case sdk.PluginTypeMiddleware:
		raw, err := rpc.Dispense(sdkgrpc.PluginKeyMiddleware)
		if err != nil {
			return nil, err
		}
		inst.Middleware = raw.(*sdkgrpc.MiddlewareGRPCClient)
		lifecycle = inst.Middleware
	default:
		return nil, fmt.Errorf("unsupported plugin type %q", info.Type)
	}
	initConfig, err := m.buildInitConfig(ctx, info.ID)
	if err != nil {
		return nil, fmt.Errorf("加载候选配置失败: %w", err)
	}
	inst.runtime = lifecycle
	spec, err := lifecycle.DescribeRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("SDK runtime contract unavailable: %w", err)
	}
	defer func() {
		if err != nil {
			m.releaseRuntimeResources(inst)
		}
	}()
	runtimeInfo, err := m.prepareRuntimeResources(ctx, inst, spec)
	if err != nil {
		return nil, err
	}
	runtimeJSON, err := json.Marshal(runtimeInfo)
	if err != nil {
		return nil, err
	}
	initConfig[sdk.RuntimeContextConfigKey] = string(runtimeJSON)
	if err := lifecycle.InitContext(ctx, newCorePluginContext(initConfig, info.ID)); err != nil {
		return nil, fmt.Errorf("初始化候选插件失败: %w", err)
	}
	if inst.Extension != nil {
		if err := inst.Extension.MigrateContext(ctx); err != nil {
			return nil, err
		}
		inst.backgroundTasks, err = inst.Extension.BackgroundTasksContext(ctx)
		if err != nil {
			return nil, err
		}
	}
	if err := lifecycle.Start(ctx); err != nil {
		return nil, fmt.Errorf("启动候选插件失败: %w", err)
	}
	if err := lifecycle.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("候选插件健康检查失败: %w", err)
	}
	if inst.Gateway != nil {
		inst.Platform, p.models, p.routes, err = inst.Gateway.Catalog(ctx)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(inst.Platform) == "" {
			return nil, errors.New("gateway platform is empty")
		}
		if raw, dispenseErr := rpc.Dispense(sdkgrpc.PluginKeyExtension); dispenseErr == nil {
			ext := raw.(*sdkgrpc.ExtensionGRPCClient)
			types, taskErr := ext.GetTaskTypes(ctx)
			if taskErr == nil && len(types) > 0 {
				inst.Extension = ext
			} else if taskErr != nil && !isOptionalTaskExtensionUnavailable(taskErr) {
				return nil, taskErr
			}
		}
	}
	assets, err := lifecycle.WebAssetsContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := m.prepareGenerationAssets(ctx, inst, assets); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p, nil
}
func (m *Manager) publishPlugin(ctx context.Context, op *pluginUpdate, p *preparedPlugin) error {
	inst, old := p.instance, op.previous
	var prior *pluginArtifact
	if old != nil {
		prior = old.Artifact
	} else {
		prior, _ = m.loadArtifact(inst.Name)
	}
	if old != nil && (old.Type != inst.Type || old.Platform != inst.Platform) {
		return errors.New("replacement cannot change plugin type or platform")
	}
	releaseHash, err := m.prepareRuntimeSettingsForPublish(ctx, inst.runtimeClient())
	if err != nil {
		return err
	}
	defer releaseHash()
	if err := ctx.Err(); err != nil {
		return err
	}
	var older string
	if prior != nil && prior.Generation != inst.Generation {
		inst.Artifact.Previous = prior.Generation
		older = prior.Previous
	}
	if err := m.writeActiveArtifact(inst.Name, inst.Artifact); err != nil {
		if prior != nil {
			_ = m.writeActiveArtifact(inst.Name, prior)
		}
		return err
	}
	m.mu.Lock()
	if m.closing || ctx.Err() != nil || !p.detachPreparation() {
		m.mu.Unlock()
		if prior != nil {
			_ = m.writeActiveArtifact(inst.Name, prior)
		} else {
			_ = os.Remove(m.manifestPath(inst.Name))
		}
		return errors.New("plugin publication canceled")
	}
	inst.owner = m
	m.instances[inst.Name] = inst
	m.registerAliasesLocked(inst.Name, inst.SourceName)
	m.frontendPageCache[inst.Name] = cloneFrontendPages(p.info.FrontendPages)
	if inst.Gateway != nil {
		m.modelCache[inst.Platform], m.routeCache[inst.Name] = cloneModels(p.models), cloneRoutes(p.routes)
		m.accountTypeCache[inst.Platform] = cloneAccountTypes(p.info.AccountTypes)
		delete(m.credCache, inst.Platform)
		if len(p.info.AccountTypes) > 0 {
			m.credCache[inst.Platform] = cloneCredentialFields(p.info.AccountTypes[0].Fields)
		}
		dispatchresolver.RegisterPlatformDSL(inst.Platform, p.info.DispatchDSL)
	}
	if inst.Artifact.SourcePath != "" {
		m.devPaths[inst.Name] = inst.Artifact.SourcePath
	} else {
		delete(m.devPaths, inst.Name)
	}
	var idle <-chan struct{}
	if old != nil {
		idle = old.beginDrain()
		if m.retiring == nil {
			m.retiring = make(map[string]*PluginInstance)
		}
		m.retiring[inst.Name] = old
	}
	m.mu.Unlock()
	if old != nil {
		old.cancelBackground()
		go m.retireGeneration(old, idle)
	}
	if inst.Type == string(sdk.PluginTypeExtension) {
		m.startExtensionBackgroundTasks(inst)
	}
	if inst.Artifact.SourcePath != "" && m.devWatcher != nil {
		m.devWatcher.add(inst.Name, inst.Artifact.SourcePath)
	}
	if older != "" && older != inst.Generation && (old == nil || older != old.Generation) {
		m.removeArtifact(older)
	}
	slog.Info("plugin_generation_activated", "plugin", inst.Name, "generation", inst.Generation, "version", inst.Version)
	return nil
}
func (m *Manager) retireGeneration(inst *PluginInstance, idle <-chan struct{}) {
	m.retireGenerationWithin(inst, idle, pluginRetirementTimeout)
}

func (m *Manager) retireGenerationWithin(inst *PluginInstance, idle <-chan struct{}, limit time.Duration) {
	ctx, cancel := context.WithTimeout(m.runtimeCtx, limit)
	defer cancel()
	m.beginRuntimeDrain(inst, ctx)
	select {
	case <-idle:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			slog.Warn("plugin_retirement_timeout", "plugin", inst.Name, "active_requests", inst.activeRequestCount(), "timeout", limit)
		}
	}
	m.stopRuntime(inst, ctx)
	m.mu.Lock()
	if m.retiring[inst.Name] == inst {
		delete(m.retiring, inst.Name)
	}
	m.mu.Unlock()
}
func (m *Manager) stopRuntime(inst *PluginInstance, parent context.Context) {
	if inst == nil {
		return
	}
	inst.stopOnce.Do(func() {
		m.beginRuntimeDrain(inst, parent)
		defer m.releaseRuntimeResources(inst)
		inst.cancelBackground()
		ctx, cancel := context.WithTimeout(parent, 10*time.Second)
		defer cancel()
		var client lifecycleClient
		switch {
		case inst.Gateway != nil:
			client = inst.Gateway
		case inst.Extension != nil:
			client = inst.Extension
		case inst.Middleware != nil:
			client = inst.Middleware
		}
		if client != nil {
			if err := client.Stop(ctx); err != nil {
				slog.Warn("plugin_stop_failed", "plugin", inst.Name, "error", err)
			}
		}
		if inst.Client != nil {
			inst.Client.Kill()
		}
		ttCache.remove(inst.Name + ":" + inst.Generation)
		if inst.stopped != nil {
			close(inst.stopped)
		}
		slog.Info("plugin_generation_stopped", "plugin", inst.Name, "generation", inst.Generation)
	})
}
func (m *Manager) stopPlugin(name string, parents ...context.Context) {
	ctx := context.Background()
	if len(parents) > 0 {
		ctx = parents[0]
	}
	m.mu.Lock()
	name = m.resolveNameLocked(name)
	inst := m.instances[name]
	if inst == nil {
		m.mu.Unlock()
		return
	}
	idle := inst.beginDrain()
	delete(m.instances, name)
	delete(m.modelCache, inst.Platform)
	delete(m.routeCache, name)
	delete(m.credCache, inst.Platform)
	delete(m.accountTypeCache, inst.Platform)
	delete(m.frontendPageCache, name)
	m.unregisterAliasesLocked(name, inst.SourceName)
	if inst.Platform != "" {
		dispatchresolver.UnregisterPlatformDSL(inst.Platform)
	}
	m.mu.Unlock()
	if m.devWatcher != nil {
		m.devWatcher.remove(name)
	}
	inst.cancelBackground()
	m.beginRuntimeDrain(inst, ctx)
	select {
	case <-idle:
	case <-ctx.Done():
	}
	m.stopRuntime(inst, ctx)
}
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	m.closing = true
	operations := make(map[*pluginUpdate]bool)
	for _, op := range m.updates {
		operations[op] = true
		op.cancel()
	}
	all := make(map[*PluginInstance]bool)
	for _, inst := range m.instances {
		inst.beginDrain()
		all[inst] = true
	}
	for _, inst := range m.retiring {
		all[inst] = true
	}
	if m.runtimeCancel != nil {
		m.runtimeCancel()
	}
	m.mu.Unlock()
	if m.devWatcher != nil {
		_ = m.devWatcher.CloseContext(ctx)
	}
	var wg sync.WaitGroup
	for inst := range all {
		wg.Add(1)
		go func() { defer wg.Done(); m.stopRuntime(inst, ctx) }()
	}
	for op := range operations {
		select {
		case <-op.done:
		case <-ctx.Done():
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
func (m *Manager) LoadAll(ctx context.Context) error {
	defer m.pruneArtifacts()
	entries, err := os.ReadDir(m.pluginDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取插件目录失败: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !pluginIDPattern.MatchString(entry.Name()) {
			continue
		}
		a, err := m.loadArtifact(entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			slog.Error("plugin_manifest_invalid", "plugin", entry.Name(), "error", err)
			continue
		}
		if a.SourcePath != "" {
			continue
		}
		if err := m.updatePlugin(ctx, entry.Name(), nil, "", nil, a); err != nil {
			slog.Error("plugin_load_failed", "plugin", entry.Name(), "error", err)
		}
	}
	return nil
}
func (m *Manager) LoadDev(ctx context.Context, name, source string) error {
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		return fmt.Errorf("plugin source directory unavailable")
	}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return errors.New("plugin manager is stopping")
	}
	name = m.resolveNameLocked(name)
	m.devPaths[name] = source
	m.mu.Unlock()
	if m.devWatcher != nil {
		m.devWatcher.add(name, source)
	}
	return m.updatePlugin(ctx, name, nil, source, nil, nil)
}
func (m *Manager) ReloadDev(ctx context.Context, name string) error {
	m.mu.RLock()
	name = m.resolveNameLocked(name)
	source := m.devPaths[name]
	m.mu.RUnlock()
	if source == "" {
		return errors.New("plugin has no development source")
	}
	return m.updatePlugin(ctx, name, nil, source, nil, nil)
}
func (m *Manager) IsDev(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.devPaths[m.resolveNameLocked(name)] != ""
}
func isOptionalTaskExtensionUnavailable(err error) bool {
	return status.Code(err) == codes.Unimplemented
}
