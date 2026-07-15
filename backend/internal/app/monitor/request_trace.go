package monitor

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeebo/xxh3"

	"github.com/DevilGenius/airgate-core/internal/requestmonitoring"
)

const (
	requestTraceSchemaVersion = 2
	requestTraceEncoding      = "gzip-json"
	requestTraceHashAlgorithm = "xxh3-128"
	maxRequestTraceRawBytes   = 256 << 20
)

type requestTracePayload struct {
	SchemaVersion int                      `json:"schema_version"`
	HashAlgorithm string                   `json:"hash_algorithm"`
	Request       requestTraceRequest      `json:"request"`
	Continuation  requestTraceContinuation `json:"continuation"`
	Attempts      []requestTraceAttempt    `json:"attempts,omitempty"`
	Final         requestTraceFinal        `json:"final"`
}

type requestTraceRequest struct {
	Method   string              `json:"method,omitempty"`
	Path     string              `json:"path,omitempty"`
	Platform string              `json:"platform,omitempty"`
	PluginID string              `json:"plugin_id,omitempty"`
	Model    string              `json:"model,omitempty"`
	Stream   bool                `json:"stream"`
	Headers  map[string][]string `json:"headers,omitempty"`
	Body     requestTraceBody    `json:"body"`
}

type requestTraceContinuation struct {
	PreviousResponseID          string `json:"previous_response_id,omitempty"`
	RequireContinuationAffinity bool   `json:"require_continuation_affinity"`
	RecoveryApplied             bool   `json:"recovery_applied"`
}

type requestTraceAttempt struct {
	Number      int    `json:"number"`
	AccountID   int    `json:"account_id,omitempty"`
	AccountType string `json:"account_type,omitempty"`

	ClientModel     string `json:"client_model,omitempty"`
	SchedulingModel string `json:"scheduling_model,omitempty"`
	WireModel       string `json:"wire_model,omitempty"`
	RuleID          string `json:"rule_id,omitempty"`
	Operation       string `json:"operation,omitempty"`
	TimeoutProfile  string `json:"timeout_profile,omitempty"`

	OutcomeKind   string `json:"outcome_kind,omitempty"`
	FailoverScope string `json:"failover_scope,omitempty"`
	Reason        string `json:"reason,omitempty"`
	PluginError   string `json:"plugin_error,omitempty"`

	UpstreamStatus  int                    `json:"upstream_status,omitempty"`
	UpstreamHeaders map[string][]string    `json:"upstream_headers,omitempty"`
	UpstreamBody    requestTraceBody       `json:"upstream_body"`
	Outbound        []requestTraceOutbound `json:"outbound_requests,omitempty"`
	RawError        requestTraceBody       `json:"raw_upstream_error"`
}

