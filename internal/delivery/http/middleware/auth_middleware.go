package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/usecase"
)

type AuthMiddleware struct {
	authUsecase usecase.AuthUsecase
}

func NewAuthMiddleware(authUsecase usecase.AuthUsecase) *AuthMiddleware {
	return &AuthMiddleware{authUsecase: authUsecase}
}

func (m *AuthMiddleware) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearerToken(c)
		if token == "" {
			abortError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "No token provided")
			return
		}
		user, err := m.authUsecase.ValidateToken(c.Request.Context(), token)
		if err != nil {
			_ = c.Error(err)
			abortError(c, http.StatusUnauthorized, "AUTH_INVALID_TOKEN", "Invalid or expired token")
			return
		}
		if user.MustChangePassword && c.Request.URL.Path != "/api/v1/auth/password" &&
			c.Request.URL.Path != "/api/v1/auth/me" &&
			c.Request.URL.Path != "/api/v1/auth/logout" &&
			c.Request.URL.Path != "/api/v1/auth/logout-all" {
			abortError(c, http.StatusForbidden, "AUTH_PASSWORD_CHANGE_REQUIRED", "Change your password before continuing")
			return
		}
		c.Set("user", user)
		c.Set("token", token)
		c.Set("user_id", user.ID)
		c.Set("roles", user.Roles)
		c.Set("site_ids", user.Sites)
		c.Set("vendor_ids", user.Vendors)
		c.Next()
	}
}

func (m *AuthMiddleware) RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		roles, exists := c.Get("roles")
		if !exists {
			abortError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "User is not authenticated")
			return
		}
		current, ok := roles.([]string)
		if !ok {
			abortError(c, http.StatusForbidden, "AUTH_FORBIDDEN", "Role authorization failed")
			return
		}
		for _, role := range current {
			for _, allowed := range allowedRoles {
				if role == allowed {
					c.Next()
					return
				}
			}
		}
		abortError(c, http.StatusForbidden, "AUTH_FORBIDDEN", "You are not allowed to perform this action")
	}
}

func abortError(c *gin.Context, status int, code, message string) {
	c.Set(errorCodeContextKey, code)
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"code": code, "message": message},
	})
}

func extractBearerToken(c *gin.Context) string {
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
