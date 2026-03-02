// Package server 提供 HTTP 服务器初始化和生命周期管理
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/internal/auth"
	"github.com/DouDOU-start/airgate-core/internal/config"
	"github.com/DouDOU-start/airgate-core/internal/ratelimit"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Server HTTP 服务器
type Server struct {
	cfg     *config.Config
	db      *ent.Client
	rdb     *redis.Client
	jwtMgr  *auth.JWTManager
	limiter *ratelimit.Limiter
	engine  *gin.Engine
	srv     *http.Server
}

// NewServer 创建 HTTP 服务器
func NewServer(cfg *config.Config, db *ent.Client, rdb *redis.Client) *Server {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	jwtMgr := auth.NewJWTManager(cfg.JWT.Secret, cfg.JWT.ExpireHour)
	limiter := ratelimit.NewLimiter(rdb, ratelimit.DefaultConfig())

	s := &Server{
		cfg:     cfg,
		db:      db,
		rdb:     rdb,
		jwtMgr:  jwtMgr,
		limiter: limiter,
		engine:  gin.Default(),
	}

	// 注册路由
	s.registerRoutes()

	s.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: s.engine,
	}

	return s
}

// Start 启动 HTTP 服务器（阻塞）
func (s *Server) Start() error {
	slog.Info("AirGate Core 服务器启动", "addr", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("正在关闭服务器...")
	return s.srv.Shutdown(ctx)
}
