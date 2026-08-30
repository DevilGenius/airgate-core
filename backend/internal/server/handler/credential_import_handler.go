package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	appaccount "github.com/DevilGenius/airgate-core/internal/app/account"
	appcredentialimport "github.com/DevilGenius/airgate-core/internal/app/credentialimport"
	"github.com/DevilGenius/airgate-core/internal/server/dto"
	"github.com/DevilGenius/airgate-core/internal/server/middleware"
	"github.com/DevilGenius/airgate-core/internal/server/response"
)

const (
	compatibleImportContractVersion = 1
	compatibleImportMaxItems        = 1024
	compatibleImportMaxBytes        = 32 << 20
	compatibleImportRequestMaxBytes = compatibleImportMaxBytes + (2 << 20)
	compatibleImportFieldMaxBytes   = 4 << 10
	compatibleImportFilenameMax     = 255
)

type CredentialImportHandler struct {
	accounts *AccountHandler
	parser   *appcredentialimport.Service
}

func NewCredentialImportHandler(
	accounts *AccountHandler,
	parser *appcredentialimport.Service,
) *CredentialImportHandler {
	return &CredentialImportHandler{accounts: accounts, parser: parser}
}

type compatibleImportRequest struct {
	Platform   string
	Format     string
	DryRun     bool
	Files      []appcredentialimport.InputFile
	TotalBytes int
}

type compatibleImportRequestError struct {
	Status  int
	Message string
}

func (e *compatibleImportRequestError) Error() string { return e.Message }

// ImportCompatibleAccounts 原子完成兼容凭证解析、Core 导入配置应用和账号导入。
// 请求体只在内存中处理，不写临时文件，也不记录凭证明文。
func (h *CredentialImportHandler) ImportCompatibleAccounts(c *gin.Context) {
	var req compatibleImportRequest
	var result dto.CompatibleAccountImportResp
	defer func() {
		logCompatibleImportAudit(c, req, result)
	}()

	if h == nil || h.accounts == nil || h.accounts.service == nil || h.parser == nil {
		response.InternalError(c, "兼容凭证导入服务不可用")
		return
	}

	var err error
	req, err = readCompatibleImportRequest(c)
	if err != nil {
		writeCompatibleImportError(c, err)
		return
	}
	parsed, err := h.parser.Parse(c.Request.Context(), appcredentialimport.ParseInput{
		Platform: req.Platform,
		Format:   req.Format,
		Files:    req.Files,
	})
	if err != nil {
		writeCompatibleImportError(c, err)
		return
	}
	if len(parsed.Accounts) > compatibleImportMaxItems {
		response.Error(c, http.StatusRequestEntityTooLarge, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("单次最多导入 %d 个账号", compatibleImportMaxItems))
		return
	}
	if len(parsed.Accounts) == 0 {
		response.BadRequest(c, "未识别到可导入的账号")
		return
	}

	inputs := make([]appaccount.CreateInput, 0, len(parsed.Accounts))
	for _, account := range parsed.Accounts {
		rateMultiplier := account.RateMultiplier
		inputs = append(inputs, appaccount.CreateInput{
			Name:           account.Name,
			Email:          account.Email,
			Platform:       req.Platform,
			Type:           account.Type,
			Credentials:    account.Credentials,
			Priority:       account.Priority,
			MaxConcurrency: account.MaxConcurrency,
			RateMultiplier: &rateMultiplier,
		})
	}
	inputs, err = h.accounts.applyConfiguredImport(c.Request.Context(), inputs)
	if err != nil {
		if isConfiguredImportClientError(err) {
			response.BadRequest(c, err.Error())
		} else {
			response.InternalError(c, err.Error())
		}
		return
	}

	issues := compatibleParseIssues(parsed.Issues)
	result = dto.CompatibleAccountImportResp{
		ContractVersion: compatibleImportContractVersion,
		Platform:        strings.ToLower(strings.TrimSpace(req.Platform)),
		Format:          strings.ToLower(strings.TrimSpace(req.Format)),
		DryRun:          req.DryRun,
		Parsed:          len(inputs),
		Issues:          issues,
	}
	if req.DryRun {
		validationErrors := h.accounts.service.ValidateConfiguredImport(c.Request.Context(), inputs)
		result.Failed = len(validationErrors)
		result.Issues = append(result.Issues, compatibleAccountIssues("validation", validationErrors)...)
	} else {
		summary := h.accounts.importConfiguredAccounts(c.Request.Context(), inputs)
		result.Imported = summary.Imported
		result.Failed = summary.Failed
		result.Issues = append(result.Issues, compatibleAccountIssues("import", summary.Errors)...)
	}

	response.Success(c, result)
}

