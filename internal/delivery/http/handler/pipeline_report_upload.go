package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/infrastructure/exif"
)

const maxPipelineEvidenceSize = 5 << 20

// bindPipelineReportInput reads a pipeline report request as JSON, or as
// multipart/form-data carrying evidence_before / evidence_after photos.
// Uploaded photos are stored in object storage, and their EXIF capture time
// becomes open_date (before) / closed_date (after), so a report sent late for
// lack of signal still records when the work was actually done. The returned
// keys must be removed by the caller if the report is not saved.
func (h *WorkflowHandler) bindPipelineReportInput(c *gin.Context) (entity.PipelineReportInput, []string, bool) {
	var input entity.PipelineReportInput
	if !strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return input, nil, false
		}
		return input, nil, true
	}
	if err := parseMultipartRequest(c, 2*maxPipelineEvidenceSize); err != nil {
		handleMultipartError(c, err)
		return input, nil, false
	}
	input.PipelineItemID = formValue(c, "pipeline_item_id")
	input.Shift = formValue(c, "shift")
	input.ReportDate = formValue(c, "report_date")
	input.Status = formValue(c, "status")
	input.NoteCategory = formString(c, "note_category")
	input.Notes = formString(c, "notes")
	input.Reason = formString(c, "reason")
	var err error
	if input.OpenDate, err = formTime(c, "open_date"); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", "open_date must be an RFC 3339 timestamp")
		return input, nil, false
	}
	if input.ClosedDate, err = formTime(c, "closed_date"); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", "closed_date must be an RFC 3339 timestamp")
		return input, nil, false
	}

	var keys []string
	for _, photo := range []struct {
		field    string
		evidence **string
		taken    **time.Time
	}{
		{"evidence_before", &input.EvidenceBefore, &input.OpenDate},
		{"evidence_after", &input.EvidenceAfter, &input.ClosedDate},
	} {
		upload, err := readUpload(c, photo.field, maxPipelineEvidenceSize, isSupportedImage)
		if err != nil {
			h.removeUploadedEvidence(keys)
			handleMultipartError(c, err)
			return input, nil, false
		}
		if upload == nil {
			continue
		}
		if h.objectStorage == nil {
			h.removeUploadedEvidence(keys)
			writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
			return input, nil, false
		}
		key, err := newObjectKey("pipeline-reports", upload.extension)
		if err != nil {
			h.removeUploadedEvidence(keys)
			handleWorkflowError(c, err)
			return input, nil, false
		}
		if err := h.objectStorage.Upload(c.Request.Context(), key, upload.contentType, upload.data); err != nil {
			h.removeUploadedEvidence(keys)
			_ = c.Error(err)
			writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to upload evidence photo")
			return input, nil, false
		}
		keys = append(keys, key)
		*photo.evidence = &key
		// The camera's capture time wins over a client-supplied timestamp.
		if captured, ok := exif.CaptureTime(upload.data, h.location); ok {
			*photo.taken = &captured
		}
	}
	return input, keys, true
}

func (h *WorkflowHandler) removeUploadedEvidence(keys []string) {
	for _, key := range keys {
		deleteObjectAfterFailure(h.objectStorage, key)
	}
}

func formValue(c *gin.Context, key string) string {
	if value := formString(c, key); value != nil {
		return *value
	}
	return ""
}

func formTime(c *gin.Context, key string) (*time.Time, error) {
	value := formValue(c, key)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New(key + " must be an RFC 3339 timestamp")
	}
	return &parsed, nil
}
