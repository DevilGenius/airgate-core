package dto

// SetupStatusResp 安装状态响应
type SetupStatusResp struct {
	NeedsSetup bool `json:"needs_setup"`
}

// TestDBReq 测试数据库连接请求
type TestDBReq struct {
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	User     string `json:"user" binding:"required"`
	Password string `json:"password"`
	DBName   string `json:"dbname" binding:"required"`
	SSLMode  string `json:"sslmode"` // disable / require / verify-full
}

// TestRedisReq 测试 Redis 连接请求
type TestRedisReq struct {
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	TLS      bool   `json:"tls"`
}

// InstallReq 执行安装请求
type InstallReq struct {
	Database TestDBReq    `json:"database" binding:"required"`
	Redis    TestRedisReq `json:"redis" binding:"required"`
	Admin    AdminSetup   `json:"admin" binding:"required"`
}

// AdminSetup 管理员账户设置
type AdminSetup struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// TestConnectionResp 测试连接响应
type TestConnectionResp struct {
	Success  bool   `json:"success"`
	ErrorMsg string `json:"error_msg,omitempty"`
}
