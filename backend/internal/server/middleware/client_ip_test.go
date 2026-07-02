package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAuditClientIPUsesForwardedForFromLocalProxy(t *testing.T) {
	c := testIPContext("172.18.0.1:4567")
	c.Request.Header.Set("X-Forwarded-For", "198.51.100.20, 172.18.0.1")

	if got := AuditClientIP(c); got != "198.51.100.20" {
		t.Fatalf("AuditClientIP = %q, want forwarded client", got)
	}
}

func TestAuditClientIPFallsBackToRealIPFromLocalProxy(t *testing.T) {
	c := testIPContext("172.18.0.1:4567")
	c.Request.Header.Set("X-Real-IP", "198.51.100.21")

	if got := AuditClientIP(c); got != "198.51.100.21" {
		t.Fatalf("AuditClientIP = %q, want real ip", got)
	}
}

func TestAuditClientIPPrefersForwardedForOverRealIP(t *testing.T) {
	c := testIPContext("172.18.0.1:4567")
	c.Request.Header.Set("X-Forwarded-For", "198.51.100.20, 172.18.0.1")
	c.Request.Header.Set("X-Real-IP", "198.51.100.99")

	if got := AuditClientIP(c); got != "198.51.100.20" {
		t.Fatalf("AuditClientIP = %q, want X-Forwarded-For value", got)
	}
}

func TestAuditClientIPIgnoresSpoofedForwardedForFromPublicPeer(t *testing.T) {
	c := testIPContext("203.0.113.10:4567")
	c.Request.Header.Set("X-Forwarded-For", "198.51.100.22")

	if got := AuditClientIP(c); got != "203.0.113.10" {
		t.Fatalf("AuditClientIP = %q, want socket peer", got)
	}
}

func TestPeerIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "ipv4 with port", remoteAddr: "192.168.1.1:8080", want: "192.168.1.1"},
		{name: "ipv6 with port", remoteAddr: "[::1]:8080", want: "::1"},
		{name: "public with port", remoteAddr: "203.0.113.1:1234", want: "203.0.113.1"},
		{name: "without port", remoteAddr: "192.168.1.1", want: "192.168.1.1"},
		{name: "empty", remoteAddr: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testIPContext(tt.remoteAddr)
			if got := PeerIP(c); got != tt.want {
				t.Fatalf("PeerIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func testIPContext(remoteAddr string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	c.Request = req
	return c
}
