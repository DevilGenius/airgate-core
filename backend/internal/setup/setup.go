// Package setup 提供安装向导逻辑
package setup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/migrate"
	"github.com/DouDOU-start/airgate-core/internal/config"

	_ "github.com/lib/pq"
)

var installMu sync.Mutex

// EnvDBConfig 从环境变量解析数据库配置。
// 当 DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME 全部存在时返回非 nil；
// 任意一项缺失返回 nil（用户需要走 wizard 手填）。
//
// 用途：docker compose 部署时 env 已经提供了完整连接信息，wizard 不应再问一遍。
func EnvDBConfig() *config.DatabaseConfig {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	portStr := os.Getenv("DB_PORT")
	if host == "" || user == "" || password == "" || dbname == "" || portStr == "" {
		return nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}
	sslmode := os.Getenv("DB_SSLMODE")
	if sslmode == "" {
		sslmode = "disable"
	}
	return &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		DBName:   dbname,
		SSLMode:  sslmode,
	}
}

// EnvRedisConfig 从环境变量解析 Redis 配置。
// 当 REDIS_HOST/REDIS_PORT/REDIS_PASSWORD 全部存在时返回非 nil；
// 没有密码或缺主机视为不可用，wizard 仍需手填。
func EnvRedisConfig() *config.RedisConfig {
	host := os.Getenv("REDIS_HOST")
	password := os.Getenv("REDIS_PASSWORD")
	portStr := os.Getenv("REDIS_PORT")
	if host == "" || password == "" || portStr == "" {
		return nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil
	}
	dbNum := 0
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			dbNum = n
		}
	}
	return &config.RedisConfig{
		Host:     host,
		Port:     port,
		Password: password,
		DB:       dbNum,
	}
}

// NeedsSetup 检查是否需要安装。
// 判断逻辑：config.yaml 不存在 → 需要安装；
// config.yaml 存在则尝试连接数据库，查询是否已有管理员账户。
func NeedsSetup() bool {
	configPath := config.ConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return true
	}

	// config.yaml 存在，尝试加载并连接数据库确认是否已初始化
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Warn("加载配置文件失败，进入安装向导", "error", err)
		return true
	}

	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		slog.Warn("打开数据库失败，进入安装向导", "error", err)
		return true
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		slog.Warn("数据库连接失败，进入安装向导", "error", err)
		return true
	}

	// 查询 users 表是否存在管理员记录
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	if err != nil {
		// 表不存在或查询失败，视为未安装
		slog.Warn("查询管理员记录失败，进入安装向导", "error", err)
		return true
	}

	return count == 0
}

// TestDBConnection 测试数据库连接
func TestDBConnection(host string, port int, user, password, dbname, sslmode string) error {
	if sslmode == "" {
		sslmode = "disable"
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("关闭测试数据库连接失败", "error", err)
		}
	}()
	return db.PingContext(context.Background())
}

// TestRedisConnection 测试 Redis 连接
func TestRedisConnection(host string, port int, password string, db int) error {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: password,
		DB:       db,
	})
	defer func() {
		if err := rdb.Close(); err != nil {
			slog.Warn("关闭测试 Redis 连接失败", "error", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return rdb.Ping(ctx).Err()
}

// InstallParams 安装参数
type InstallParams struct {
	DB    config.DatabaseConfig
	Redis config.RedisConfig
	Admin struct {
		Email    string
		Password string
	}
}

// Install 执行安装
func Install(params InstallParams) error {
	installMu.Lock()
	defer installMu.Unlock()

	if !NeedsSetup() {
		return fmt.Errorf("系统已安装")
	}

	slog.Info("开始安装...")

	// 1. 测试数据库连接
	if err := TestDBConnection(params.DB.Host, params.DB.Port, params.DB.User, params.DB.Password, params.DB.DBName, params.DB.SSLMode); err != nil {
		return fmt.Errorf("数据库连接失败: %w", err)
	}

	// 2. 连接数据库，运行 Ent 迁移
	dsn := params.DB.DSN()
	drv, err := entsql.Open(dialect.Postgres, dsn)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	client := ent.NewClient(ent.Driver(drv))
	defer func() {
		if err := client.Close(); err != nil {
			slog.Warn("关闭安装数据库客户端失败", "error", err)
		}
	}()

	slog.Info("正在执行数据库迁移...")
	if err := client.Schema.Create(context.Background(),
		migrate.WithDropIndex(false),
		migrate.WithDropColumn(false),
	); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	slog.Info("数据库迁移完成")

	// 3. 创建管理员账户
	hash, err := bcrypt.GenerateFromPassword([]byte(params.Admin.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}
	_, err = client.User.Create().
		SetEmail(params.Admin.Email).
		SetPasswordHash(string(hash)).
		SetRole("admin").
		SetStatus("active").
		Save(context.Background())
	if err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	// 4. 写入配置文件
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: config.GetPort(), Mode: "release"},
		Database: params.DB,
		Redis:    params.Redis,
		JWT:      config.JWTConfig{Secret: generateSecret(), ExpireHour: 24},
	}
	cfgData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(config.ConfigPath(), cfgData, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	slog.Info("安装完成")
	return nil
}

func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下的回退
		return "airgate-default-secret-change-me"
	}
	return hex.EncodeToString(b)
}
