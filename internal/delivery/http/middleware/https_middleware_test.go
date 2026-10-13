package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireHTTPSRejectsUntrustedPlainHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequireHTTPS(true, []string{"10.0.0.0/8"}))
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("expected %d, got %d", http.StatusUpgradeRequired, response.Code)
	}
	if response.Header().Get("Location") != "" {
		t.Fatal("plain HTTP rejection must not redirect using the request Host header")
	}
}

func TestRequireHTTPSAcceptsTrustedTLSProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequireHTTPS(true, []string{"10.0.0.0/8"}))
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected trusted TLS proxy request to pass, got %d", response.Code)
	}
}
