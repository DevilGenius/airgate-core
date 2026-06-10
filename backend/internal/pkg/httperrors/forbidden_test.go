package httperrors

import (
	"net/http"
	"testing"
)

func TestIsForbiddenError(t *testing.T) {
	tests := []struct {
		name           string
		reason         string
		upstreamStatus int
		want           bool
	}{
		{name: "status", upstreamStatus: http.StatusForbidden, want: true},
		{name: "prefix", reason: "403 access denied", want: true},
		{name: "http", reason: "HTTP 403: forbidden", want: true},
		{name: "status text", reason: "status 403 from upstream", want: true},
		{name: "forbidden text", reason: "403 forbidden", want: true},
		{name: "colon", reason: "upstream returned 403: denied", want: true},
		{name: "unauthorized", reason: "HTTP 401: invalid token", upstreamStatus: http.StatusUnauthorized, want: false},
		{name: "empty", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsForbiddenError(tt.reason, tt.upstreamStatus); got != tt.want {
				t.Fatalf("IsForbiddenError(%q, %d) = %v, want %v", tt.reason, tt.upstreamStatus, got, tt.want)
			}
		})
	}
}
