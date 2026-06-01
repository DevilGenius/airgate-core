package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/ent"
	"github.com/DevilGenius/airgate-core/internal/auth"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	sdk "github.com/DevilGenius/airgate-sdk/sdkgo"
)

// parseRequest 从 HTTP 请求构造 forwardState。认证 / body 读取 / 插件匹配失败时
// 直接写响应并返回 false。
func (f *Forwarder) parseRequest(c *gin.Context) (*forwardState, bool) {
	startedAt := time.Now()

	keyInfo, ok := requireKeyInfo(c)
	if !ok {
		return nil, false
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		slog.Error("request_body_read_failed",
			sdk.LogFieldUserID, keyInfo.UserID,
			sdk.LogFieldAPIKeyID, keyInfo.KeyID,
			sdk.LogFieldError, err,
		)
		openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid_request", "读取请求体失败")
		return nil, false
	}

	path := requestPath(c)
	parsed := parseBody(body, c.GetHeader("Content-Type"))
	if !validateRequestShape(c, keyInfo, path, parsed) {
		return nil, false
	}
	parsed.PreviousResponseID = firstNonEmpty(parsed.PreviousResponseID, previousResponseIDFromHeaders(c.Request.Header))
	parsed.SessionID = resolveRequestSessionID(c.Request.Header, parsed)
	requestedPlatform := requestedPlatform(c, keyInfo)
	inst := f.matchPlugin(c, keyInfo, requestedPlatform, path)
	if inst == nil {
		return nil, false
	}
	schedulingModels := schedulingModelsForRequest(requestedPlatform, path, parsed.Model)
	schedulingModel := ""
	if len(schedulingModels) > 0 {
		schedulingModel = schedulingModels[0]
	}

	return &forwardState{
		startedAt:                   startedAt,
		requestPath:                 path,
		body:                        body,
		model:                       parsed.Model,
		schedulingModels:            schedulingModels,
		schedulingModel:             schedulingModel,
		stream:                      parsed.Stream,
		realtime:                    parsed.Stream,
		sessionID:                   parsed.SessionID,
		previousResponseID:          parsed.PreviousResponseID,
		requireContinuationAffinity: requestRequiresContinuationAffinity(parsed),
		reasoningEffort:             parsed.ReasoningEffort,
		requestedPlatform:           requestedPlatform,
		keyInfo:                     keyInfo,
		plugin:                      inst,
	}, true
}

func requireKeyInfo(c *gin.Context) (*auth.APIKeyInfo, bool) {
	raw, exists := c.Get(middleware.CtxKeyKeyInfo)
	if !exists {
		writeUnauthenticated(c)
		return nil, false
	}
	keyInfo, ok := raw.(*auth.APIKeyInfo)
	if !ok || keyInfo == nil {
		writeUnauthenticated(c)
		return nil, false
	}
	return keyInfo, true
}

func writeUnauthenticated(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"message": "未认证",
			"type":    "authentication_error",
			"code":    "missing_api_key",
		},
	})
}

