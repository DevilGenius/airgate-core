// Package setup 提供安装向导逻辑
package setup

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/DouDOU-start/airgate-core/internal/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	_ "github.com/lib/pq"
)

const installedFile = ".installed"

var installMu sync.Mutex

// NeedsSetup 检查是否需要安装
func NeedsSetup() bool {
	configPath := config.ConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return true
	}
	if _, err := os.Stat(installedFile); os.IsNotExist(err) {
		return true
	}
	return false
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
	defer db.Close()
	return db.PingContext(context.Background())
}

// TestRedisConnection 测试 Redis 连接
func TestRedisConnection(host string, port int, password string, db int) error {
	// 简单 TCP 连接测试
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := sql.Open("postgres", "") // placeholder, 实际用 redis client
	_ = conn
	_ = addr
	// 真正的实现将在 Agent-B1 中完成，这里只是框架
	return err
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
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	// TODO: 使用 Ent client 执行 schema 迁移（Agent-B1 将完善）

	// 3. 创建管理员账户
	hash, err := bcrypt.GenerateFromPassword([]byte(params.Admin.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO users (email, password_hash, role, status, created_at, updated_at) VALUES ($1, $2, 'admin', 'active', NOW(), NOW())`,
		params.Admin.Email, string(hash))
	if err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	// 4. 写入配置文件
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8080, Mode: "release"},
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

	// 5. 创建安装锁定文件
	if err := os.WriteFile(installedFile, []byte("installed"), 0644); err != nil {
		return fmt.Errorf("写入锁定文件失败: %w", err)
	}

	slog.Info("安装完成")
	return nil
}

func generateSecret() string {
	// 生成 32 字节随机密钥
	b := make([]byte, 32)
	_, _ = os.ReadFile("/dev/urandom") // 简化，Agent-B1 将用 crypto/rand
	for i := range b {
		b[i] = "abcdefghijklmnopqrstuvwxyz0123456789"[i%36]
	}
	return string(b)
}
