package router

import (
	"testing"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/delivery/http/handler"
	"peelen-ms-be/internal/delivery/http/middleware"
)

func TestWorkflowRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(Config{
		AuthHandler:        &handler.AuthHandler{},
		AdminHandler:       &handler.AdminHandler{},
		OperationalHandler: &handler.OperationalHandler{},
		WorkflowHandler:    handler.NewWorkflowHandler(nil),
		AssignmentHandler:  handler.NewAssignmentHandler(nil),
		AuthMiddleware:     middleware.NewAuthMiddleware(nil),
	})
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	required := []string{
		"GET /api/v1/sites", "POST /api/v1/sites", "GET /api/v1/sites/:id",
		"PATCH /api/v1/sites/:id", "DELETE /api/v1/sites/:id",
		"GET /api/v1/vendors", "POST /api/v1/vendors", "GET /api/v1/vendors/:id",
		"PATCH /api/v1/vendors/:id", "DELETE /api/v1/vendors/:id",
		"GET /api/v1/me/task", "GET /api/v1/shifts", "POST /api/v1/shifts", "GET /api/v1/shifts/:shift",
		"PUT /api/v1/shifts/:shift", "DELETE /api/v1/shifts/:shift",
		"GET /api/v1/pipeline-reports", "POST /api/v1/pipeline-reports",
		"GET /api/v1/pipeline-reports/:id", "PATCH /api/v1/pipeline-reports/:id",
		"GET /api/v1/inventory-report", "POST /api/v1/inventory-report", "GET /api/v1/inventory-report/:id",
		"PATCH /api/v1/inventory-report/:id/review", "GET /api/v1/dashboard/report",
		"GET /api/v1/reports/daily",
		"GET /api/v1/assignments", "POST /api/v1/assignments", "GET /api/v1/assignments/:id",
		"PATCH /api/v1/assignments/:id", "DELETE /api/v1/assignments/:id",
	}
	for _, route := range required {
		if !registered[route] {
			t.Errorf("required route is not registered: %s", route)
		}
	}
}
