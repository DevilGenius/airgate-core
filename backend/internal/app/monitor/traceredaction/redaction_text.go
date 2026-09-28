package traceredaction

import "regexp"

var (
	textURLPattern = regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s"'<>]+`)
	bearerPattern  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	skKeyPattern   = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	emailPattern   = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	// Generic token/secret/session labels apply only to free-form summaries;
	// they are not removed from normal structured model payloads.
	textSecretPattern = regexp.MustCompile(`(?i)\b(` + fieldPattern(diagnosticFieldNames()) + `)\b["']?\s*[:=]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^"',\s}]+)`)
	textImagePattern  = regexp.MustCompile(`(?i)\b(` + fieldPattern(imageFields) + `)\b["']?\s*[:=]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^"',\s}]+)`)
	dataImagePattern  = regexp.MustCompile(`(?i)data:image/[a-z0-9.+-]+(?:;[a-z0-9=.+-]+)*;base64,[a-z0-9+/=_-]+`)
)

// SanitizeText is for diagnostic summaries, not prompts or arbitrary body text.
func SanitizeText(text string) string {
	text = textURLPattern.ReplaceAllStringFunc(text, SanitizeURL)
	text = bearerPattern.ReplaceAllString(text, "Bearer [REDACTED]")
	text = skKeyPattern.ReplaceAllString(text, "sk-[REDACTED]")
	text = textSecretPattern.ReplaceAllString(text, "$1=[REDACTED]")
	text = textImagePattern.ReplaceAllString(text, "$1=[REDACTED_IMAGE]")
	text = dataImagePattern.ReplaceAllString(text, "[REDACTED_IMAGE]")
	return emailPattern.ReplaceAllString(text, "[REDACTED_EMAIL]")
}
