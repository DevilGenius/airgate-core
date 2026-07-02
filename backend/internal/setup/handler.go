package setup

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/config"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

// 安装完成回调
var onInstallDone func()
var setupInstallCallbackDelay = 500 * time.Millisecond

// RegisterRoutes 注册安装向导路由（无回调）
func RegisterRoutes(r *gin.Engine) {
	RegisterRoutesWithCallback(r, nil)
}

// RegisterRoutesWithCallback 注册安装向导路由，安装成功后触发回调
func RegisterRoutesWithCallback(r *gin.Engine, callback func()) {
	onInstallDone = callback
	setup := r.Group("/setup")
	{
		setup.GET("/status", handleStatus)
		setup.Use(setupGuard())
		setup.POST("/test-db", handleTestDB)
		setup.POST("/test-redis", handleTestRedis)
		setup.POST("/install", handleInstall)
	}
}

// setupGuard 安装保护中间件：安装完成后禁止访问
func setupGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !setupNeedsSetup() {
			response.Error(c, http.StatusForbidden, 403, "系统已安装")
			c.Abort()
			return
		}
		c.Next()
	}
}

func handleStatus(c *gin.Context) {
	resp := dto.SetupStatusResp{
		NeedsSetup: setupNeedsSetup(),
	}
	// 当 docker compose 之类的部署已经通过环境变量提供了完整的 DB / Redis 连接信息，
	// 并且这些信息实际可连通时，把"提示"挂在响应里 —— 前端据此跳过对应配置步骤。
	// 注意：密码字段一律不返回，避免 wizard 页面在浏览器里读出明文。
	if envDB := setupEnvDBConfig(); envDB != nil {
		if err := setupTestDBConnection(envDB.Host, envDB.Port, envDB.User, envDB.Password, envDB.DBName, envDB.SSLMode); err == nil {
			resp.EnvDB = &dto.EnvDBHint{
				Host:    envDB.Host,
				Port:    envDB.Port,
				User:    envDB.User,
				DBName:  envDB.DBName,
				SSLMode: envDB.SSLMode,
			}
		}
	}
	if envRedis := setupEnvRedisConfig(); envRedis != nil {
		if err := setupTestRedisConnection(envRedis.Host, envRedis.Port, envRedis.Password, envRedis.DB, envRedis.TLS); err == nil {
			resp.EnvRedis = &dto.EnvRedisHint{
				Host: envRedis.Host,
				Port: envRedis.Port,
				DB:   envRedis.DB,
				TLS:  envRedis.TLS,
			}
		}
	}
	response.Success(c, resp)
}

func handleTestDB(c *gin.Context) {
	var req dto.TestDBReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	err := setupTestDBConnection(req.Host, req.Port, req.User, req.Password, req.DBName, req.SSLMode)
	if err != nil {
		response.Success(c, dto.TestConnectionResp{Success: false, ErrorMsg: err.Error()})
		return
	}
	response.Success(c, dto.TestConnectionResp{Success: true})
}

func handleTestRedis(c *gin.Context) {
	var req dto.TestRedisReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	err := setupTestRedisConnection(req.Host, req.Port, req.Password, req.DB, req.TLS)
	if err != nil {
		response.Success(c, dto.TestConnectionResp{Success: false, ErrorMsg: err.Error()})
		return
	}
	response.Success(c, dto.TestConnectionResp{Success: true})
}

func handleInstall(c *gin.Context) {
	var req dto.InstallReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 构造 DB 配置：环境变量优先（含密码），覆盖前端可能传入的任何值。
	// 这样即使前端因为跳过了 wizard 步骤、传了占位/空值，也能拿到真正的连接信息。
	dbCfg := config.DatabaseConfig{
		Host:     req.Database.Host,
		Port:     req.Database.Port,
		User:     req.Database.User,
		Password: req.Database.Password,
		DBName:   req.Database.DBName,
		SSLMode:  req.Database.SSLMode,
	}
	if envDB := setupEnvDBConfig(); envDB != nil {
		dbCfg = *envDB
	}

	redisCfg := config.RedisConfig{
		Host:     req.Redis.Host,
		Port:     req.Redis.Port,
		Password: req.Redis.Password,
		DB:       req.Redis.DB,
		TLS:      req.Redis.TLS,
	}
	if envRedis := setupEnvRedisConfig(); envRedis != nil {
		redisCfg = *envRedis
	}

	params := InstallParams{
		DB:    dbCfg,
		Redis: redisCfg,
	}
	params.Admin.Email = req.Admin.Email
	params.Admin.Password = req.Admin.Password

	if err := setupInstall(params); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, nil)

	// 安装成功，触发回调通知主进程切换到正常模式
	if onInstallDone != nil {
		go func() {
			// 延迟一点让响应先发回前端
			time.Sleep(setupInstallCallbackDelay)
			onInstallDone()
		}()
	}
}
