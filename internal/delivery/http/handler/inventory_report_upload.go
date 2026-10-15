package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
)

const maxInventoryReportEvidenceSize = 5 << 20

func (h *WorkflowHandler) bindInventoryReportInput(c *gin.Context) (entity.InventoryReportInput, []string, bool) {
	var input entity.InventoryReportInput
	if !strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return input, nil, false
		}
		return input, nil, true
	}

	if err := parseMultipartRequest(c, maxInventoryReportEvidenceSize); err != nil {
		handleMultipartError(c, err)
		return input, nil, false
	}

	input.VendorID = formValue(c, "vendor_id")
	if inventoryID := formValue(c, "inventory_id"); inventoryID != "" && !strings.EqualFold(inventoryID, "null") {
		input.InventoryID = &inventoryID
	}
	input.ReportType = strings.ToUpper(formValue(c, "report_type"))
	input.Description = formValue(c, "description")
	input.Evidence = formString(c, "evidence")

	upload, err := readUpload(c, "file", maxInventoryReportEvidenceSize, isInventoryReportEvidence)
	if err != nil {
		handleMultipartError(c, err)
		return input, nil, false
	}
	if upload == nil {
		return input, nil, true
	}
	if h.objectStorage == nil {
		writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
		return input, nil, false
	}
	key, err := newObjectKey("inventory-reports", upload.extension)
	if err != nil {
		handleWorkflowError(c, err)
		return input, nil, false
	}
	if err := h.objectStorage.Upload(c.Request.Context(), key, upload.contentType, upload.data); err != nil {
		_ = c.Error(err)
		writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to upload inventory report evidence")
		return input, nil, false
	}
	input.Evidence = &key
	return input, []string{key}, true
}

func isInventoryReportEvidence(data []byte) (string, string, bool) {
	return isSupportedImage(data)
}
