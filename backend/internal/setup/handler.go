package setup

import (
	"net/http"

	"github.com/DouDOU-start/airgate-core/internal/config"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册安装向导路由
func RegisterRoutes(r *gin.Engine) {
	setup := r.Group("/setup")
	{
		setup.GET("/status", handleStatus)
		setup.POST("/test-db", handleTestDB)
		setup.POST("/test-redis", handleTestRedis)
		setup.POST("/install", handleInstall)
	}
}

// setupGuard 安装保护中间件：安装完成后禁止访问
func setupGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !NeedsSetup() {
			response.Error(c, http.StatusForbidden, 403, "系统已安装")
			c.Abort()
			return
		}
		c.Next()
	}
}

func handleStatus(c *gin.Context) {
	response.Success(c, dto.SetupStatusResp{
		NeedsSetup: NeedsSetup(),
	})
}

func handleTestDB(c *gin.Context) {
	var req dto.TestDBReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	err := TestDBConnection(req.Host, req.Port, req.User, req.Password, req.DBName, req.SSLMode)
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
	err := TestRedisConnection(req.Host, req.Port, req.Password, req.DB)
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

	params := InstallParams{
		DB: config.DatabaseConfig{
			Host:     req.Database.Host,
			Port:     req.Database.Port,
			User:     req.Database.User,
			Password: req.Database.Password,
			DBName:   req.Database.DBName,
			SSLMode:  req.Database.SSLMode,
		},
		Redis: config.RedisConfig{
			Host:     req.Redis.Host,
			Port:     req.Redis.Port,
			Password: req.Redis.Password,
			DB:       req.Redis.DB,
			TLS:      req.Redis.TLS,
		},
	}
	params.Admin.Email = req.Admin.Email
	params.Admin.Password = req.Admin.Password

	if err := Install(params); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, nil)
}
