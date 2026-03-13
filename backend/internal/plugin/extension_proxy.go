package plugin

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	pb "github.com/DouDOU-start/airgate-sdk/proto"
)

// ExtensionProxy 将 HTTP 请求代理到 extension 类型插件
type ExtensionProxy struct {
	manager *Manager
}

// NewExtensionProxy 创建 extension 代理
func NewExtensionProxy(manager *Manager) *ExtensionProxy {
	return &ExtensionProxy{manager: manager}
}

// Handle 处理 extension 插件的 HTTP 请求
// 路由格式：/api/v1/ext/:pluginName/*path
func (ep *ExtensionProxy) Handle(c *gin.Context) {
	pluginName := c.Param("pluginName")
	subPath := c.Param("path")
	if subPath == "" {
		subPath = "/"
	}

	slog.Debug("ExtensionProxy 收到请求", "pluginName", pluginName, "subPath", subPath, "method", c.Request.Method, "fullPath", c.Request.URL.Path)

	ext := ep.manager.GetExtensionByName(pluginName)
	if ext == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "extension 插件未找到或未运行"})
		return
	}

	// 构建 gRPC HttpRequest
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}

	headers := make(map[string]*pb.HeaderValues)
	for k, v := range c.Request.Header {
		headers[strings.ToLower(k)] = &pb.HeaderValues{Values: v}
	}

	req := &pb.HttpRequest{
		Method:     c.Request.Method,
		Path:       subPath,
		Query:      c.Request.URL.RawQuery,
		Headers:    headers,
		Body:       body,
		RemoteAddr: c.ClientIP(),
	}

	resp, err := ext.HandleHTTPRequest(c.Request.Context(), req)
	if err != nil {
		slog.Error("extension 插件请求失败", "plugin", pluginName, "path", subPath, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "extension 插件请求失败"})
		return
	}

	// 写回响应头
	for k, vals := range resp.Headers {
		for _, v := range vals.Values {
			c.Writer.Header().Add(k, v)
		}
	}

	c.Data(int(resp.StatusCode), c.Writer.Header().Get("Content-Type"), resp.Body)
}