func requestPath(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if p := c.Param("path"); p != "" {
		return p
	}
	if c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func validateRequestShape(c *gin.Context, keyInfo *auth.APIKeyInfo, path string, parsed parsedRequest) bool {
	if !isImageSubmitAPIPath(path) {
		return true
	}
	if c.Request.Method != http.MethodPost {
		c.Header("Allow", http.MethodPost)
		slog.Info("image_request_method_not_allowed",
			sdk.LogFieldUserID, keyInfo.UserID,
			sdk.LogFieldAPIKeyID, keyInfo.KeyID,
			sdk.LogFieldPath, path,
			"method", c.Request.Method,
		)
		openAIError(c, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "Method Not Allowed")
		return false
	}
	if strings.TrimSpace(parsed.Model) == "" {
		slog.Info("image_request_missing_model",
			sdk.LogFieldUserID, keyInfo.UserID,
			sdk.LogFieldAPIKeyID, keyInfo.KeyID,
			sdk.LogFieldPath, path,
		)
		openAIError(c, http.StatusBadRequest, "invalid_request_error", "invalid_request", "model is required")
		return false
	}
	return true
}

func requestedPlatform(c *gin.Context, keyInfo *auth.APIKeyInfo) string {
	if platform := strings.TrimSpace(c.GetHeader("X-Airgate-Platform")); platform != "" {
		return platform
	}
	return keyInfo.GroupPlatform
}

func parseBody(body []byte, contentType string) parsedRequest {
	var fields requestFields
	if json.Unmarshal(body, &fields) == nil {
		effort := extractAndNormalizeReasoningEffort(fields)
		signals := analyzeContinuationSignals(fields)
		return parsedRequest{
			Model:               fields.Model,
			Stream:              fields.Stream,
			SessionID:           strings.TrimSpace(fields.Metadata.UserID),
			PromptCacheKey:      strings.TrimSpace(fields.PromptCacheKey),
			PreviousResponseID:  strings.TrimSpace(fields.PreviousResponseID),
			HasToolOutput:       signals.hasToolOutput,
			HasToolCallContext:  signals.hasToolCallContext,
			HasEncryptedContent: signals.hasEncryptedContent,
			ReasoningEffort:     effort,
		}
	}
	if strings.HasPrefix(contentType, "multipart/") {
		return parseMultipartFields(body, contentType)
	}
	return parsedRequest{}
}

func resolveRequestSessionID(headers http.Header, parsed parsedRequest) string {
	if parsed.SessionID != "" {
		return parsed.SessionID
	}
	if headers != nil {
		if v := firstNonEmpty(headers.Get("session_id"), headers.Get("Session_ID")); v != "" {
			return v
		}
		if v := firstNonEmpty(headers.Get("conversation_id"), headers.Get("Conversation_ID")); v != "" {
			return "conversation:" + v
		}
	}
	if parsed.PromptCacheKey != "" {
		return "prompt_cache:" + parsed.PromptCacheKey
	}
	return ""
}

func previousResponseIDFromHeaders(headers http.Header) string {
	if headers == nil {
		return ""
	}
	return firstNonEmpty(
		headers.Get("x-openai-previous-response-id"),
		headers.Get("OpenAI-Previous-Response-ID"),
		headers.Get("previous_response_id"),
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func requestRequiresContinuationAffinity(parsed parsedRequest) bool {
	return strings.TrimSpace(parsed.PreviousResponseID) != "" ||
		parsed.HasEncryptedContent ||
		(parsed.HasToolOutput && !parsed.HasToolCallContext)
}

type continuationSignals struct {
	hasToolOutput       bool
	hasToolCallContext  bool
	hasEncryptedContent bool
}

func analyzeContinuationSignals(fields requestFields) continuationSignals {
	signals := continuationSignals{}
	mergeSignals(&signals, analyzeResponsesInputSignals(fields.Input))
	mergeSignals(&signals, analyzeMessagesSignals(fields.Messages))
	return signals
}

func mergeSignals(dst *continuationSignals, src continuationSignals) {
	if dst == nil {
		return
	}
	dst.hasToolOutput = dst.hasToolOutput || src.hasToolOutput
	dst.hasToolCallContext = dst.hasToolCallContext || src.hasToolCallContext
	dst.hasEncryptedContent = dst.hasEncryptedContent || src.hasEncryptedContent
}

func analyzeResponsesInputSignals(raw json.RawMessage) continuationSignals {
	var signals continuationSignals
	if len(raw) == 0 {
		return signals
	}

	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err == nil {
		for _, item := range items {
			analyzeResponsesInputItemSignals(item, &signals)
		}
		return signals
	}

	var item map[string]any
	if err := json.Unmarshal(raw, &item); err == nil {
		analyzeResponsesInputItemSignals(item, &signals)
	}
	return signals
}

func analyzeResponsesInputItemSignals(item map[string]any, signals *continuationSignals) {
	if signals == nil {
		return
	}
	itemType, _ := item["type"].(string)
	if isReasoningItemWithEncryptedContent(itemType, item) {
		signals.hasEncryptedContent = true
	}
	switch {
	case isToolOutputItemType(itemType):
		signals.hasToolOutput = true
	case isToolCallContextItemType(itemType):
		if strings.TrimSpace(asString(item["call_id"])) != "" {
			signals.hasToolCallContext = true
		}
	}
}

func analyzeMessagesSignals(raw json.RawMessage) continuationSignals {
	var signals continuationSignals
	if len(raw) == 0 {
		return signals
	}
	var messages []map[string]any
	if err := json.Unmarshal(raw, &messages); err != nil {
		return signals
	}
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		if isReasoningItemWithEncryptedContent(asString(msg["type"]), msg) {
			signals.hasEncryptedContent = true
		}
		if role == "tool" {
			signals.hasToolOutput = true
		}
		if role == "assistant" {
			if _, ok := msg["tool_calls"]; ok {
				signals.hasToolCallContext = true
			}
			if _, ok := msg["function_call"]; ok {
				signals.hasToolCallContext = true
			}
		}
		analyzeMessageContentSignals(msg["content"], &signals)
	}
	return signals
}

func isReasoningItemWithEncryptedContent(itemType string, item map[string]any) bool {
	if strings.TrimSpace(itemType) != "reasoning" {
		return false
	}
	return strings.TrimSpace(asString(item["encrypted_content"])) != ""
}

func analyzeMessageContentSignals(content any, signals *continuationSignals) {
	if signals == nil {
		return
	}
	items, ok := content.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType := strings.TrimSpace(asString(itemMap["type"]))
		switch itemType {
		case "reasoning":
			if isReasoningItemWithEncryptedContent(itemType, itemMap) {
				signals.hasEncryptedContent = true
			}
		case "tool_result":
			signals.hasToolOutput = true
		case "tool_use":
			signals.hasToolCallContext = true
		}
	}
}

func isToolCallContextItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call":
		return true
	default:
		return false
	}
}

func isToolOutputItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "function_call_output", "tool_search_output", "custom_tool_call_output", "mcp_tool_call_output":
		return true
	default:
		return false
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

// extractAndNormalizeReasoningEffort 提取并归一化推理强度档位。
func extractAndNormalizeReasoningEffort(fields requestFields) string {
	effort := fields.ReasoningEffort
	if effort == "" && fields.Reasoning != nil {
		effort = fields.Reasoning.Effort
	}

	if effort == "" && fields.OutputConfig != nil {
		effort = fields.OutputConfig.Effort
	}

	if effort == "" && (fields.OutputConfig != nil || fields.Thinking != nil) {
		effort = "high"
	}

	return normalizeReasoningEffort(effort)
}

// normalizeReasoningEffort 归一化推理强度档位。
func normalizeReasoningEffort(effort string) string {
	normalized := strings.ToLower(strings.TrimSpace(effort))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")

	switch normalized {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "xhigh", "extrahigh":
		return "xhigh"
	case "max":
		return "max"
	default:
		return ""
	}
}

// requestNeedsImage 判断请求是否应走图片分组。
// 对话模型携带 image_generation tool 仍按对话请求路由，避免普通 Responses
// 工具调用被图片分组开关挡住；只有 Images API 路径和图像专用模型才强制图片分组。
func requestNeedsImage(path, model string, _ []byte) bool {
	return isImageSubmitAPIPath(path) || isImageModel(model)
}

// requestHasImageWorkload 判断请求是否需要更长的图片工作超时。
// 这里仍保留 Responses API 的 image_generation tool 识别，用于放宽生成链路
// 的等待时间，但不参与图片分组路由。
func requestHasImageWorkload(path, model string, body []byte) bool {
	return isImageSubmitAPIPath(path) || isImageModel(model) || hasImageGenerationTool(body)
}

