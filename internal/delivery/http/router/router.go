package router

import (
	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/delivery/http/handler"
	"peelen-ms-be/internal/delivery/http/middleware"
	"peelen-ms-be/internal/domain/entity"
)

type Config struct {
	AuthHandler        *handler.AuthHandler
	AdminHandler       *handler.AdminHandler
	OperationalHandler *handler.OperationalHandler
	WorkflowHandler    *handler.WorkflowHandler
	AssignmentHandler  *handler.AssignmentHandler
	ScoringHandler     *handler.ScoringHandler
	DashboardHandler   *handler.DashboardHandler
	AuthMiddleware     *middleware.AuthMiddleware
	EnforceHTTPS       bool
	TrustedProxyCIDRs  []string
}

func NewRouter(cfg Config) *gin.Engine {
	router := gin.Default()
	router.Use(middleware.ErrorLogger())
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

	operations := v1.Group("")
	operations.Use(cfg.AuthMiddleware.Authenticate())
	sites := operations.Group("/sites")
	sites.GET("", cfg.WorkflowHandler.ListSites)
	sites.POST("", cfg.WorkflowHandler.CreateSite)
	sites.GET("/:id", cfg.WorkflowHandler.GetSite)
	sites.PATCH("/:id", cfg.WorkflowHandler.UpdateSite)
	sites.DELETE("/:id", cfg.WorkflowHandler.DeleteSite)

	vendors := operations.Group("/vendors")
	vendors.GET("", cfg.WorkflowHandler.ListVendors)
	vendors.POST("", cfg.WorkflowHandler.CreateVendor)
	vendors.GET("/:id", cfg.WorkflowHandler.GetVendor)
	vendors.PATCH("/:id", cfg.WorkflowHandler.UpdateVendor)
	vendors.DELETE("/:id", cfg.WorkflowHandler.DeleteVendor)

	operations.GET("/me/task", cfg.WorkflowHandler.MyTasks)
	operations.GET("/me/tasks", cfg.WorkflowHandler.MyTasks)
	operations.GET("/shifts", cfg.WorkflowHandler.ListShifts)
	operations.POST("/shifts", cfg.WorkflowHandler.CreateShift)
	operations.GET("/shifts/:shift", cfg.WorkflowHandler.GetShift)
	operations.PUT("/shifts/:shift", cfg.WorkflowHandler.UpdateShift)
	operations.DELETE("/shifts/:shift", cfg.WorkflowHandler.DeleteShift)

	assignments := operations.Group("/assignments")
	assignments.GET("", cfg.AssignmentHandler.ListAssignments)
	assignments.POST("", cfg.AssignmentHandler.CreateAssignment)
	assignments.GET("/:id", cfg.AssignmentHandler.GetAssignment)
	assignments.PATCH("/:id", cfg.AssignmentHandler.UpdateAssignment)
	assignments.DELETE("/:id", cfg.AssignmentHandler.DeleteAssignment)

	pipelineReports := operations.Group("/pipeline-reports")
	pipelineReports.GET("", cfg.WorkflowHandler.ListPipelineReports)
	pipelineReports.POST("", cfg.WorkflowHandler.CreatePipelineReport)
	pipelineReports.GET("/:id", cfg.WorkflowHandler.GetPipelineReport)
	pipelineReports.PATCH("/:id", cfg.WorkflowHandler.UpdatePipelineReport)

	inventoryReports := operations.Group("/inventory-report")
	inventoryReports.GET("", cfg.WorkflowHandler.ListInventoryReports)
	inventoryReports.POST("", cfg.WorkflowHandler.CreateInventoryReport)
	inventoryReports.GET("/:id", cfg.WorkflowHandler.GetInventoryReport)
	inventoryReports.PATCH("/:id/review", cfg.WorkflowHandler.ReviewInventoryReport)
	operations.GET("/inventory-reports", cfg.WorkflowHandler.ListInventoryReports)
	operations.POST("/inventory-reports", cfg.WorkflowHandler.CreateInventoryReport)
	operations.GET("/inventory-reports/:id", cfg.WorkflowHandler.GetInventoryReport)
	operations.PATCH("/inventory-reports/:id/review", cfg.WorkflowHandler.ReviewInventoryReport)

	operations.GET("/dashboard/report", cfg.WorkflowHandler.DashboardReport)
	operations.GET("/reports/daily", cfg.WorkflowHandler.DailyReport)

	inventories := operations.Group("/inventories")
	inventories.GET("", cfg.OperationalHandler.List(entity.ResourceInventory))
	inventories.POST("", cfg.OperationalHandler.CreateInventory())
	inventories.GET("/:id", cfg.OperationalHandler.Get(entity.ResourceInventory))
	inventories.PATCH("/:id", cfg.OperationalHandler.UpdateInventory())
	inventories.DELETE("/:id", cfg.OperationalHandler.Delete(entity.ResourceInventory))

	policies := operations.Group("/policies")
	policies.GET("", cfg.OperationalHandler.List(entity.ResourcePolicy))
	policies.POST("", cfg.OperationalHandler.CreatePolicy())
	policies.GET("/:id/file", cfg.OperationalHandler.PolicyFile())
	policies.GET("/:id", cfg.OperationalHandler.Get(entity.ResourcePolicy))
	policies.PATCH("/:id", cfg.OperationalHandler.UpdatePolicy())
	policies.DELETE("/:id", cfg.OperationalHandler.Delete(entity.ResourcePolicy))

	items := operations.Group("/items")
	items.GET("", cfg.OperationalHandler.List(entity.ResourceItem))
	items.POST("", cfg.OperationalHandler.Create(entity.ResourceItem))
	items.GET("/:id", cfg.OperationalHandler.Get(entity.ResourceItem))
	items.PATCH("/:id", cfg.OperationalHandler.Update(entity.ResourceItem))
	items.DELETE("/:id", cfg.OperationalHandler.Delete(entity.ResourceItem))

	pipelines := operations.Group("/pipelines")
	pipelines.GET("", cfg.OperationalHandler.List(entity.ResourcePipeline))
	pipelines.POST("", cfg.OperationalHandler.Create(entity.ResourcePipeline))
	pipelines.GET("/:id", cfg.OperationalHandler.Get(entity.ResourcePipeline))
	pipelines.PATCH("/:id", cfg.OperationalHandler.Update(entity.ResourcePipeline))
	pipelines.DELETE("/:id", cfg.OperationalHandler.Delete(entity.ResourcePipeline))
	pipelines.GET("/:id/items", cfg.OperationalHandler.ListPipelineItems())
	pipelines.POST("/:id/items", cfg.OperationalHandler.CreatePipelineItem())

	pipelineItems := operations.Group("/pipeline-items")
	pipelineItems.GET("/:id", cfg.OperationalHandler.Get(entity.ResourcePipelineItem))
	pipelineItems.PATCH("/:id", cfg.OperationalHandler.Update(entity.ResourcePipelineItem))
	pipelineItems.DELETE("/:id", cfg.OperationalHandler.Delete(entity.ResourcePipelineItem))
	for _, kind := range []struct {
		code, rules, results string
	}{
		{entity.RuleKindSLA, "/sla-rules", "/sla-scores"},
		{entity.RuleKindMaturity, "/maturity-rules", "/maturity-levels"},
	} {
		catalog := "/sla-catalog"
		if kind.code == entity.RuleKindMaturity {
			catalog = "/maturity-catalog"
		}
		operations.GET(catalog, cfg.ScoringHandler.Catalog(kind.code))
		rules := operations.Group(kind.rules)
		rules.GET("", cfg.ScoringHandler.ListRules(kind.code, false))
		rules.GET("/history", cfg.ScoringHandler.ListRules(kind.code, true))
		rules.POST("", cfg.ScoringHandler.CreateRule(kind.code))
		rules.GET("/:id", cfg.ScoringHandler.GetRule)
		rules.DELETE("/:id", cfg.ScoringHandler.DeleteRule)
		operations.GET(kind.results+"/live", cfg.ScoringHandler.Live(kind.code))
		operations.GET(kind.results, cfg.ScoringHandler.History(kind.code))
	}

	operations.GET("/dashboard/summary", cfg.DashboardHandler.GetDashboard)

	return router
}
