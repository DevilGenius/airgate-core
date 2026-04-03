package handler

import (
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	appsettings "github.com/DouDOU-start/airgate-core/internal/app/settings"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// publicGroups 允许公开访问的设置分组。
var publicGroups = []string{"site", "registration"}

// publicSafeKeys registration 分组中允许公开的 key（不暴露敏感项）。
var publicSafeKeys = map[string]bool{
	"registration_enabled": true,
	"email_verify_enabled": true,
}

// GetPublicSettings 获取公开设置（无需认证）。
func (h *SettingsHandler) GetPublicSettings(c *gin.Context) {
	result := make(map[string]string)

	for _, group := range publicGroups {
		list, err := h.service.List(c.Request.Context(), group)
		if err != nil {
			slog.Error("查询公共设置失败", "group", group, "error", err)
			continue
		}
		for _, item := range list {
			// site 分组全部公开；其他分组只公开白名单 key
			if group == "site" || publicSafeKeys[item.Key] {
				result[item.Key] = item.Value
			}
		}
	}

	response.Success(c, result)
}

// GetSettings 获取所有设置。
func (h *SettingsHandler) GetSettings(c *gin.Context) {
	list, err := h.service.List(c.Request.Context(), c.Query("group"))
	if err != nil {
		slog.Error("查询设置失败", "error", err)
		response.InternalError(c, "查询失败")
		return
	}

	resp := make([]dto.SettingResp, 0, len(list))
	for _, item := range list {
		resp = append(resp, toSettingResp(item))
	}
	response.Success(c, resp)
}

// UpdateSettings 批量更新设置。
func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	var req dto.UpdateSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	items := make([]appsettings.ItemInput, 0, len(req.Settings))
	for _, item := range req.Settings {
		items = append(items, appsettings.ItemInput{
			Key:   item.Key,
			Value: item.Value,
			Group: item.Group,
		})
	}

	if err := h.service.Update(c.Request.Context(), items); err != nil {
		slog.Error("更新设置失败", "error", err)
		response.InternalError(c, "更新设置失败")
		return
	}

	response.Success(c, nil)
}

// TestSMTP 测试 SMTP 连接并发送测试邮件。
func (h *SettingsHandler) TestSMTP(c *gin.Context) {
	var req dto.TestSMTPReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BindError(c, err)
		return
	}

	addr := fmt.Sprintf("%s:%d", req.Host, req.Port)

	// 构造邮件内容
	subject := "AirGate SMTP Test"
	body := "This is a test email from AirGate to verify your SMTP configuration."
	msg := strings.Join([]string{
		"From: " + req.From,
		"To: " + req.To,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	var auth smtp.Auth
	if req.Username != "" {
		auth = smtp.PlainAuth("", req.Username, req.Password, req.Host)
	}

	var sendErr error
	if req.UseTLS {
		// TLS 直连
		tlsConfig := &tls.Config{ServerName: req.Host}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			slog.Error("SMTP TLS 连接失败", "error", err)
			response.BadRequest(c, fmt.Sprintf("TLS connection failed: %v", err))
			return
		}
		defer func() { _ = conn.Close() }()

		client, err := smtp.NewClient(conn, req.Host)
		if err != nil {
			response.BadRequest(c, fmt.Sprintf("SMTP client error: %v", err))
			return
		}
		defer func() { _ = client.Close() }()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				response.BadRequest(c, fmt.Sprintf("SMTP auth failed: %v", err))
				return
			}
		}
		if err := client.Mail(req.From); err != nil {
			response.BadRequest(c, fmt.Sprintf("SMTP MAIL FROM error: %v", err))
			return
		}
		if err := client.Rcpt(req.To); err != nil {
			response.BadRequest(c, fmt.Sprintf("SMTP RCPT TO error: %v", err))
			return
		}
		w, err := client.Data()
		if err != nil {
			response.BadRequest(c, fmt.Sprintf("SMTP DATA error: %v", err))
			return
		}
		_, sendErr = w.Write([]byte(msg))
		_ = w.Close()
	} else {
		sendErr = smtp.SendMail(addr, auth, req.From, []string{req.To}, []byte(msg))
	}

	if sendErr != nil {
		slog.Error("SMTP 发送测试邮件失败", "error", sendErr)
		response.BadRequest(c, fmt.Sprintf("Send failed: %v", sendErr))
		return
	}

	response.Success(c, nil)
}

// UploadFile 上传文件（图片等）。
func (h *SettingsHandler) UploadFile(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择要上传的文件")
		return
	}
	defer func() { _ = file.Close() }()

	// 限制 2MB
	if header.Size > 2<<20 {
		response.BadRequest(c, "文件大小不能超过 2MB")
		return
	}

	// 只允许图片
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowed := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true}
	if !allowed[ext] {
		response.BadRequest(c, "只支持 PNG/JPG/GIF/SVG/ICO/WebP 格式")
		return
	}

	// 保存到 data/uploads/
	uploadDir := "data/uploads"
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		response.InternalError(c, "创建上传目录失败")
		return
	}

	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	dst, err := os.Create(filepath.Join(uploadDir, filename))
	if err != nil {
		response.InternalError(c, "保存文件失败")
		return
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, file); err != nil {
		response.InternalError(c, "写入文件失败")
		return
	}

	url := "/uploads/" + filename
	response.Success(c, map[string]string{"url": url})
}
