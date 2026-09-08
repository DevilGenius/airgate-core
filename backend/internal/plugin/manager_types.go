// Package plugin 提供插件生命周期管理、市场和请求转发。
package plugin

import (
	"context"
	"errors"
	"strings"
	"sync"

	goplugin "github.com/hashicorp/go-plugin"

	sdkgrpc "github.com/DevilGenius/airgate-sdk/runtimego/grpc"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"

	"github.com/DevilGenius/airgate-core/ent"
)

// PluginInstance 运行中的插件实例。
type PluginInstance struct {
	Name               string
	SourceName         string
	Generation         string
	Artifact           *pluginArtifact
	owner              *Manager
	stopOnce           sync.Once
	drainOnce          sync.Once
	stopped            chan struct{}
	backgroundTasks    []sdk.BackgroundTask
	frontendAssets     map[string]bool
	runtime            lifecycleClient
	callbacks          []*runtimeCallbackResource
	DisplayName        string
	Version            string
	Author             string
	Platform           string
	Type               string // "gateway", "extension", "middleware"
	InstructionPresets []string
	ConfigSchema       []sdk.ConfigField
	Metadata           map[string]string
	Capabilities       []string // 插件声明的 host capability 列表（仅展示用）
	Priority           int32    // 仅对 type=middleware 生效，决定 chain 顺序

	Client     *goplugin.Client
	Gateway    *sdkgrpc.GatewayGRPCClient
	Extension  *sdkgrpc.ExtensionGRPCClient
	Middleware *sdkgrpc.MiddlewareGRPCClient

	// 后台任务调度上下文。stopBackground 由 Core 调度器创建，用于停止
	// 该插件实例的所有后台任务 goroutine。stopPlugin 时调用。
	stopBackground context.CancelFunc

	lifecycleMu    sync.Mutex
	activeRequests int
	draining       bool
	idleCh         chan struct{}
}

var errPluginInstanceDraining = errors.New("plugin instance is reloading or stopping")

func (inst *PluginInstance) Forward(ctx context.Context, req *sdk.ForwardRequest) (sdk.ForwardOutcome, error) {
	current, release, err := inst.Acquire()
	if err != nil {
		return sdk.ForwardOutcome{}, err
	}
	defer release()
	if current.Gateway == nil {
		return sdk.ForwardOutcome{}, errors.New("plugin gateway is not available")
	}
	return current.Gateway.Forward(ctx, req)
}

func (inst *PluginInstance) acquireRequest() bool {
	if inst == nil {
		return false
	}
	inst.lifecycleMu.Lock()
	defer inst.lifecycleMu.Unlock()
	if inst.draining {
		return false
	}
	inst.activeRequests++
	return true
}

func (inst *PluginInstance) releaseRequest() {
	if inst == nil {
		return
	}
	inst.lifecycleMu.Lock()
	defer inst.lifecycleMu.Unlock()
	if inst.activeRequests > 0 {
		inst.activeRequests--
	}
	if inst.draining && inst.activeRequests == 0 && inst.idleCh != nil {
		close(inst.idleCh)
		inst.idleCh = nil
	}
}

func (inst *PluginInstance) beginDrain() <-chan struct{} {
	closed := make(chan struct{})
	close(closed)
	if inst == nil {
		return closed
	}
	inst.lifecycleMu.Lock()
	defer inst.lifecycleMu.Unlock()
	inst.draining = true
	if inst.activeRequests == 0 {
		return closed
	}
	if inst.idleCh == nil {
		inst.idleCh = make(chan struct{})
	}
	return inst.idleCh
}

func (inst *PluginInstance) activeRequestCount() int {
	if inst == nil {
		return 0
	}
	inst.lifecycleMu.Lock()
	defer inst.lifecycleMu.Unlock()
	return inst.activeRequests
}