type requestTraceOutbound struct {
	Transport  string              `json:"transport,omitempty"`
	Method     string              `json:"method,omitempty"`
	URL        string              `json:"url,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       requestTraceBody    `json:"body"`
	StatusCode int                 `json:"status_code,omitempty"`
}

type requestTraceFinal struct {
	Stage      string `json:"stage,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	ErrorType  string `json:"error_type,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type requestTraceBody struct {
	ContentType      string                             `json:"content_type,omitempty"`
	Size             int                                `json:"size"`
	OriginalSize     int64                              `json:"original_size,omitempty"`
	Redacted         bool                               `json:"redacted,omitempty"`
	RedactionReason  string                             `json:"redaction_reason,omitempty"`
	Hash             string                             `json:"hash,omitempty"`
	HashAlgorithm    string                             `json:"hash_algorithm,omitempty"`
	Encoding         string                             `json:"encoding,omitempty"`
	Text             string                             `json:"text,omitempty"`
	Base64           string                             `json:"base64,omitempty"`
	EncryptedContent []encryptedContentTraceFingerprint `json:"encrypted_content,omitempty"`
}

type encryptedContentTraceFingerprint struct {
	Path          string `json:"path"`
	Type          string `json:"type,omitempty"`
	ID            string `json:"id,omitempty"`
	Size          int    `json:"size"`
	Hash          string `json:"hash"`
	HashAlgorithm string `json:"hash_algorithm"`
}

type requestTraceBodyOptions struct {
	RedactImageInputs bool
	ForceImageRequest bool
	AlreadyRedacted   bool
	RedactionReason   string
	OriginalSize      int64
}

func encodeRequestTrace(input requestmonitoring.TraceInput, retention time.Duration) (StoredRequestTrace, error) {
	requestBody := buildRequestTraceBody(input.RequestBody, headerContentType(input.RequestHeaders), requestTraceBodyOptions{
		RedactImageInputs: true,
		ForceImageRequest: isRequestTraceImagePath(input.Path),
	})
	payload := requestTracePayload{
		SchemaVersion: requestTraceSchemaVersion,
		HashAlgorithm: requestTraceHashAlgorithm,
		Request: requestTraceRequest{
			Method:   input.Method,
			Path:     input.Path,
			Platform: input.Platform,
			PluginID: input.PluginID,
			Model:    input.Model,
			Stream:   input.Stream,
			Headers:  storedTraceHeadersForBody(input.RequestHeaders, requestBody),
			Body:     requestBody,
		},
		Continuation: requestTraceContinuation{
			PreviousResponseID:          input.PreviousResponseID,
			RequireContinuationAffinity: input.RequireContinuationAffinity,
			RecoveryApplied:             input.ContinuationRecoveryApplied,
		},
		Final: requestTraceFinal{
			Stage:      input.Final.Stage,
			HTTPStatus: input.Final.HTTPStatus,
			ErrorType:  input.Final.ErrorType,
			ErrorCode:  input.Final.ErrorCode,
			Message:    scrubText(input.Final.Message),
		},
	}
	if len(input.Attempts) > 0 {
		payload.Attempts = make([]requestTraceAttempt, 0, len(input.Attempts))
	}
	for _, attempt := range input.Attempts {
		upstreamBody := buildRequestTraceBody(attempt.UpstreamBody, headerContentType(attempt.UpstreamHeaders), requestTraceBodyOptions{
			RedactImageInputs: true,
		})
		storedAttempt := requestTraceAttempt{
			Number:          attempt.Number,
			AccountID:       attempt.AccountID,
			AccountType:     attempt.AccountType,
			ClientModel:     attempt.ClientModel,
			SchedulingModel: attempt.SchedulingModel,
			WireModel:       attempt.WireModel,
			RuleID:          attempt.RuleID,
			Operation:       attempt.Operation,
			TimeoutProfile:  attempt.TimeoutProfile,
			OutcomeKind:     attempt.OutcomeKind,
			FailoverScope:   attempt.FailoverScope,
			Reason:          scrubText(attempt.Reason),
			PluginError:     scrubText(attempt.PluginError),
			UpstreamStatus:  attempt.UpstreamStatus,
			UpstreamHeaders: storedTraceHeadersForBody(attempt.UpstreamHeaders, upstreamBody),
			UpstreamBody:    upstreamBody,
			RawError: buildRequestTraceBody(attempt.UpstreamErrorBody, "application/json", requestTraceBodyOptions{
				RedactImageInputs: true,
			}),
		}
		if len(attempt.OutboundRequests) > 0 {
			storedAttempt.Outbound = make([]requestTraceOutbound, 0, len(attempt.OutboundRequests))
		}
		for _, outbound := range attempt.OutboundRequests {
			outboundBody := buildRequestTraceBody(outbound.Body, headerContentType(outbound.Headers), requestTraceBodyOptions{
				RedactImageInputs: true,
				ForceImageRequest: isRequestTraceImageURL(outbound.URL),
				AlreadyRedacted:   outbound.BodyRedacted,
				RedactionReason:   outbound.BodyRedactionReason,
				OriginalSize:      outbound.BodyOriginalSize,
			})
			storedAttempt.Outbound = append(storedAttempt.Outbound, requestTraceOutbound{
				Transport:  outbound.Transport,
				Method:     outbound.Method,
				URL:        redactStoredTraceURL(outbound.URL),
				Headers:    storedTraceHeadersForBody(outbound.Headers, outboundBody),
				Body:       outboundBody,
				StatusCode: outbound.StatusCode,
			})
		}
		payload.Attempts = append(payload.Attempts, storedAttempt)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return StoredRequestTrace{}, fmt.Errorf("marshal request trace: %w", err)
	}
	if len(raw) > maxRequestTraceRawBytes {
		return StoredRequestTrace{}, fmt.Errorf("request trace exceeds %d bytes", maxRequestTraceRawBytes)
	}
	digest := requestTraceFastHash(raw)
	var compressed bytes.Buffer
	zw, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err != nil {
		return StoredRequestTrace{}, fmt.Errorf("create request trace compressor: %w", err)
	}
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return StoredRequestTrace{}, fmt.Errorf("compress request trace: %w", err)
	}
	if err := zw.Close(); err != nil {
		return StoredRequestTrace{}, fmt.Errorf("close request trace compressor: %w", err)
	}

	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	if retention <= 0 {
		retention = defaultRetention
	}
	return StoredRequestTrace{
		Hash:           digest,
		SchemaVersion:  requestTraceSchemaVersion,
		Encoding:       requestTraceEncoding,
		Payload:        compressed.Bytes(),
		RawSize:        int64(len(raw)),
		CompressedSize: int64(compressed.Len()),
		SeenCount:      1,
		FirstSeenAt:    observedAt,
		LastSeenAt:     observedAt,
		ExpiresAt:      observedAt.Add(retention),
	}, nil
}

