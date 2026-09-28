package traceredaction

import (
	"net/http"
	"net/url"
	"strings"
)

// SanitizeHeaders accepts raw diagnostic headers only. Plugin/client supplied
// fingerprints are ignored; Core derives them from the original identity fields.
func SanitizeHeaders(headers http.Header) http.Header {
	out := make(http.Header)
	for name, values := range headers {
		key := strings.ToLower(strings.TrimSpace(name))
		if allowedHeaders[key] {
			out[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
		}
	}
	for key, digest := range HeaderFingerprints(headers) {
		out.Set(key, digest)
	}
	return out
}

// SanitizeURL preserves routing information, not credentials or signed queries.
func SanitizeURL(raw string) string {
	if raw == "<invalid-url>" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return "<invalid-url>"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}