func isImageSubmitAPIPath(path string) bool {
	switch path {
	case "/v1/images/generations", "/images/generations",
		"/v1/images/edits", "/images/edits":
		return true
	}
	if !strings.Contains(path, "images") && !strings.Contains(path, "Images") && !strings.Contains(path, "IMAGES") {
		return false
	}
	switch normalizeForwardPath(path) {
	case "/v1/images/generations", "/images/generations",
		"/v1/images/edits", "/images/edits":
		return true
	default:
		return false
	}
}

func normalizeForwardPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	if strings.Contains(path, "://") {
		if u, err := url.Parse(path); err == nil && u != nil && u.Path != "" {
			path = u.Path
		}
	}
	path = strings.TrimRight(strings.ToLower(strings.TrimSpace(path)), "/")
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func isImageModel(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "image")
}

func hasImageGenerationTool(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var payload struct {
		Tools []struct {
			Type string `json:"type"`
		} `json:"tools"`
		ToolChoice json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	for _, tool := range payload.Tools {
		if strings.EqualFold(strings.TrimSpace(tool.Type), "image_generation") {
			return true
		}
	}
	if len(payload.ToolChoice) == 0 {
		return false
	}
	var choiceString string
	if err := json.Unmarshal(payload.ToolChoice, &choiceString); err == nil {
		return strings.EqualFold(strings.TrimSpace(choiceString), "image_generation")
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(payload.ToolChoice, &choice); err == nil {
		if strings.EqualFold(strings.TrimSpace(choice.Type), "image_generation") {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(choice.Name), "image_generation") {
			return true
		}
	}
	return false
}

func parseMultipartFields(body []byte, contentType string) parsedRequest {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return parsedRequest{}
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var pr parsedRequest
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		name := part.FormName()
		if name != "model" && name != "stream" {
			_ = part.Close()
			continue
		}
		data, _ := io.ReadAll(part)
		_ = part.Close()
		switch name {
		case "model":
			pr.Model = strings.TrimSpace(string(data))
		case "stream":
			pr.Stream = strings.TrimSpace(string(data)) == "true"
		}
	}
	return pr
}

// matchPlugin 按 (platform, path) 路由到具体插件。
// 插件未运行返回 503；路由不匹配返回 404。
func (f *Forwarder) matchPlugin(c *gin.Context, keyInfo *auth.APIKeyInfo, platform, path string) *PluginInstance {
	if platform != "" {
		inst := f.manager.MatchPluginByPlatformAndPath(platform, path)
		if inst != nil {
			return inst
		}
		if f.manager.GetPluginByPlatform(platform) == nil {
			slog.Error("plugin_not_loaded_for_platform",
				sdk.LogFieldPlatform, platform,
				"available", availablePlatforms(f.manager),
				sdk.LogFieldUserID, keyInfo.UserID,
				sdk.LogFieldGroupID, keyInfo.GroupID,
				sdk.LogFieldPath, path,
			)
			openAIError(c, http.StatusServiceUnavailable, "server_error", "plugin_unavailable", "插件不可用，请联系管理员")
		} else {
			slog.Warn("plugin_route_not_found",
				sdk.LogFieldPlatform, platform,
				sdk.LogFieldPath, path,
				sdk.LogFieldGroupID, keyInfo.GroupID,
				sdk.LogFieldUserID, keyInfo.UserID,
			)
			openAIError(c, http.StatusNotFound, "invalid_request_error", "route_not_found", "当前平台不支持该 API 路径")
		}
		return nil
	}

	inst := f.manager.MatchPluginByPathPrefix(path)
	if inst == nil {
		slog.Warn("plugin_route_not_found",
			sdk.LogFieldPath, path,
			sdk.LogFieldUserID, keyInfo.UserID,
		)
		openAIError(c, http.StatusNotFound, "invalid_request_error", "route_not_found", "未找到匹配的插件")
	}
	return inst
}

// buildPluginRequest 组装给插件的 sdk.ForwardRequest。流式场景会带上 Writer。
func buildPluginRequest(c *gin.Context, state *forwardState) *sdk.ForwardRequest {
	headers := buildHeaders(c.Request.Header, state.keyInfo)
	// 路径和方法显式塞进 header：sdk.ForwardRequest 里没有这两字段，
	// 插件侧 extractForwardedPath 会优先读取这对 header。
	headers.Set("X-Forwarded-Path", state.requestPath)
	headers.Set("X-Forwarded-Method", c.Request.Method)
	if qs := c.Request.URL.RawQuery; qs != "" {
		headers.Set("X-Forwarded-Query", qs)
	}

	req := &sdk.ForwardRequest{
		Account: buildSDKAccount(state.account),
		Body:    state.body,
		Headers: headers,
		Model:   state.model,
		Stream:  state.stream,
	}
	if state.realtime {
		req.Writer = c.Writer
	}
	return req
}

// buildHeaders 克隆请求头并附加 X-Airgate-* 系列（分组级 service_tier / 强制 instructions / 插件开关）。
func buildHeaders(source http.Header, keyInfo *auth.APIKeyInfo) http.Header {
	headers := source.Clone()
	if keyInfo.UserID > 0 {
		headers.Set("X-Airgate-User-ID", strconv.Itoa(keyInfo.UserID))
	}
	if keyInfo.KeyID > 0 {
		headers.Set("X-Airgate-API-Key-ID", strconv.Itoa(keyInfo.KeyID))
	}
	if keyInfo.GroupID > 0 {
		headers.Set("X-Airgate-Group-ID", strconv.Itoa(keyInfo.GroupID))
	}
	if keyInfo.GroupServiceTier != "" {
		headers.Set("X-Airgate-Service-Tier", keyInfo.GroupServiceTier)
	}
	if keyInfo.GroupForceInstructions != "" {
		headers.Set("X-Airgate-Force-Instructions", keyInfo.GroupForceInstructions)
	}
	// 分组级插件开关：X-Airgate-Plugin-{plugin}-{key} 约定。
	for plugin, kv := range keyInfo.GroupPluginSettings {
		for k, v := range kv {
			if v == "" || !shouldForwardPluginSetting(plugin, k) {
				continue
			}
			headers.Set("X-Airgate-Plugin-"+canonicalHeaderToken(plugin)+"-"+canonicalHeaderToken(k), v)
		}
	}
	return headers
}

// canonicalHeaderToken 把 snake_case / kebab-case 规范化为 HTTP header token 风格（首字母大写、下划线变连字符）。
func canonicalHeaderToken(s string) string {
	out := make([]byte, 0, len(s))
	upNext := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c == '-' || c == '.' {
			out = append(out, '-')
			upNext = true
			continue
		}
		if upNext && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
		upNext = false
	}
	return string(out)
}

