package forwardpath

import (
	"net/url"
	"strings"
)

// Normalize canonicalizes forwarded HTTP paths for routing, monitoring, and
// scheduler-model resolution.
func Normalize(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.Contains(path, "://") {
		if u, err := url.Parse(path); err == nil && u != nil && u.Path != "" {
			path = u.Path
		}
	}
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
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
