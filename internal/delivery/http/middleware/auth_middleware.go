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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "AUTH_UNAUTHORIZED", "message": "No token provided"},
			})
			return
		}
		user, err := m.authUsecase.ValidateToken(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "AUTH_INVALID_TOKEN", "message": "Invalid or expired token"},
			})
			return
		}
		if user.MustChangePassword && c.Request.URL.Path != "/api/v1/auth/password" &&
			c.Request.URL.Path != "/api/v1/auth/me" &&
			c.Request.URL.Path != "/api/v1/auth/logout" &&
			c.Request.URL.Path != "/api/v1/auth/logout-all" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"code": "AUTH_PASSWORD_CHANGE_REQUIRED", "message": "Change your password before continuing"},
			})
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "AUTH_UNAUTHORIZED", "message": "User is not authenticated"},
			})
			return
		}
		current, ok := roles.([]string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"code": "AUTH_FORBIDDEN", "message": "Role authorization failed"},
			})
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
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": gin.H{"code": "AUTH_FORBIDDEN", "message": "You are not allowed to perform this action"},
		})
	}
}

func extractBearerToken(c *gin.Context) string {
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