// Manager 插件管理器。
type Manager struct {
	pluginDir string
	logLevel  string
	coreDSN   string      // core 数据库 DSN，启动插件时自动注入到 Init Config 的 db_dsn 字段
	db        *ent.Client // 用于读取/持久化插件配置

	// hostFactory 是 Core 暴露给插件的 HostService 工厂。每个插件 spawn 时会从中
	// 派生一个独立的 *pluginHostHandle，做 per-plugin 的 capability 隔离。
	// 由 SetHostService 注入；nil 时插件 ctx.Host()==nil（软失败模式）。
	hostFactory *HostService

	// pluginDB 给每个插件 provision 独立 schema + 受限 role + plugin_dsn。
	// 详见 ADR-0001 Decision 5。nil 时不做 provisioning（仍然可以正常加载插件，
	// 只是它们拿不到 plugin_dsn，必须用旧的 db_dsn）。
	pluginDB *pluginDSNProvisioner

	// devWatcher 监听 dev 模式插件源码目录的 .go 改动，自动 ReloadDev。
	// 实现是 mtime 轮询（不是 fsnotify），原因见 dev_watcher.go 顶部注释。
	devWatcher *devWatcher

	mu             sync.RWMutex
	instances      map[string]*PluginInstance
	loading        bool
	closing        bool
	runtimeCtx     context.Context
	runtimeCancel  context.CancelFunc
	updates        map[string]*pluginUpdate
	activeUpdates  int
	artifactMu     sync.RWMutex
	retiring       map[string]*PluginInstance
	runtimeState   runtimeStateStore
	callbackMu     sync.Mutex
	callbacks      map[runtimeCallbackKey]*runtimeCallbackResource
	taskPoolOnce   sync.Once
	taskPool       *taskWorkerPool
	taskDispatchMu sync.Mutex
	aliases        map[string]string
	devPaths       map[string]string

	modelCache        map[string][]sdk.ModelInfo
	routeCache        map[string][]sdk.RouteDefinition
	credCache         map[string][]sdk.CredentialField
	accountTypeCache  map[string][]sdk.AccountType
	frontendPageCache map[string][]sdk.FrontendPage

	runtimeHashMu         sync.RWMutex
	runtimeHashState      RuntimeHashState
	runtimeHashConfigured bool
}

// SetHostService 注入 Core 实现的 HostService 工厂。
//
// 必须在 Manager 加载任何插件之前（即 server 启动时）调用，否则启动较早的插件
// 会拿到 host_broker_id=0，需要重启才能恢复 host 通路。
func (m *Manager) SetHostService(factory *HostService) {
	m.hostFactory = factory
}

// SetLoading 记录启动阶段插件是否仍在后台加载。
func (m *Manager) SetLoading(loading bool) {
	m.mu.Lock()
	m.loading = loading
	m.mu.Unlock()
}

// IsLoading 返回启动阶段插件后台加载状态。
func (m *Manager) IsLoading() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loading
}

// PluginMeta 插件运行时元信息。
type PluginMeta struct {
	Generation         string
	UpdateState        string
	DrainingRequests   int
	Name               string
	DisplayName        string
	Version            string
	Author             string
	Type               string
	Platform           string
	AccountTypes       []sdk.AccountType
	FrontendPages      []sdk.FrontendPage
	InstructionPresets []string
	ConfigSchema       []sdk.ConfigField
	Metadata           map[string]string
	Config             map[string]string
	HasWebAssets       bool
	IsDev              bool
	BinarySHA256       string
	CommitSHA          string
}

// NewManager 创建插件管理器。
//
// 插件要调用 core 能力一律通过 HostService（hashicorp/go-plugin GRPCBroker 反向 gRPC），
// 由 SetHostService 注入。因此这里不再需要 coreBaseURL / apiKeySecret 参数：插件不再
// 走 HTTP + admin key 回调 core。
func NewManager(pluginDir, logLevel, coreDSN string, db *ent.Client) *Manager {
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	m := &Manager{
		runtimeCtx:            runtimeCtx,
		runtimeCancel:         runtimeCancel,
		updates:               make(map[string]*pluginUpdate),
		retiring:              make(map[string]*PluginInstance),
		pluginDir:             pluginDir,
		logLevel:              logLevel,
		coreDSN:               coreDSN,
		db:                    db,
		instances:             make(map[string]*PluginInstance),
		aliases:               make(map[string]string),
		devPaths:              make(map[string]string),
		modelCache:            make(map[string][]sdk.ModelInfo),
		routeCache:            make(map[string][]sdk.RouteDefinition),
		credCache:             make(map[string][]sdk.CredentialField),
		accountTypeCache:      make(map[string][]sdk.AccountType),
		frontendPageCache:     make(map[string][]sdk.FrontendPage),
		runtimeHashState:      DefaultRuntimeHashState(),
		runtimeHashConfigured: true,
	}
	if coreDSN != "" && db != nil {
		m.pluginDB = newPluginDSNProvisioner(db, coreDSN)
	}
	m.devWatcher = newDevWatcher(m)
	return m
}

func normalizePluginName(name string) string {
	return strings.TrimSpace(name)
}
