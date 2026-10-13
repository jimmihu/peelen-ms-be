package router

import (
	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/delivery/http/handler"
	"peelen-ms-be/internal/delivery/http/middleware"
)

type Config struct {
	AuthHandler       *handler.AuthHandler
	AdminHandler      *handler.AdminHandler
	AuthMiddleware    *middleware.AuthMiddleware
	EnforceHTTPS      bool
	TrustedProxyCIDRs []string
}

func NewRouter(cfg Config) *gin.Engine {
	router := gin.Default()
	router.Use(middleware.RequireHTTPS(cfg.EnforceHTTPS, cfg.TrustedProxyCIDRs))
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	v1 := router.Group("/api/v1")
	auth := v1.Group("/auth")
	auth.POST("/login", cfg.AuthHandler.Login)
	auth.POST("/refresh", cfg.AuthHandler.Refresh)

	protected := auth.Group("")
	protected.Use(cfg.AuthMiddleware.Authenticate())
	protected.POST("/logout", cfg.AuthHandler.Logout)
	protected.POST("/logout-all", cfg.AuthHandler.LogoutAll)
	protected.GET("/me", cfg.AuthHandler.Me)
	protected.PUT("/password", cfg.AuthHandler.ChangePassword)

	admin := v1.Group("")
	admin.Use(cfg.AuthMiddleware.Authenticate())
	users := admin.Group("/users")
	users.Use(cfg.AuthMiddleware.RequireRole("SUPER_ADMIN", "ADMIN_WORKER"))
	users.GET("", cfg.AdminHandler.ListUsers)
	users.POST("", cfg.AdminHandler.CreateUser)
	users.GET("/:id", cfg.AdminHandler.GetUser)
	users.PATCH("/:id", cfg.AdminHandler.UpdateUser)
	users.POST("/:id/reset-password", cfg.AdminHandler.ResetPassword)
	users.PUT("/:id/roles", cfg.AuthMiddleware.RequireRole("SUPER_ADMIN"), cfg.AdminHandler.SetRoles)
	users.PUT("/:id/scopes", cfg.AuthMiddleware.RequireRole("SUPER_ADMIN"), cfg.AdminHandler.SetScopes)
	users.DELETE("/:id/sessions", cfg.AuthMiddleware.RequireRole("SUPER_ADMIN"), cfg.AdminHandler.RevokeSessions)
	admin.GET("/roles", cfg.AuthMiddleware.RequireRole("SUPER_ADMIN"), cfg.AdminHandler.ListRoles)
	admin.GET("/audit-logs", cfg.AuthMiddleware.RequireRole("SUPER_ADMIN"), cfg.AdminHandler.ListAuditLogs)
	return router
}
