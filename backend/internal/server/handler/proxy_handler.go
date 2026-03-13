package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/proxy"

	"github.com/DouDOU-start/airgate-core/ent"
	entProxy "github.com/DouDOU-start/airgate-core/ent/proxy"
	"github.com/DouDOU-start/airgate-core/internal/server/dto"
	"github.com/DouDOU-start/airgate-core/internal/server/response"
)

// ProxyHandler 代理管理 Handler
type ProxyHandler struct {
	db *ent.Client
}

// NewProxyHandler 创建 ProxyHandler
func NewProxyHandler(db *ent.Client) *ProxyHandler {
	return &ProxyHandler{db: db}
}

// ListProxies 分页列表代理
func (h *ProxyHandler) ListProxies(c *gin.Context) {
	var page dto.PageReq
	if err := c.ShouldBindQuery(&page); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	query := h.db.Proxy.Query()

	// 关键词搜索
	if page.Keyword != "" {
		query = query.Where(entProxy.NameContains(page.Keyword))
	}

	// 状态筛选
	if status := c.Query("status"); status != "" {
		query = query.Where(entProxy.StatusEQ(entProxy.Status(status)))
	}

	total, err := query.Count(c.Request.Context())
	if err != nil {
		slog.Error("查询代理总数失败", "error", err)
		response.InternalError(c, "查询失败")
		return
	}

	proxies, err := query.
		Offset((page.Page - 1) * page.PageSize).
		Limit(page.PageSize).
		Order(ent.Desc(entProxy.FieldCreatedAt)).
		All(c.Request.Context())
	if err != nil {
		slog.Error("查询代理列表失败", "error", err)
		response.InternalError(c, "查询失败")
		return
	}

	list := make([]dto.ProxyResp, 0, len(proxies))
	for _, p := range proxies {
		list = append(list, toProxyResp(p))
	}

	response.Success(c, response.PagedData(list, int64(total), page.Page, page.PageSize))
}

// CreateProxy 创建代理
func (h *ProxyHandler) CreateProxy(c *gin.Context) {
	var req dto.CreateProxyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	builder := h.db.Proxy.Create().
		SetName(req.Name).
		SetProtocol(entProxy.Protocol(req.Protocol)).
		SetAddress(req.Address).
		SetPort(req.Port)

	if req.Username != "" {
		builder = builder.SetUsername(req.Username)
	}
	if req.Password != "" {
		builder = builder.SetPassword(req.Password)
	}

	p, err := builder.Save(c.Request.Context())
	if err != nil {
		slog.Error("创建代理失败", "error", err)
		response.InternalError(c, "创建失败")
		return
	}

	response.Success(c, toProxyResp(p))
}

// UpdateProxy 更新代理
func (h *ProxyHandler) UpdateProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的代理 ID")
		return
	}

	var req dto.UpdateProxyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误: "+err.Error())
		return
	}

	builder := h.db.Proxy.UpdateOneID(id)

	if req.Name != nil {
		builder = builder.SetName(*req.Name)
	}
	if req.Protocol != nil {
		builder = builder.SetProtocol(entProxy.Protocol(*req.Protocol))
	}
	if req.Address != nil {
		builder = builder.SetAddress(*req.Address)
	}
	if req.Port != nil {
		builder = builder.SetPort(*req.Port)
	}
	if req.Username != nil {
		builder = builder.SetUsername(*req.Username)
	}
	if req.Password != nil {
		builder = builder.SetPassword(*req.Password)
	}
	if req.Status != nil {
		builder = builder.SetStatus(entProxy.Status(*req.Status))
	}

	p, err := builder.Save(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			response.NotFound(c, "代理不存在")
			return
		}
		slog.Error("更新代理失败", "error", err)
		response.InternalError(c, "更新失败")
		return
	}

	response.Success(c, toProxyResp(p))
}

// DeleteProxy 删除代理
func (h *ProxyHandler) DeleteProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的代理 ID")
		return
	}

	if err := h.db.Proxy.DeleteOneID(id).Exec(c.Request.Context()); err != nil {
		if ent.IsNotFound(err) {
			response.NotFound(c, "代理不存在")
			return
		}
		slog.Error("删除代理失败", "error", err)
		response.InternalError(c, "删除失败")
		return
	}

	response.Success(c, nil)
}

// TestProxy 测试代理连通性，通过代理请求 ip-api.com 获取出口 IP 和地理信息
func (h *ProxyHandler) TestProxy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "无效的代理 ID")
		return
	}

	p, err := h.db.Proxy.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			response.NotFound(c, "代理不存在")
			return
		}
		slog.Error("查询代理失败", "error", err)
		response.InternalError(c, "查询失败")
		return
	}

	result := probeProxy(c.Request.Context(), p)
	response.Success(c, result)
}

