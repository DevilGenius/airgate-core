package handler

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	appauth "github.com/DouDOU-start/airgate-core/internal/app/auth"
	"github.com/DouDOU-start/airgate-core/internal/infra/mailer"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// Login 用户登录。
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	result, err := h.service.Login(c.Request.Context(), appauth.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		httpCode, message, unauthorized := h.handleLoginError(err)
		if unauthorized && httpCode == 401 {
			response.Unauthorized(c, message)
			return
		}
		if httpCode == 403 {
			response.Forbidden(c, message)
			return
		}
		if httpCode == 400 {
			response.BadRequest(c, message)
			return
		}
		response.InternalError(c, message)
		return
	}

	response.Success(c, dto.LoginResp{
		Token: result.Token,
		User:  userToResp(result.User),
	})
}

// Register 用户注册。
func (h *AuthHandler) Register(c *gin.Context) {
	// 检查是否允许注册
	if !h.isRegistrationEnabled(c) {
		response.Forbidden(c, "注册功能已关闭")
		return
	}

	var req dto.RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	// 检查是否开启了邮箱验证
	if h.isEmailVerifyEnabled(c) {
		if req.VerifyCode == "" {
			response.BadRequest(c, "请输入验证码")
			return
		}
		if !h.codeStore.Verify(req.Email, req.VerifyCode) {
			response.BadRequest(c, "验证码无效或已过期")
			return
		}
	}

	// 读取新用户默认值
	defaultBalance, defaultConcurrency := h.getNewUserDefaults(c)

	result, err := h.service.Register(c.Request.Context(), appauth.RegisterInput{
		Email:          req.Email,
		Password:       req.Password,
		Username:       req.Username,
		Balance:        defaultBalance,
		MaxConcurrency: defaultConcurrency,
	})
	if err != nil {
		httpCode, message := h.handleRegisterError(err)
		if httpCode == 400 {
			response.BadRequest(c, message)
			return
		}
		response.InternalError(c, message)
		return
	}

	response.Success(c, dto.LoginResp{
		Token: result.Token,
		User:  userToResp(result.User),
	})
}

// SendVerifyCode 发送邮箱验证码。
func (h *AuthHandler) SendVerifyCode(c *gin.Context) {
	var req dto.SendVerifyCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	// 检查邮箱是否已注册
	exists, err := h.service.EmailExists(c.Request.Context(), req.Email)
	if err != nil {
		response.InternalError(c, "检查邮箱失败")
		return
	}
	if exists {
		response.BadRequest(c, "该邮箱已被注册")
		return
	}

	// 生成验证码
	code := h.codeStore.Generate(req.Email)

	// 从设置读取 SMTP 配置并发送
	m, err := h.buildMailer(c)
	if err != nil {
		slog.Error("构建邮件发送器失败", "error", err)
		response.InternalError(c, "邮件服务未配置")
		return
	}

	// 读取站点名称
	siteName := "AirGate"
	siteSettings, _ := h.settingsService.List(c.Request.Context(), "site")
	for _, s := range siteSettings {
		if s.Key == "site_name" && s.Value != "" {
			siteName = s.Value
		}
	}

	subject := fmt.Sprintf("%s - 邮箱验证码", siteName)
	body := fmt.Sprintf(`
		<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 480px; margin: 0 auto; padding: 40px 20px;">
			<h2 style="color: #333; margin-bottom: 8px;">%s</h2>
			<p style="color: #666; font-size: 14px; margin-bottom: 24px;">您正在注册账户，请使用以下验证码完成注册：</p>
			<div style="background: #f5f5f5; border-radius: 8px; padding: 20px; text-align: center; margin-bottom: 24px;">
				<span style="font-size: 32px; font-weight: bold; letter-spacing: 8px; color: #333;">%s</span>
			</div>
			<p style="color: #999; font-size: 12px;">验证码 10 分钟内有效，请勿泄露给他人。</p>
		</div>
	`, siteName, code)

	if err := m.Send(req.Email, subject, body); err != nil {
		slog.Error("发送验证码邮件失败", "email", req.Email, "error", err)
		response.InternalError(c, fmt.Sprintf("发送邮件失败: %v", err))
		return
	}

	response.Success(c, nil)
}

// RefreshToken 刷新 JWT Token。
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	identity, ok := authIdentityFromContext(c)
	if !ok {
		response.Unauthorized(c, "用户未认证")
		return
	}

	token, err := h.service.RefreshToken(identity)
	if err != nil {
		response.InternalError(c, "刷新 Token 失败")
		return
	}

	response.Success(c, dto.RefreshResp{
		Token: token,
	})
}

// isRegistrationEnabled 检查是否允许注册（默认允许）。
func (h *AuthHandler) isRegistrationEnabled(c *gin.Context) bool {
	settings, err := h.settingsService.List(c.Request.Context(), "registration")
	if err != nil {
		return true
	}
	for _, s := range settings {
		if s.Key == "registration_enabled" && s.Value == "false" {
			return false
		}
	}
	return true
}

// getNewUserDefaults 读取新用户默认余额和并发数。
func (h *AuthHandler) getNewUserDefaults(c *gin.Context) (balance float64, concurrency int) {
	concurrency = 5 // 默认值
	settings, err := h.settingsService.List(c.Request.Context(), "defaults")
	if err != nil {
		return
	}
	for _, s := range settings {
		switch s.Key {
		case "default_balance":
			if v, e := strconv.ParseFloat(strings.TrimSpace(s.Value), 64); e == nil {
				balance = v
			}
		case "default_concurrency":
			if v, e := strconv.Atoi(strings.TrimSpace(s.Value)); e == nil && v > 0 {
				concurrency = v
			}
		}
	}
	return
}

// isEmailVerifyEnabled 检查是否开启了邮箱验证。
func (h *AuthHandler) isEmailVerifyEnabled(c *gin.Context) bool {
	settings, err := h.settingsService.List(c.Request.Context(), "registration")
	if err != nil {
		return false
	}
	for _, s := range settings {
		if s.Key == "email_verify_enabled" && s.Value == "true" {
			return true
		}
	}
	return false
}

// buildMailer 从系统设置构建邮件发送器。
func (h *AuthHandler) buildMailer(c *gin.Context) (*mailer.Mailer, error) {
	settings, err := h.settingsService.List(c.Request.Context(), "smtp")
	if err != nil {
		return nil, err
	}

	cfg := mailer.Config{}
	for _, s := range settings {
		switch s.Key {
		case "smtp_host":
			cfg.Host = s.Value
		case "smtp_port":
			cfg.Port, _ = strconv.Atoi(s.Value)
		case "smtp_username":
			cfg.Username = s.Value
		case "smtp_password":
			cfg.Password = s.Value
		case "smtp_from_email":
			cfg.FromAddr = s.Value
		case "smtp_from_name":
			cfg.FromName = s.Value
		case "smtp_use_tls":
			cfg.UseTLS = s.Value == "true"
		}
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("SMTP 未配置")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return mailer.New(cfg), nil
}
