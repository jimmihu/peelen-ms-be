package handler

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
	"peelen-ms-be/internal/usecase"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type AdminHandler struct {
	adminUsecase usecase.AdminUsecase
}

func NewAdminHandler(adminUsecase usecase.AdminUsecase) *AdminHandler {
	return &AdminHandler{adminUsecase: adminUsecase}
}

func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.adminUsecase.ListUsers(c.Request.Context(), currentUser(c))
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req usecase.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	user, temporaryPassword, err := h.adminUsecase.CreateUser(
		c.Request.Context(), currentUser(c), req, requestIPAddress(c),
	)
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"user": user, "temporary_password": temporaryPassword,
		"message": "User created. The user must change this password on first login.",
	})
}

func (h *AdminHandler) GetUser(c *gin.Context) {
	if !validTargetID(c) {
		return
	}
	user, err := h.adminUsecase.GetUser(c.Request.Context(), currentUser(c), c.Param("id"))
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *AdminHandler) UpdateUser(c *gin.Context) {
	if !validTargetID(c) {
		return
	}
	var req usecase.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	user, err := h.adminUsecase.UpdateUser(
		c.Request.Context(), currentUser(c), c.Param("id"), req, requestIPAddress(c),
	)
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *AdminHandler) SetRoles(c *gin.Context) {
	if !validTargetID(c) {
		return
	}
	var req usecase.AuthorizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	if len(req.Roles) == 0 {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "roles must contain at least one role")
		return
	}
	updateScopes := req.SiteIDs != nil || req.VendorIDs != nil
	if err := h.adminUsecase.SetAuthorization(
		c.Request.Context(), currentUser(c), c.Param("id"), req, true, updateScopes, requestIPAddress(c),
	); err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "User roles updated"})
}

func (h *AdminHandler) SetScopes(c *gin.Context) {
	if !validTargetID(c) {
		return
	}
	var req usecase.AuthorizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	if err := h.adminUsecase.SetAuthorization(
		c.Request.Context(), currentUser(c), c.Param("id"), req, req.Roles != nil, true, requestIPAddress(c),
	); err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "User scopes updated"})
}

func (h *AdminHandler) ResetPassword(c *gin.Context) {
	if !validTargetID(c) {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	password, err := h.adminUsecase.ResetPassword(
		c.Request.Context(), currentUser(c), c.Param("id"), req.Password, requestIPAddress(c),
	)
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"temporary_password": password,
		"message":            "Password reset. The user must change it on next login.",
	})
}

func (h *AdminHandler) RevokeSessions(c *gin.Context) {
	if !validTargetID(c) {
		return
	}
	if err := h.adminUsecase.RevokeSessions(c.Request.Context(), currentUser(c), c.Param("id"), requestIPAddress(c)); err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "All user sessions revoked"})
}

func (h *AdminHandler) ListRoles(c *gin.Context) {
	roles, err := h.adminUsecase.ListRoles(c.Request.Context(), currentUser(c))
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

func (h *AdminHandler) ListAuditLogs(c *gin.Context) {
	var from, to *time.Time
	if value := c.Query("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "from must be an RFC3339 timestamp")
			return
		}
		from = &parsed
	}
	if value := c.Query("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "to must be an RFC3339 timestamp")
			return
		}
		to = &parsed
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	logs, err := h.adminUsecase.ListAuditLogs(
		c.Request.Context(), currentUser(c), c.Query("user_id"), c.Query("action"),
		c.Query("entity"), from, to, limit,
	)
	if err != nil {
		handleAdminError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"audit_logs": logs})
}

func currentUser(c *gin.Context) *entity.User {
	user, _ := c.Get("user")
	value, _ := user.(*entity.User)
	return value
}

func validTargetID(c *gin.Context) bool {
	if uuidPattern.MatchString(c.Param("id")) {
		return true
	}
	writeError(c, http.StatusNotFound, "AUTH_NOT_FOUND", "User not found")
	return false
}

func handleAdminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrForbidden):
		writeError(c, http.StatusForbidden, "AUTH_FORBIDDEN", "You are not allowed to perform this action")
	case errors.Is(err, usecase.ErrNotFound):
		writeError(c, http.StatusNotFound, "AUTH_NOT_FOUND", "Resource not found")
	case errors.Is(err, usecase.ErrInvalidScope):
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_SCOPE", err.Error())
	case errors.Is(err, repository.ErrConflict):
		writeError(c, http.StatusConflict, "AUTH_USER_EXISTS", "Username or email already exists")
	case errors.Is(err, usecase.ErrUsernamePolicy), errors.Is(err, usecase.ErrPasswordPolicy):
		writeError(c, http.StatusBadRequest, "AUTH_VALIDATION_ERROR", err.Error())
	case errors.Is(err, usecase.ErrInvalidUserData):
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_USER_DATA", err.Error())
	default:
		writeError(c, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "Unable to complete the request")
	}
}
