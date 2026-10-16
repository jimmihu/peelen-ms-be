package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/infrastructure/storage"
	"peelen-ms-be/internal/usecase"
)

type WorkflowHandler struct {
	usecase       usecase.WorkflowUsecase
	objectStorage storage.ObjectStorage
	// location interprets photo EXIF times that carry no UTC offset.
	location *time.Location
}

func NewWorkflowHandler(workflowUsecase usecase.WorkflowUsecase, objectStorage storage.ObjectStorage, location *time.Location) *WorkflowHandler {
	if location == nil {
		location = time.UTC
	}
	return &WorkflowHandler{usecase: workflowUsecase, objectStorage: objectStorage, location: location}
}

func (h *WorkflowHandler) ListSites(c *gin.Context) {
	result, err := h.usecase.ListSites(c.Request.Context(), currentUser(c))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sites": result})
}

func (h *WorkflowHandler) GetSite(c *gin.Context) {
	result, err := h.usecase.GetSite(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site": result})
}

func (h *WorkflowHandler) CreateSite(c *gin.Context) {
	var input entity.Site
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.CreateSite(c.Request.Context(), currentUser(c), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"site": result})
}

func (h *WorkflowHandler) UpdateSite(c *gin.Context) {
	var input entity.Site
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.UpdateSite(c.Request.Context(), currentUser(c), c.Param("id"), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"site": result})
}

func (h *WorkflowHandler) DeleteSite(c *gin.Context) {
	if err := h.usecase.DeleteSite(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *WorkflowHandler) ListVendors(c *gin.Context) {
	result, err := h.usecase.ListVendors(c.Request.Context(), currentUser(c), entity.VendorFilter{
		SiteID: c.Query("site_id"), Status: c.Query("status"), Query: strings.TrimSpace(c.Query("q")),
	})
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"vendors": result})
}

func (h *WorkflowHandler) GetVendor(c *gin.Context) {
	result, err := h.usecase.GetVendor(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"vendor": result})
}

func (h *WorkflowHandler) CreateVendor(c *gin.Context) {
	var input entity.Vendor
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.CreateVendor(c.Request.Context(), currentUser(c), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"vendor": result})
}

func (h *WorkflowHandler) UpdateVendor(c *gin.Context) {
	var input entity.Vendor
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.UpdateVendor(c.Request.Context(), currentUser(c), c.Param("id"), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"vendor": result})
}

func (h *WorkflowHandler) DeleteVendor(c *gin.Context) {
	if err := h.usecase.DeleteVendor(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *WorkflowHandler) ListShifts(c *gin.Context) {
	result, err := h.usecase.ListShifts(c.Request.Context(), currentUser(c))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"shifts": result})
}

