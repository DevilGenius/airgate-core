package middleware

import (
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

// PeerIP returns the socket peer IP without consulting forwarded headers.
func PeerIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

// AuditClientIP returns the best client IP for logs and audit records.
//
// Public security decisions should use PeerIP instead. Forwarded headers are
// accepted here only when the immediate peer is a local/private address, which
// covers common Docker and reverse-proxy deployments without making public
// direct connections authoritative for X-Forwarded-For.
func AuditClientIP(c *gin.Context) string {
	peer := PeerIP(c)
	if peer == "" || !isLocalProxyPeer(peer) {
		return peer
	}
	if ip := firstForwardedFor(c.Request.Header.Get("X-Forwarded-For")); ip != "" {
		return ip
	}
	if ip := normalizeIP(c.Request.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	return peer
}

func firstForwardedFor(value string) string {
	for _, part := range strings.Split(value, ",") {
		if ip := normalizeIP(part); ip != "" {
			return ip
		}
	}
	return ""
}

func normalizeIP(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

func isLocalProxyPeer(value string) bool {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}
