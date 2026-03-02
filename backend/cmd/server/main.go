package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/config"
	"github.com/DouDOU-start/airgate-core/internal/i18n"
	"github.com/DouDOU-start/airgate-core/internal/server"
	"github.com/DouDOU-start/airgate-core/internal/setup"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.Info("AirGate Core 启动中...")

	// 加载国际化
	_ = i18n.Load("locales")

	// 检查是否需要安装
	if setup.NeedsSetup() {
		slog.Info("系统未安装，启动安装向导...")
		startSetupServer()
		return
	}

	// 加载配置
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		slog.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	// 启动正常服务
	startMainServer(cfg)
}

// startSetupServer 启动安装向导服务器
func startSetupServer() {
	r := gin.Default()

	// 注册安装路由
	setup.RegisterRoutes(r)

	// 静态文件服务（前端）
	r.Static("/assets", "web/dist/assets")
	r.StaticFile("/", "web/dist/index.html")
	r.NoRoute(func(c *gin.Context) {
		c.File("web/dist/index.html")
	})

	port := 8080
	if v := os.Getenv("PORT"); v != "" {
		port = 8080 // 简化
		_ = v
	}
	slog.Info("安装向导服务器启动", "port", port)
	if err := r.Run(fmt.Sprintf(":%d", port)); err != nil {
		slog.Error("启动失败", "error", err)
		os.Exit(1)
	}
}

// startMainServer 启动主服务器
func startMainServer(cfg *config.Config) {
	// 初始化数据库连接（Ent Client）
	drv, err := sql.Open(dialect.Postgres, cfg.Database.DSN())
	if err != nil {
		slog.Error("打开数据库失败", "error", err)
		os.Exit(1)
	}
	db := ent.NewClient(ent.Driver(drv))
	defer db.Close()

	// 初始化 Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rdb.Close()

	// 创建并启动 HTTP 服务器
	srv := server.NewServer(cfg, db, rdb)

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