// probeProxy 通过代理发起 HTTP 请求，检测出口 IP 和延迟
func probeProxy(ctx context.Context, p *ent.Proxy) dto.TestProxyResp {
	const timeout = 15 * time.Second

	transport, err := buildProxyTransport(p)
	if err != nil {
		return dto.TestProxyResp{Success: false, ErrorMsg: "构建代理传输失败: " + err.Error()}
	}

	client := &http.Client{Transport: transport, Timeout: timeout}

	// 依次尝试多个检测端点（IP 信息 → 纯 IP → TCP 连通性）
	type probeEndpoint struct {
		url   string
		parse func([]byte) (ip, country, countryCode, city string)
	}
	endpoints := []probeEndpoint{
		{
			url: "http://ip-api.com/json/?lang=zh-CN",
			parse: func(body []byte) (string, string, string, string) {
				var r struct {
					Status      string `json:"status"`
					Query       string `json:"query"`
					Country     string `json:"country"`
					CountryCode string `json:"countryCode"`
					City        string `json:"city"`
				}
				if json.Unmarshal(body, &r) != nil || r.Status != "success" {
					return "", "", "", ""
				}
				return r.Query, r.Country, r.CountryCode, r.City
			},
		},
		{
			url: "http://httpbin.org/ip",
			parse: func(body []byte) (string, string, string, string) {
				var r struct {
					Origin string `json:"origin"`
				}
				if json.Unmarshal(body, &r) != nil {
					return "", "", "", ""
				}
				return r.Origin, "", "", ""
			},
		},
	}

	var lastErr string
	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "GET", ep.url, nil)
		if err != nil {
			lastErr = fmt.Sprintf("[%s] 创建请求失败: %v", ep.url, err)
			continue
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			lastErr = fmt.Sprintf("[%s] 请求失败: %v", ep.url, err)
			slog.Warn("代理检测端点请求失败", "url", ep.url, "error", err)
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Sprintf("[%s] HTTP %d", ep.url, resp.StatusCode)
			continue
		}

		ip, country, countryCode, city := ep.parse(body)
		if ip == "" {
			lastErr = fmt.Sprintf("[%s] 解析响应失败", ep.url)
			continue
		}

		return dto.TestProxyResp{
			Success:     true,
			Latency:     latency,
			IPAddress:   ip,
			Country:     country,
			CountryCode: countryCode,
			City:        city,
		}
	}

	// 所有 IP 检测端点都失败时，尝试通过代理访问常用 API（只测连通性和延迟）
	for _, target := range []string{"https://api.openai.com", "https://api.anthropic.com"} {
		req, err := http.NewRequestWithContext(ctx, "HEAD", target, nil)
		if err != nil {
			continue
		}
		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		// 任意 HTTP 响应（包括 401/403）都说明代理是通的
		return dto.TestProxyResp{
			Success: true,
			Latency: latency,
		}
	}

	return dto.TestProxyResp{Success: false, ErrorMsg: lastErr}
}

// buildProxyTransport 根据代理协议构建 http.Transport
func buildProxyTransport(p *ent.Proxy) (*http.Transport, error) {
	addr := net.JoinHostPort(p.Address, strconv.Itoa(p.Port))

	switch p.Protocol {
	case entProxy.ProtocolHTTP:
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   addr,
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		if p.Username != "" {
			proxyURL.User = url.UserPassword(p.Username, p.Password)
			// CONNECT 隧道（HTTPS 请求）需要显式携带 Proxy-Authorization
			basicAuth := base64.StdEncoding.EncodeToString([]byte(p.Username + ":" + p.Password))
			transport.ProxyConnectHeader = http.Header{
				"Proxy-Authorization": {"Basic " + basicAuth},
			}
		}
		return transport, nil

	case entProxy.ProtocolSocks5:
		var auth *proxy.Auth
		if p.Username != "" {
			auth = &proxy.Auth{User: p.Username, Password: p.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("创建 SOCKS5 dialer 失败: %w", err)
		}
		return &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}, nil

	default:
		return nil, fmt.Errorf("不支持的代理协议: %s", p.Protocol)
	}
}

// toProxyResp 将 ent.Proxy 转换为 dto.ProxyResp
func toProxyResp(p *ent.Proxy) dto.ProxyResp {
	return dto.ProxyResp{
		ID:       int64(p.ID),
		Name:     p.Name,
		Protocol: string(p.Protocol),
		Address:  p.Address,
		Port:     p.Port,
		Username: p.Username,
		Status:   string(p.Status),
		TimeMixin: dto.TimeMixin{
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
		},
	}
}