func requestTraceReferencedBytes(input requestmonitoring.TraceInput) int64 {
	total := int64(len(input.RequestBody))
	for _, attempt := range input.Attempts {
		total += int64(len(attempt.UpstreamBody) + len(attempt.UpstreamErrorBody))
		for _, outbound := range attempt.OutboundRequests {
			total += int64(len(outbound.Body))
		}
	}
	return total
}

func decodeStoredRequestTrace(stored StoredRequestTrace) (RequestTrace, error) {
	if stored.Encoding != requestTraceEncoding || stored.RawSize < 0 || stored.RawSize > maxRequestTraceRawBytes {
		return RequestTrace{}, fmt.Errorf("unsupported or invalid request trace encoding")
	}
	zr, err := gzip.NewReader(bytes.NewReader(stored.Payload))
	if err != nil {
		return RequestTrace{}, fmt.Errorf("open request trace: %w", err)
	}
	limit := stored.RawSize + 1
	if limit <= 0 || limit > maxRequestTraceRawBytes+1 {
		limit = maxRequestTraceRawBytes + 1
	}
	raw, readErr := io.ReadAll(io.LimitReader(zr, limit))
	closeErr := zr.Close()
	if readErr != nil {
		return RequestTrace{}, fmt.Errorf("read request trace: %w", readErr)
	}
	if closeErr != nil {
		return RequestTrace{}, fmt.Errorf("close request trace: %w", closeErr)
	}
	if int64(len(raw)) != stored.RawSize {
		return RequestTrace{}, fmt.Errorf("request trace size mismatch")
	}
	if requestTraceFastHash(raw) != stored.Hash {
		return RequestTrace{}, fmt.Errorf("request trace hash mismatch")
	}
	return RequestTrace{
		Hash:           stored.Hash,
		HashAlgorithm:  requestTraceHashAlgorithm,
		SchemaVersion:  stored.SchemaVersion,
		Encoding:       stored.Encoding,
		Payload:        raw,
		RawSize:        stored.RawSize,
		CompressedSize: stored.CompressedSize,
		SeenCount:      stored.SeenCount,
		FirstSeenAt:    stored.FirstSeenAt,
		LastSeenAt:     stored.LastSeenAt,
		ExpiresAt:      stored.ExpiresAt,
	}, nil
}

