package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func RequireHTTPS(enforce bool, trustedProxyCIDRs []string) gin.HandlerFunc {
	trustedRanges := make([]*net.IPNet, 0, len(trustedProxyCIDRs))
	for _, cidr := range trustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			trustedRanges = append(trustedRanges, network)
		}
	}
	return func(c *gin.Context) {
		if !enforce || c.Request.TLS != nil || requestCameFromTLSProxy(c.Request, trustedRanges) {
			c.Next()
			return
		}
		c.Header("Upgrade", "TLS/1.2")
		c.Set(errorCodeContextKey, "HTTPS_REQUIRED")
		c.AbortWithStatusJSON(426, gin.H{
			"error": gin.H{"code": "HTTPS_REQUIRED", "message": "This endpoint requires HTTPS"},
		})
	}
}

func requestCameFromTLSProxy(request *http.Request, trustedRanges []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	for _, network := range trustedRanges {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