// DeleteAccount 按账号主键软删除单个账号。
// 请求体严格限制为 {"id": <positive integer>}，不接受邮箱、名称或账号配置。
func (h *CredentialImportHandler) DeleteAccount(c *gin.Context) {
	if h == nil || h.accounts == nil || h.accounts.service == nil {
		response.InternalError(c, "账号删除服务不可用")
		return
	}

	var req dto.CredentialAccountDeleteReq
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		response.BadRequest(c, "请求只能包含有效的账号 id")
		return
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		response.BadRequest(c, "请求只能包含一个账号 id")
		return
	}
	if req.ID <= 0 {
		response.BadRequest(c, "账号 id 必须是正整数")
		return
	}

	if err := h.accounts.service.Delete(c.Request.Context(), req.ID); err != nil {
		httpCode, message := h.accounts.handleError("删除账号失败", "删除失败", err)
		response.Error(c, httpCode, httpCode, message)
		return
	}
	h.accounts.removeRouteGraphAccount(req.ID)
	response.Success(c, nil)
}

func readCompatibleImportRequest(c *gin.Context) (compatibleImportRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, compatibleImportRequestMaxBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		return compatibleImportRequest{}, &compatibleImportRequestError{
			Status: http.StatusBadRequest, Message: "请求必须使用 multipart/form-data",
		}
	}

	var req compatibleImportRequest
	seenFields := map[string]bool{}
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(nextErr, &maxErr) {
				return compatibleImportRequest{}, requestTooLargeError()
			}
			return compatibleImportRequest{}, &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "读取 multipart 请求失败"}
		}

		readErr := readCompatibleImportPart(&req, seenFields, part)
		_ = part.Close()
		if readErr != nil {
			return compatibleImportRequest{}, readErr
		}
	}

	req.Platform = strings.ToLower(strings.TrimSpace(req.Platform))
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	switch {
	case req.Platform == "":
		return compatibleImportRequest{}, &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "缺少 platform"}
	case req.Format == "":
		return compatibleImportRequest{}, &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "缺少 format"}
	case len(req.Files) == 0:
		return compatibleImportRequest{}, &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "请至少上传一个 files 文件"}
	}
	return req, nil
}

func readCompatibleImportPart(
	req *compatibleImportRequest,
	seenFields map[string]bool,
	part *multipart.Part,
) error {
	field := strings.TrimSpace(part.FormName())
	switch field {
	case "platform", "format", "dry_run":
		if seenFields[field] {
			return &compatibleImportRequestError{Status: http.StatusBadRequest, Message: field + " 只能提交一次"}
		}
		seenFields[field] = true
		value, err := readLimitedPart(part, compatibleImportFieldMaxBytes)
		if err != nil {
			return &compatibleImportRequestError{Status: http.StatusBadRequest, Message: field + " 内容过长"}
		}
		switch field {
		case "platform":
			req.Platform = string(value)
		case "format":
			req.Format = string(value)
		case "dry_run":
			trimmed := strings.TrimSpace(string(value))
			if trimmed == "" {
				return &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "dry_run 必须是 true 或 false"}
			}
			parsed, parseErr := strconv.ParseBool(trimmed)
			if parseErr != nil {
				return &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "dry_run 必须是 true 或 false"}
			}
			req.DryRun = parsed
		}
		return nil
	case "files":
		if len(req.Files) >= compatibleImportMaxItems {
			return &compatibleImportRequestError{Status: http.StatusRequestEntityTooLarge, Message: fmt.Sprintf("单次最多上传 %d 个文件", compatibleImportMaxItems)}
		}
		name := compatibleImportFilename(part.FileName(), len(req.Files)+1)
		remaining := compatibleImportMaxBytes - req.TotalBytes
		content, err := readLimitedPart(part, remaining)
		if err != nil {
			return requestTooLargeError()
		}
		req.TotalBytes += len(content)
		req.Files = append(req.Files, appcredentialimport.InputFile{Name: name, Content: content})
		return nil
	default:
		return &compatibleImportRequestError{Status: http.StatusBadRequest, Message: "不支持的 multipart 字段: " + field}
	}
}