func (h *WorkflowHandler) GetShift(c *gin.Context) {
	result, err := h.usecase.GetShift(c.Request.Context(), currentUser(c), c.Param("shift"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *WorkflowHandler) CreateShift(c *gin.Context) {
	var input entity.Shift
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.CreateShift(c.Request.Context(), currentUser(c), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *WorkflowHandler) UpdateShift(c *gin.Context) {
	var input entity.Shift
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.UpdateShift(c.Request.Context(), currentUser(c), c.Param("shift"), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *WorkflowHandler) DeleteShift(c *gin.Context) {
	if err := h.usecase.DeleteShift(c.Request.Context(), currentUser(c), c.Param("shift")); err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *WorkflowHandler) MyTasks(c *gin.Context) {
	// Without a date the repository uses today at each task's site.
	date := c.Query("date")
	tasks, err := h.usecase.ListMyTasks(c.Request.Context(), currentUser(c), date)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "tasks": tasks})
}

func (h *WorkflowHandler) ListPipelineReports(c *gin.Context) {
	filter, err := reportFilterFromQuery(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REPORT_FILTER", err.Error())
		return
	}
	result, total, err := h.usecase.ListPipelineReports(c.Request.Context(), currentUser(c), filter)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	for i := range result {
		h.signReportEvidence(c, &result[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": result, "page": filter.Page, "limit": filter.Limit, "total": total})
}

func (h *WorkflowHandler) GetPipelineReport(c *gin.Context) {
	result, err := h.usecase.GetPipelineReport(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	h.signReportEvidence(c, result)
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// signReportEvidence adds signed download URLs for the report's evidence photos
// (stored as object keys). A photo that cannot be signed keeps only its key.
func (h *WorkflowHandler) signReportEvidence(c *gin.Context, report *entity.PipelineReport) {
	if h.objectStorage == nil || report == nil {
		return
	}
	for _, photo := range []struct{ key, url **string }{
		{&report.EvidenceBefore, &report.EvidenceBeforeURL},
		{&report.EvidenceAfter, &report.EvidenceAfterURL},
	} {
		if *photo.key == nil || **photo.key == "" {
			continue
		}
		signed, err := h.objectStorage.SignedDownloadURL(c.Request.Context(), **photo.key, policyDownloadExpiry)
		if err != nil {
			_ = c.Error(err)
			continue
		}
		*photo.url = &signed
	}
}

func (h *WorkflowHandler) CreatePipelineReport(c *gin.Context) {
	input, uploaded, ok := h.bindPipelineReportInput(c)
	defer removeMultipartForm(c)
	if !ok {
		return
	}
	result, err := h.usecase.CreatePipelineReport(c.Request.Context(), currentUser(c), input)
	if err != nil {
		h.removeUploadedEvidence(uploaded)
		handleWorkflowError(c, err)
		return
	}
	h.signReportEvidence(c, result)
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *WorkflowHandler) UpdatePipelineReport(c *gin.Context) {
	input, uploaded, ok := h.bindPipelineReportInput(c)
	defer removeMultipartForm(c)
	if !ok {
		return
	}
	result, err := h.usecase.UpdatePipelineReport(c.Request.Context(), currentUser(c), c.Param("id"), input)
	if err != nil {
		h.removeUploadedEvidence(uploaded)
		handleWorkflowError(c, err)
		return
	}
	h.signReportEvidence(c, result)
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *WorkflowHandler) ListInventoryReports(c *gin.Context) {
	filter, err := reportFilterFromQuery(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_REPORT_FILTER", err.Error())
		return
	}
	result, total, err := h.usecase.ListInventoryReports(c.Request.Context(), currentUser(c), filter)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result, "page": filter.Page, "limit": filter.Limit, "total": total})
}

func (h *WorkflowHandler) GetInventoryReport(c *gin.Context) {
	result, err := h.usecase.GetInventoryReport(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *WorkflowHandler) GetInventoryReportEvidence(c *gin.Context) {
	if h.objectStorage == nil {
		writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
		return
	}
	report, err := h.usecase.GetInventoryReport(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	if report.Evidence == nil || strings.TrimSpace(*report.Evidence) == "" {
		writeError(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Inventory report evidence not found")
		return
	}
	url, err := h.objectStorage.SignedDownloadURL(c.Request.Context(), *report.Evidence, policyDownloadExpiry)
	if err != nil {
		_ = c.Error(err)
		writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to generate evidence URL")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"url": url, "expires_in": int(policyDownloadExpiry.Seconds())}})
}

func (h *WorkflowHandler) CreateInventoryReport(c *gin.Context) {
	input, uploaded, ok := h.bindInventoryReportInput(c)
	defer removeMultipartForm(c)
	if !ok {
		return
	}
	result, err := h.usecase.CreateInventoryReport(c.Request.Context(), currentUser(c), input)
	if err != nil {
		h.removeUploadedEvidence(uploaded)
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *WorkflowHandler) ReviewInventoryReport(c *gin.Context) {
	var input struct {
		Status       string  `json:"status" binding:"required"`
		ReviewReason *string `json:"review_reason"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.ReviewInventoryReport(
		c.Request.Context(), currentUser(c), c.Param("id"), input.Status, input.ReviewReason,
	)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *WorkflowHandler) DashboardReport(c *gin.Context) {
	result, err := h.usecase.ReportDashboard(
		c.Request.Context(), currentUser(c), c.Query("date"), c.Query("site_id"), c.Query("vendor_id"),
	)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *WorkflowHandler) DailyReport(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		date = time.Now().In(h.location).Format("2006-01-02")
	}
	result, err := h.usecase.DailyReport(
		c.Request.Context(), currentUser(c), date, c.Query("site_id"), c.Query("vendor_id"),
	)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "data": result})
}

func reportFilterFromQuery(c *gin.Context) (entity.ReportFilter, error) {
	page, limit := 1, 20
	var err error
	if value := c.Query("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 {
			return entity.ReportFilter{}, errors.New("page must be positive")
		}
	}
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return entity.ReportFilter{}, errors.New("limit must be between 1 and 100")
		}
	}
	return entity.ReportFilter{
		VendorID: c.Query("vendor_id"), SiteID: c.Query("site_id"), UserID: c.Query("user_id"),
		Status: strings.ToUpper(c.Query("status")), From: c.Query("from"), To: c.Query("to"),
		Page: page, Limit: limit,
	}, nil
}

func handleWorkflowError(c *gin.Context, err error) {
	_ = c.Error(err)
	switch {
	case errors.Is(err, usecase.ErrForbidden):
		writeError(c, http.StatusForbidden, "AUTH_FORBIDDEN", "You are not allowed to perform this action")
	case errors.Is(err, usecase.ErrNotFound), errors.Is(err, repository.ErrNotFound), errors.Is(err, entity.ErrInvalidID):
		writeError(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Resource not found")
	case errors.Is(err, usecase.ErrInvalidOperationalData), errors.Is(err, entity.ErrInvalidOperationalData):
		writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", err.Error())
	case errors.Is(err, repository.ErrConflict):
		writeError(c, http.StatusConflict, "RESOURCE_CONFLICT", "The resource already exists or violates a constraint")
	case errors.Is(err, entity.ErrReportUploadClosed):
		writeError(c, http.StatusConflict, "REPORT_UPLOAD_CLOSED", err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to complete the request")
	}
}
