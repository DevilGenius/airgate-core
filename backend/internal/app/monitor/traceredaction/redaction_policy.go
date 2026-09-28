package traceredaction

import (
	"regexp"
	"strings"
)

// All trace redaction vocabularies live here. Format adapters must use these
// rules rather than maintaining independent lists in Core or a gateway plugin.
var credentialFields = []string{
	"access_token", "refresh_token", "id_token", "api_key", "client_secret", "authorization", "password", "cookie",
}

// Summaries and event detail use broader labels than structured model bodies.
var diagnosticSecretFields = []string{"token", "private_key", "secret", "session"}
var detailSecretFragments = diagnosticFragments()

func diagnosticFieldNames() []string {
	return append(append([]string(nil), credentialFields...), diagnosticSecretFields...)
}

func diagnosticFragments() []string {
	fields := diagnosticFieldNames()
	for i, field := range fields {
		fields[i] = normalizeField(field)
	}
	return fields
}

func IsSensitiveDetailKey(key string) bool {
	key = normalizeField(key)
	for _, field := range detailSecretFragments {
		if strings.Contains(key, field) {
			return true
		}
	}
	return false
}

var imageFields = []string{
	"image", "images", "mask", "masks", "image_url", "image_urls", "input_image", "input_images",
	"input_image_url", "input_image_urls", "image_data", "image_base64", "input_image_data",
	"reference_image", "reference_images", "source_image", "source_images", "init_image", "init_images",
	"b64_json", "partial_image_b64", "partial_image", "image_b64",
}

var identityHeaders = []struct {
	name    string
	aliases []string
}{
	{"session-id", []string{"session_id", "session-id", "x-session-id"}},
	{"conversation-id", []string{"conversation_id", "conversation-id"}},
	{"x-codex-turn-state", []string{"x-codex-turn-state"}},
}

var allowedHeaders = map[string]bool{
	"accept": true, "content-type": true, "openai-beta": true, "originator": true, "user-agent": true,
	"x-openai-previous-response-id": true, "retry-after": true, "retry-after-ms": true,
}

var imageTypes = map[string]bool{
	"image": true, "image_url": true, "input_image": true, "input_image_url": true, "image_file": true,
	"input_image_file": true, "computer_screenshot": true, "screenshot": true,
}

const generatedImageType = "image_generation_call"

var imageTypeHint = regexp.MustCompile(`(?i)["']type["']\s*:\s*["'](?:` + imageTypePattern() + `)(?:["']|$)`)

func imageTypePattern() string {
	names := make([]string, 0, len(imageTypes)+1)
	for name := range imageTypes {
		names = append(names, regexp.QuoteMeta(name))
	}
	names = append(names, regexp.QuoteMeta(generatedImageType))
	return strings.Join(names, "|")
}

var fieldSeparators = strings.NewReplacer("_", "", "-", "")
var fieldRules = buildFieldRules()
var imageFieldHint = regexp.MustCompile(`(?i)["']\s*(?:` + fieldPattern(imageFields) + `)\s*["']\s*:`)
var credentialFieldHint = regexp.MustCompile(`(?i)["']\s*(?:` + fieldPattern(credentialFields) + `)\s*["']\s*:`)
var imageValueHint = regexp.MustCompile(`(?i)data:image/|image/|\.(?:png|jpe?g|webp|gif)["']`)

func normalizeField(key string) string {
	return fieldSeparators.Replace(strings.ToLower(strings.TrimSpace(key)))
}
func buildFieldRules() map[string]redactions {
	out := make(map[string]redactions, len(credentialFields)+len(imageFields))
	for _, name := range credentialFields {
		out[name] = credentialRedaction
		out[normalizeField(name)] = credentialRedaction
	}
	for _, name := range imageFields {
		out[name] = imageRedaction
		out[normalizeField(name)] = imageRedaction
	}
	return out
}
func fieldPattern(names []string) string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = strings.ReplaceAll(regexp.QuoteMeta(name), "_", "[_-]?")
	}
	return strings.Join(out, "|")
}
func fieldRule(key string) redactions {
	key = strings.ToLower(strings.TrimSpace(key))
	if rule, ok := fieldRules[key]; ok {
		return rule
	}
	if !strings.ContainsAny(key, "_-") {
		return 0
	}
	return fieldRules[fieldSeparators.Replace(key)]
}
func isCredentialField(key string) bool { return fieldRule(key)&credentialRedaction != 0 }
func isImageField(key string) bool      { return fieldRule(key)&imageRedaction != 0 }

func isGeneratedImageResult(itemType, key string) bool {
	return itemType == generatedImageType && strings.EqualFold(strings.TrimSpace(key), "result")
}