func buildSDKAccount(account *ent.Account) *sdk.Account {
	return &sdk.Account{
		ID:          int64(account.ID),
		Name:        account.Name,
		Platform:    account.Platform,
		Type:        account.Type,
		Credentials: account.Credentials,
		ProxyURL:    buildProxyURL(account),
	}
}

func buildProxyURL(account *ent.Account) string {
	proxy, err := account.Edges.ProxyOrErr()
	if err != nil || proxy == nil {
		return ""
	}
	if proxy.Username != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", proxy.Protocol, proxy.Username, proxy.Password, proxy.Address, proxy.Port)
	}
	return fmt.Sprintf("%s://%s:%d", proxy.Protocol, proxy.Address, proxy.Port)
}

// availablePlatforms 列出当前已加载的网关平台，用于 plugin_not_loaded_for_platform 日志诊断。
func availablePlatforms(m *Manager) []string {
	metas := m.GetAllPluginMeta()
	seen := make(map[string]struct{}, len(metas))
	out := make([]string, 0, len(metas))
	for _, mt := range metas {
		if mt.Platform == "" {
			continue
		}
		if _, ok := seen[mt.Platform]; ok {
			continue
		}
		seen[mt.Platform] = struct{}{}
		out = append(out, mt.Platform)
	}
	return out
}
