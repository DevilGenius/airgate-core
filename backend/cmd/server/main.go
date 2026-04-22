package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	sdk "github.com/DouDOU-start/airgate-sdk"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	"github.com/DouDOU-start/airgate-core/internal/bootstrap"
	"github.com/DouDOU-start/airgate-core/internal/config"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/server"
	"github.com/DouDOU-start/airgate-core/internal/setup"
	"github.com/DouDOU-start/airgate-core/internal/version"
	webfs "github.com/DouDOU-start/airgate-core/internal/web"
)

func main() {
	// CLI flags ------------------------------------------------------------
	// 仅声明少量必要 flag，避免 cobra 之类的额外依赖；其余配置项继续走
	// 配置文件 + 环境变量两条腿。
	var (
		showVersion bool
		configPath  string
	)
	flag.BoolVar(&showVersion, "version", false, "打印版本号并退出")
	flag.StringVar(&configPath, "config", "", "配置文件路径，默认为环境变量 CONFIG_PATH 或 ./config.yaml")
	flag.Parse()

	if showVersion {
		fmt.Printf("airgate-core %s %s/%s\n", version.Version, runtime.GOOS, runtime.GOARCH)
		return
	}

	// 如果 --config 提供了路径，把它写回环境变量，让后续 config.ConfigPath() 看到
	if configPath != "" {
		_ = os.Setenv("CONFIG_PATH", configPath)
	}

	// 默认初始化日志（配置加载前先用默认值）
	sdk.InitLogger("core", "info", "text")
	slog.Info("AirGate Core 启动中...", "version", version.Version, "sdk_version", sdk.SDKVersion)

	// 加载国际化（翻译文件已 //go:embed 进二进制）
	_ = i18n.LoadEmbedded()

	// 检查是否需要安装
	if setup.NeedsSetup() {
		slog.Info("系统未安装，启动安装向导...")
		startSetupServer()
		// 安装完成后继续往下执行，启动正常服务
		slog.Info("安装完成，启动主服务...")
	}

	// 加载配置
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		slog.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	// 用配置值重新初始化日志（应用配置文件中的 level/format）
	sdk.InitLogger("core", cfg.Log.Level, cfg.Log.Format)

	// 启动正常服务
	startMainServer(cfg)
}

// startSetupServer 启动安装向导服务器，安装完成后自动关闭
func startSetupServer() {
	r := gin.Default()

	// 用于通知安装完成
	done := make(chan struct{})
	setup.RegisterRoutesWithCallback(r, func() {
		close(done)
	})

	// 静态文件服务（前端 SPA 来自嵌入资源）
	distFS, err := webfs.FS()
	if err != nil {
		slog.Error("加载嵌入前端失败，安装向导无法启动", "error", err)
		os.Exit(1)
	}
	indexHTML, _ := webfs.IndexHTML()
	assetsFS, err := fs.Sub(distFS, "assets")
	if err != nil {
		slog.Error("嵌入前端缺少 assets 子目录", "error", err)
		os.Exit(1)
	}
	r.StaticFS("/assets", http.FS(assetsFS))
	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
	r.NoRoute(func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})

	host := config.GetHost()
	port := config.GetPort()
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", host, port), Handler: r}

	slog.Info("安装向导服务器启动", "host", host, "port", port)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("安装向导启动失败", "error", err)
			os.Exit(1)
		}
	}()

	// 等待安装完成
	<-done
	slog.Info("安装完成，关闭安装向导服务器...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// startMainServer 启动主服务器
func startMainServer(cfg *config.Config) {
	// 初始化数据库连接（Ent Client）
	drv, err := sql.Open(dialect.Postgres, cfg.Database.DSN())
	if err != nil {
		slog.Error("打开数据库失败", "error", err)
		os.Exit(1)
	}
	// 配置连接池：不限制时 Go 会无限开连接，高并发下 Postgres "too many clients already"
	maxOpen := cfg.Database.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 50
	}
	maxIdle := cfg.Database.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 25
	}
	lifeMin := cfg.Database.ConnMaxLifetimeMinutes
	if lifeMin <= 0 {
		lifeMin = 30
	}
	drv.DB().SetMaxOpenConns(maxOpen)
	drv.DB().SetMaxIdleConns(maxIdle)
	drv.DB().SetConnMaxLifetime(time.Duration(lifeMin) * time.Minute)
	slog.Info("数据库连接池已配置",
		"max_open", maxOpen, "max_idle", maxIdle, "lifetime_min", lifeMin)

	db := ent.NewClient(ent.Driver(drv))
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("关闭数据库连接失败", "error", err)
		}
	}()

	// 启动时执行非破坏性迁移，补齐缺失表和字段，避免升级后因 schema 落后导致接口报错。
	if err := db.Schema.Create(context.Background(), migrate.WithDropIndex(false), migrate.WithDropColumn(false)); err != nil {
		slog.Error("执行数据库迁移失败", "error", err)
		os.Exit(1)
	}

	// 回填历史 API Key 的 key_hint 以及 reseller markup 新列等启动整理任务
	bootstrap.RunStartupTasks(db, drv, cfg.APIKeySecret())

	// 初始化 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer func() {
		if err := rdb.Close(); err != nil {
			slog.Warn("关闭 Redis 连接失败", "error", err)
		}
	}()

	// 创建并启动 HTTP 服务器
	srv := server.NewServer(cfg, db, rdb)

	// 启动插件系统（非阻塞，失败不影响核心服务）
	srv.StartPlugins(context.Background())

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			slog.Error("服务器退出", "error", err)
		}
	}()

	<-quit
	slog.Info("收到关闭信号，开始优雅关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("服务器关闭失败", "error", err)
	}
	slog.Info("服务器已关闭")
}
