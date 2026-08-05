package credentialimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	apppluginadmin "github.com/DevilGenius/airgate-core/internal/app/pluginadmin"
)

const (
	// CapabilityMetadataKey 是插件声明兼容凭证解析能力的稳定 metadata key。
	CapabilityMetadataKey = "account_import.v1"
	capabilityAction      = "accounts/import/compat"
)

var (
	ErrParserUnavailable = errors.New("指定平台未提供兼容凭证解析能力")
	ErrUnsupportedFormat = errors.New("指定平台不支持该凭证格式")
	ErrInvalidCapability = errors.New("插件兼容凭证能力声明无效")
)

type InputFile struct {
	Name    string
	Content []byte
}

type ParseInput struct {
	Platform string
	Format   string
	Files    []InputFile
}

type AccountDraft struct {
	Name           string            `json:"name"`
	Email          *string           `json:"email,omitempty"`
	Type           string            `json:"type"`
	Credentials    map[string]string `json:"credentials"`
	Priority       int               `json:"priority"`
	MaxConcurrency int               `json:"max_concurrency"`
	RateMultiplier float64           `json:"rate_multiplier"`
}

type Issue struct {
	File    string `json:"file,omitempty"`
	Index   int    `json:"index,omitempty"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type ParseResult struct {
	Format   string         `json:"format"`
	Accounts []AccountDraft `json:"accounts"`
	Issues   []Issue        `json:"issues,omitempty"`
	Renamed  bool           `json:"renamed"`
}

type ParserError struct {
	Status  int
	Message string
}

func (e *ParserError) Error() string {
	return e.Message
}

type capabilityMetadata struct {
	Formats []string `json:"formats"`
}

type CapabilityProxy interface {
	ResolveGatewayCapability(platform, capability string) (apppluginadmin.CapabilityTarget, error)
	Proxy(ctx context.Context, input apppluginadmin.ProxyInput) (apppluginadmin.ProxyResult, error)
}

type Service struct {
	plugins CapabilityProxy
}

func NewService(plugins CapabilityProxy) *Service {
	return &Service{plugins: plugins}
}

func (s *Service) Parse(ctx context.Context, input ParseInput) (ParseResult, error) {
	platform := strings.ToLower(strings.TrimSpace(input.Platform))
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if s == nil || s.plugins == nil {
		return ParseResult{}, ErrParserUnavailable
	}

	target, err := s.plugins.ResolveGatewayCapability(platform, CapabilityMetadataKey)
	if err != nil {
		if errors.Is(err, apppluginadmin.ErrPluginCapabilityUnavailable) {
			return ParseResult{}, ErrParserUnavailable
		}
		return ParseResult{}, err
	}
	var capability capabilityMetadata
	if err := json.Unmarshal([]byte(target.Metadata), &capability); err != nil || len(capability.Formats) == 0 {
		return ParseResult{}, ErrInvalidCapability
	}
	if !supportsFormat(capability.Formats, format) {
		return ParseResult{}, ErrUnsupportedFormat
	}

	type pluginFile struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	payload := struct {
		Format string       `json:"format"`
		Files  []pluginFile `json:"files"`
	}{Format: format, Files: make([]pluginFile, 0, len(input.Files))}
	for _, file := range input.Files {
		payload.Files = append(payload.Files, pluginFile{Name: file.Name, Content: string(file.Content)})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ParseResult{}, fmt.Errorf("编码兼容凭证解析请求失败: %w", err)
	}

	result, err := s.plugins.Proxy(ctx, apppluginadmin.ProxyInput{
		Name:   target.PluginName,
		Method: http.MethodPost,
		Action: capabilityAction,
		Body:   body,
	})
	if err != nil {
		if errors.Is(err, apppluginadmin.ErrPluginUnavailable) {
			return ParseResult{}, ErrParserUnavailable
		}
		return ParseResult{}, err
	}
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		return ParseResult{}, parserErrorFromResponse(result.StatusCode, result.Body)
	}

	var parsed ParseResult
	if err := json.Unmarshal(result.Body, &parsed); err != nil {
		return ParseResult{}, fmt.Errorf("解析插件兼容凭证响应失败: %w", err)
	}
	if parsed.Accounts == nil {
		parsed.Accounts = []AccountDraft{}
	}
	return parsed, nil
}

func supportsFormat(formats []string, format string) bool {
	for _, candidate := range formats {
		if strings.EqualFold(strings.TrimSpace(candidate), format) {
			return true
		}
	}
	return false
}

func parserErrorFromResponse(status int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	message := "兼容凭证解析失败"
	if json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Error) != "" {
		message = strings.TrimSpace(payload.Error)
	}
	return &ParserError{Status: status, Message: message}
}
