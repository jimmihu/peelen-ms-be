package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/usecase"
)

type AssignmentHandler struct {
	usecase usecase.AssignmentUsecase
}

func NewAssignmentHandler(assignmentUsecase usecase.AssignmentUsecase) *AssignmentHandler {
	return &AssignmentHandler{usecase: assignmentUsecase}
}

func (h *AssignmentHandler) ListAssignments(c *gin.Context) {
	filter, err := assignmentFilterFromQuery(c)
	if err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", err.Error())
		return
	}
	result, total, err := h.usecase.ListAssignments(c.Request.Context(), currentUser(c), filter)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	c.JSON(http.StatusOK, gin.H{"data": result, "page": filter.Page, "limit": filter.Limit, "total": total})
}

func (h *AssignmentHandler) GetAssignment(c *gin.Context) {
	result, err := h.usecase.GetAssignment(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AssignmentHandler) CreateAssignment(c *gin.Context) {
	var input entity.AssignmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.CreateAssignment(c.Request.Context(), currentUser(c), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": result})
}

func (h *AssignmentHandler) UpdateAssignment(c *gin.Context) {
	var input entity.AssignmentInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.usecase.UpdateAssignment(c.Request.Context(), currentUser(c), c.Param("id"), input)
	if err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AssignmentHandler) DeleteAssignment(c *gin.Context) {
	if err := h.usecase.DeleteAssignment(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
		handleWorkflowError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func assignmentFilterFromQuery(c *gin.Context) (entity.AssignmentFilter, error) {
	filter := entity.AssignmentFilter{
		VendorID: c.Query("vendor_id"), SiteID: c.Query("site_id"), PipelineID: c.Query("pipeline_id"),
		PipelineItemID: c.Query("pipeline_item_id"), UserID: c.Query("user_id"), Shift: c.Query("shift"),
		Page: 1, Limit: 20,
	}
	var err error
	if value := c.Query("page"); value != "" {
		filter.Page, err = strconv.Atoi(value)
		if err != nil || filter.Page < 1 {
			return entity.AssignmentFilter{}, errors.New("page must be positive")
		}
	}
	if value := c.Query("limit"); value != "" {
		filter.Limit, err = strconv.Atoi(value)
		if err != nil || filter.Limit < 1 || filter.Limit > 100 {
			return entity.AssignmentFilter{}, errors.New("limit must be between 1 and 100")
		}
	}
	if value := c.Query("is_active"); value != "" {
		active, err := strconv.ParseBool(value)
		if err != nil {
			return entity.AssignmentFilter{}, errors.New("is_active must be true or false")
		}
		filter.IsActive = &active
	}
	return filter, nil
}
