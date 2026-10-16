
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

	siteID, vendorID, date, ok := dashboardQuery(c)
	if !ok {
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

func (h *DashboardHandler) GetDashboardDetail(c *gin.Context) {
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

	detailType := strings.TrimSpace(c.Query("type"))
	switch detailType {
	case "vendor_progress", "inventory_reports", "safety_reports":
		// Jenis detail yang didukung.
	default:
		writeError(
			c,
			http.StatusBadRequest,
			"Invalid Data",
			"type must be one of: vendor_progress, inventory_reports, safety_reports",
		)
		return
	}

	siteID, vendorID, date, ok := dashboardQuery(c)
	if !ok {
		return
	}

	result, err := h.usecase.GetDashboardDetail(
		c.Request.Context(),
		actor,
		siteID,
		vendorID,
		date,
		detailType,
	)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": result,
	})
}

func dashboardQuery(c *gin.Context) (string, string, string, bool) {
	siteID := strings.TrimSpace(c.Query("site_id"))
	vendorID := strings.TrimSpace(c.Query("vendor_id"))
	date := strings.TrimSpace(c.Query("date"))

	if date == "" {
		loc, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			loc = time.FixedZone("WIB", 7*60*60)
		}

		date = time.Now().In(loc).Format("2006-01-02")
	}

	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil || parsedDate.Format("2006-01-02") != date {
		writeError(
			c,
			http.StatusBadRequest,
			"Invalid Data",
			"date must use YYYY-MM-DD format",
		)
		return "", "", "", false
	}

	if siteID != "" && !isDashboardUUID(siteID) {
		writeError(
			c,
			http.StatusBadRequest,
			"Invalid Data",
			"site_id must be a valid UUID",
		)
		return "", "", "", false
	}

	if vendorID != "" && !isDashboardUUID(vendorID) {
		writeError(
			c,
			http.StatusBadRequest,
			"Invalid Data",
			"vendor_id must be a valid UUID",
		)
		return "", "", "", false
	}

	return siteID, vendorID, date, true
}

func isDashboardUUID(value string) bool {
	const uuidPattern = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

	if len(value) != len(uuidPattern) {
		return false
	}

	for i := range uuidPattern {
		switch uuidPattern[i] {
		case 'x':
			c := value[i]
			if !((c >= '0' && c <= '9') ||
				(c >= 'a' && c <= 'f') ||
				(c >= 'A' && c <= 'F')) {
				return false
			}
		case '-':
			if value[i] != '-' {
				return false
			}
		}
	}

	return true
}
