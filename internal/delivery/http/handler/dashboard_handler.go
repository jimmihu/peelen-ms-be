package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/usecase"
)

type DashboardHandler struct {
	usecase usecase.DashboardUsecase
}

func NewDashboardHandler(dashboardUsecase usecase.DashboardUsecase) *DashboardHandler {
	return &DashboardHandler{
		usecase: dashboardUsecase,
	}
}

func (h *DashboardHandler) GetDashboard(c *gin.Context) {
	actor := currentUser(c)
	if actor == nil {
		writeError(
			c,
			http.StatusUnauthorized,
			"Unauthorized",
			"User authentication is required",
		)
		return
	}

	siteID := strings.TrimSpace(c.Query("site_id"))
	vendorID := strings.TrimSpace(c.Query("vendor_id"))
	date := strings.TrimSpace(c.Query("date"))

	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil || parsedDate.Format("2006-01-02") != date {
		writeError(
			c,
			http.StatusBadRequest,
			"Invalid Data",
			"date must use YYYY-MM-DD format",
		)
		return
	}

	result, err := h.usecase.GetDashboard(
		c.Request.Context(),
		actor,
		siteID,
		vendorID,
		date,
	)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": result,
	})
}