func buildRequestTraceBody(body []byte, contentType string, options requestTraceBodyOptions) requestTraceBody {
	originalBodySize := len(body)
	redacted := options.AlreadyRedacted
	redactionReason := strings.TrimSpace(options.RedactionReason)
	originalSize := options.OriginalSize
	if options.RedactImageInputs {
		snapshot := sanitizeStoredRequestTraceBody(body, contentType, options.ForceImageRequest)
		body = snapshot.Body
		if snapshot.Redacted {
			contentType = snapshot.ContentType
			redacted = true
			if redactionReason == "" {
				redactionReason = snapshot.RedactionReason
			}
			if originalSize <= 0 {
				originalSize = snapshot.OriginalSize
			}
		}
	}
	if redacted && originalSize <= 0 {
		originalSize = int64(originalBodySize)
	}
	out := requestTraceBody{
		ContentType:     contentType,
		Size:            len(body),
		OriginalSize:    originalSize,
		Redacted:        redacted,
		RedactionReason: redactionReason,
	}
	if len(body) == 0 {
		return out
	}
	out.Hash = requestTraceFastHash(body)
	out.HashAlgorithm = requestTraceHashAlgorithm
	if utf8.Valid(body) {
		out.Encoding = "utf-8"
		out.Text = string(body)
	} else {
		out.Encoding = "base64"
		out.Base64 = base64.StdEncoding.EncodeToString(body)
	}
	out.EncryptedContent = encryptedContentFingerprints(body)
	return out
}

func encryptedContentFingerprints(body []byte) []encryptedContentTraceFingerprint {
	if len(body) == 0 || !json.Valid(body) {
		return nil
	}
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	var out []encryptedContentTraceFingerprint
	walkEncryptedContent(value, "$", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkEncryptedContent(value interface{}, path string, out *[]encryptedContentTraceFingerprint) {
	switch current := value.(type) {
	case []interface{}:
		for index, item := range current {
			walkEncryptedContent(item, fmt.Sprintf("%s[%d]", path, index), out)
		}
	case map[string]interface{}:
		itemType, _ := current["type"].(string)
		itemID, _ := current["id"].(string)
		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := current[key]
			childPath := path + "." + key
			if key == "encrypted_content" {
				if encrypted, ok := child.(string); ok && encrypted != "" {
					*out = append(*out, encryptedContentTraceFingerprint{
						Path:          childPath,
						Type:          itemType,
						ID:            itemID,
						Size:          len(encrypted),
						Hash:          requestTraceFastHashString(encrypted),
						HashAlgorithm: requestTraceHashAlgorithm,
					})
				}
				continue
			}
			walkEncryptedContent(child, childPath, out)
		}
	}
}

func safeStoredTraceHeaders(headers http.Header) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string)
	for name, values := range headers {
		canonical := strings.ToLower(strings.TrimSpace(name))
		switch canonical {
		case "accept", "content-type", "openai-beta", "originator", "user-agent", "x-openai-previous-response-id", "retry-after", "retry-after-ms":
			out[canonical] = append([]string(nil), values...)
		case "session_id", "session-id", "x-session-id", "conversation_id", "conversation-id", "x-codex-turn-state":
			key := strings.NewReplacer("_", "-", ".", "-").Replace(canonical)
			out["x-airgate-trace-"+key+"-xxh3-128"] = []string{requestTraceFastHashString(strings.Join(values, "\x00"))}
		default:
			if strings.HasPrefix(canonical, "x-airgate-trace-") {
				out[canonical] = append([]string(nil), values...)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func storedTraceHeadersForBody(headers http.Header, body requestTraceBody) map[string][]string {
	out := safeStoredTraceHeaders(headers)
	if !body.Redacted || body.ContentType == "" {
		return out
	}
	if out == nil {
		out = make(map[string][]string)
	}
	out["content-type"] = []string{body.ContentType}
	return out
}

func requestTraceFastHash(value []byte) string {
	digest := xxh3.Hash128(value).Bytes()
	return hex.EncodeToString(digest[:])
}

func requestTraceFastHashString(value string) string {
	digest := xxh3.HashString128(value).Bytes()
	return hex.EncodeToString(digest[:])
}

func headerContentType(headers http.Header) string {
	if headers == nil {
		return ""
	}
	return strings.TrimSpace(headers.Get("Content-Type"))
}

func redactStoredTraceURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		if index := strings.IndexByte(raw, '?'); index >= 0 {
			return raw[:index]
		}
		return raw
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
