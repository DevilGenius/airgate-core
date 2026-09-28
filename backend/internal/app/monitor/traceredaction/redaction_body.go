package traceredaction

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const imageRedactionReason = "image_input"

type BodySnapshot struct {
	Body            []byte
	ContentType     string
	Redacted        bool
	RedactionReason string
	OriginalSize    int64
}

type BodyOptions struct{ ForceImageRequest bool }

func SanitizeBody(body []byte, contentType string, options BodyOptions) BodySnapshot {
	return sanitizeBody(body, contentType, options.ForceImageRequest)
}

type redactions uint8

const (
	credentialRedaction redactions = 1 << iota
	imageRedaction
	uncertainRedaction
)

func (r redactions) reason() string {
	if r&imageRedaction != 0 {
		return imageRedactionReason
	}
	if r&credentialRedaction != 0 {
		return "credentials"
	}
	return "unparseable_sensitive_body"
}

func sanitized(body []byte, contentType string, originalSize int, flags redactions) BodySnapshot {
	return BodySnapshot{Body: body, ContentType: contentType, Redacted: true, RedactionReason: flags.reason(), OriginalSize: int64(originalSize)}
}

func sanitizeBody(body []byte, contentType string, forceImage bool) BodySnapshot {
	unchanged := BodySnapshot{Body: body, ContentType: contentType}
	if len(body) == 0 {
		return unchanged
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	// Raw diagnostic error bytes may have no original response Content-Type.
	if strings.HasPrefix(http.DetectContentType(body), "image/") {
		return sanitized(nil, "application/json", len(body), imageRedaction)
	}
	switch {
	case mediaType == "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return sanitized(nil, contentType, len(body), uncertainRedaction)
		}
		flags := redactions(0)
		for key := range values {
			if isCredentialField(key) {
				values.Set(key, "[REDACTED]")
				flags |= credentialRedaction
			} else if isImageField(key) {
				values.Del(key)
				flags |= imageRedaction
			}
		}
		if flags != 0 {
			return sanitized([]byte(values.Encode()), contentType, len(body), flags)
		}
	case strings.HasPrefix(mediaType, "image/"):
		return sanitized(nil, "application/json", len(body), imageRedaction)
	case strings.HasPrefix(mediaType, "multipart/"):
		result, flags, err := redactMultipart(body, contentType, forceImage)
		if err != nil {
			return sanitized(nil, "application/json", len(body), uncertainRedaction)
		}
		if flags != 0 {
			return sanitized(result, "application/json", len(body), flags)
		}
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") || json.Valid(body):
		result, flags, err := redactJSON(body)
		if err != nil {
			flags = sensitiveHints(body)
			if forceImage {
				flags |= imageRedaction
			}
			if flags != 0 {
				return sanitized(nil, "application/json", len(body), flags)
			}
			return unchanged
		}
		if flags != 0 {
			return sanitized(result, "application/json", len(body), flags)
		}
	case forceImage:
		return sanitized(nil, "application/json", len(body), imageRedaction)
	}
	return unchanged
}

func sensitiveHints(body []byte) redactions {
	var flags redactions
	if imageFieldHint.Match(body) || imageTypeHint.Match(body) || imageValueHint.Match(body) {
		flags |= imageRedaction
	}
	if credentialFieldHint.Match(body) {
		flags |= credentialRedaction
	}
	// Escaped keys in invalid JSON cannot be safely classified.
	if bytes.Contains(body, []byte(`\u`)) {
		flags |= uncertainRedaction
	}
	return flags
}

func redactJSON(body []byte) ([]byte, redactions, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, 0, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, 0, errors.New("trailing JSON data")
	}
	result, keep, flags := redactJSONValue(value)
	if flags == 0 {
		return body, 0, nil
	}
	if !keep {
		result = map[string]any{}
	}
	encoded, err := json.Marshal(result)
	return encoded, flags, err
}

// The decoded tree belongs to this call. Mutate it once instead of walking it
// separately for credentials and images or rebuilding unchanged maps/slices.
func redactJSONValue(value any) (any, bool, redactions) {
	switch current := value.(type) {
	case map[string]any:
		if isImageContentBlock(current) {
			return nil, false, imageRedaction
		}
		flags := redactions(0)
		itemType, _ := current["type"].(string)
		itemType = strings.ToLower(strings.TrimSpace(itemType))
		for key, child := range current {
			rule := fieldRule(key)
			switch {
			case rule&credentialRedaction != 0:
				current[key] = "[REDACTED]"
				flags |= credentialRedaction
			case rule&imageRedaction != 0 || isGeneratedImageResult(itemType, key):
				delete(current, key)
				flags |= imageRedaction
			default:
				redacted, keep, changed := redactJSONValue(child)
				flags |= changed
				if keep {
					current[key] = redacted
				} else {
					delete(current, key)
				}
			}
		}
		return current, true, flags
	case []any:
		out := current[:0]
		flags := redactions(0)
		for _, child := range current {
			redacted, keep, changed := redactJSONValue(child)
			flags |= changed
			if keep {
				out = append(out, redacted)
			}
		}
		return out, true, flags
	case string:
		if isInlineImage(current) {
			return nil, false, imageRedaction
		}
	}
	return value, true, 0
}

func isImageContentBlock(value map[string]any) bool {
	kind, _ := value["type"].(string)
	if imageTypes[strings.ToLower(strings.TrimSpace(kind))] {
		return true
	}
	for _, field := range []string{"media_type", "mime_type"} {
		v, _ := value[field].(string)
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "image/") {
			return true
		}
	}
	return false
}
func isInlineImage(value string) bool {
	value = strings.TrimSpace(value)
	const prefix = "data:image/"
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

func redactMultipart(body []byte, contentType string, canonicalize bool) ([]byte, redactions, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, 0, err
	}
	if params["boundary"] == "" {
		return nil, 0, errors.New("missing multipart boundary")
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	fields := make(map[string][]string)
	flags := redactions(0)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, flags, err
		}
		name := part.FormName()
		partType := strings.ToLower(strings.TrimSpace(part.Header.Get("Content-Type")))
		if part.FileName() != "" || isImageField(name) || strings.HasPrefix(partType, "image/") || partType == "application/octet-stream" {
			// Close discards this part without allocating the file contents.
			if err := part.Close(); err != nil {
				return nil, flags, err
			}
			flags |= imageRedaction
			continue
		}
		if isCredentialField(name) {
			fields[name] = append(fields[name], "[REDACTED]")
			flags |= credentialRedaction
			if err := part.Close(); err != nil {
				return nil, flags, err
			}
			continue
		}
		data, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return nil, flags, err
		}
		if !utf8.Valid(data) || isInlineImage(string(data)) {
			flags |= imageRedaction
			continue
		}
		fields[name] = append(fields[name], string(data))
	}
	if flags == 0 && !canonicalize {
		return body, 0, nil
	}
	encoded, err := json.Marshal(fields)
	if flags == 0 {
		flags = imageRedaction
	}
	return encoded, flags, err
}

func IsImagePath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return strings.HasSuffix(path, "/images/generations") || strings.HasSuffix(path, "/images/edits")
}
func IsImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err == nil {
		return IsImagePath(parsed.Path)
	}
	return IsImagePath(raw)
}
