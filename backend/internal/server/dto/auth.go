package dto

// LoginReq 登录请求
type LoginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	TOTPCode string `json:"totp_code"` // 可选，启用 TOTP 时必填
}

// LoginResp 登录响应
type LoginResp struct {
	Token string   `json:"token"`
	User  UserResp `json:"user"`
}

// RegisterReq 注册请求
type RegisterReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Username string `json:"username"`
}

// TOTPSetupResp TOTP 设置响应
type TOTPSetupResp struct {
	Secret string `json:"secret"` // Base32 编码的密钥
	URI    string `json:"uri"`    // otpauth:// URI，用于生成二维码
}

// TOTPVerifyReq TOTP 验证请求
type TOTPVerifyReq struct {
	Code string `json:"code" binding:"required,len=6"`
}

// RefreshResp Token 刷新响应
type RefreshResp struct {
	Token string `json:"token"`
}
