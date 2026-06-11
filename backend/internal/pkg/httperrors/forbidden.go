package httperrors

import (
	"net/http"
	"strings"
)

func IsForbiddenError(reason string, upstreamStatus int) bool {
	if upstreamStatus == http.StatusForbidden {
		return true
	}
	reason = strings.ToLower(strings.TrimSpace(reason))
	return strings.HasPrefix(reason, "403") ||
		strings.Contains(reason, "http 403") ||
		strings.Contains(reason, "status 403") ||
		strings.Contains(reason, "403 forbidden") ||
		strings.Contains(reason, "403:")
}