func readLimitedPart(reader io.Reader, limit int) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("size limit exceeded")
	}
	content, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(content) > limit {
		return nil, errors.New("size limit exceeded")
	}
	return content, nil
}

func compatibleImportFilename(raw string, index int) string {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return fmt.Sprintf("account-%d.json", index)
	}
	if len(name) > compatibleImportFilenameMax {
		name = name[:compatibleImportFilenameMax]
	}
	return name
}

func requestTooLargeError() error {
	return &compatibleImportRequestError{
		Status:  http.StatusRequestEntityTooLarge,
		Message: fmt.Sprintf("兼容导入文件总大小不能超过 %d MiB", compatibleImportMaxBytes>>20),
	}
}

func writeCompatibleImportError(c *gin.Context, err error) {
	var requestErr *compatibleImportRequestError
	if errors.As(err, &requestErr) {
		response.Error(c, requestErr.Status, requestErr.Status, requestErr.Message)
		return
	}
	var parserErr *appcredentialimport.ParserError
	if errors.As(err, &parserErr) {
		status := parserErr.Status
		if status < http.StatusBadRequest || status > http.StatusNetworkAuthenticationRequired {
			status = http.StatusBadGateway
		}
		response.Error(c, status, status, parserErr.Message)
		return
	}
	switch {
	case errors.Is(err, appcredentialimport.ErrParserUnavailable):
		response.Error(c, http.StatusServiceUnavailable, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, appcredentialimport.ErrUnsupportedFormat):
		response.Error(c, http.StatusUnprocessableEntity, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, appcredentialimport.ErrInvalidCapability):
		response.InternalError(c, err.Error())
	default:
		slog.Error("credential_compat_import_failed", "error", err, "request_id", middleware.RequestIDFromGinContext(c))
		response.InternalError(c, "兼容凭证导入失败")
	}
}

func compatibleParseIssues(items []appcredentialimport.Issue) []dto.CompatibleAccountImportIssueResp {
	issues := make([]dto.CompatibleAccountImportIssueResp, 0, len(items))
	for _, item := range items {
		issue := dto.CompatibleAccountImportIssueResp{
			Stage: "parse", File: item.File, Level: item.Level, Message: item.Message,
		}
		if item.Index > 0 {
			index := item.Index
			issue.Index = &index
		}
		issues = append(issues, issue)
	}
	return issues
}

func compatibleAccountIssues(stage string, items []appaccount.ImportItemError) []dto.CompatibleAccountImportIssueResp {
	issues := make([]dto.CompatibleAccountImportIssueResp, 0, len(items))
	for _, item := range items {
		index := item.Index
		issues = append(issues, dto.CompatibleAccountImportIssueResp{
			Stage: stage, Index: &index, Name: item.Name, Level: "error", Message: item.Message,
		})
	}
	return issues
}

func logCompatibleImportAudit(c *gin.Context, req compatibleImportRequest, result dto.CompatibleAccountImportResp) {
	authKind := "admin"
	if value, ok := c.Get(middleware.CtxKeyAuthKind); ok {
		if text, ok := value.(string); ok && text != "" {
			authKind = text
		}
	}
	platform := result.Platform
	if platform == "" {
		platform = req.Platform
	}
	format := result.Format
	if format == "" {
		format = req.Format
	}
	slog.InfoContext(c.Request.Context(), "credential_compat_import_audit",
		"auth_kind", authKind,
		"platform", platform,
		"format", format,
		"dry_run", req.DryRun,
		"file_count", len(req.Files),
		"content_bytes", req.TotalBytes,
		"parsed", result.Parsed,
		"imported", result.Imported,
		"failed", result.Failed,
		"issue_count", len(result.Issues),
		"status", c.Writer.Status(),
		"ip", middleware.AuditClientIP(c),
		"request_id", middleware.RequestIDFromGinContext(c),
	)
}
