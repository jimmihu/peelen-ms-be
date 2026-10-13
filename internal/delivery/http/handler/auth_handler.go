package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/delivery/http/middleware"
	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/usecase"
)

const refreshCookieName = "refresh_token"

type AuthHandler struct {
	authUsecase usecase.AuthUsecase
	limiter     *middleware.RateLimiter
}

func NewAuthHandler(authUsecase usecase.AuthUsecase) *AuthHandler {
	return &AuthHandler{
		authUsecase: authUsecase,
		limiter:     middleware.NewRateLimiter(10, time.Minute),
	}
}

type LoginRequest struct {
	Username string `json:"username" binding:"required,min=3,max=30"`
	Password string `json:"password" binding:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8,max=72"`
}

type TokenResponse struct {
	AccessToken string       `json:"access_token"`
	ExpiresAt   time.Time    `json:"expires_at"`
	User        *entity.User `json:"user,omitempty"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type SuccessResponse struct {
	Message string `json:"message"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	ipAddress := requestIPAddress(c)
	if !h.limiter.Allow("login:ip:"+ipAddress, "login:user:"+username) {
		writeError(c, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "Too many login attempts")
		return
	}

	result, err := h.authUsecase.Login(c.Request.Context(), username, req.Password, requestMetadata(c))
	if err != nil {
		_ = c.Error(err)
		switch {
		case errors.Is(err, usecase.ErrAccountLocked):
			writeError(c, http.StatusLocked, "AUTH_ACCOUNT_LOCKED", "Account is temporarily locked")
		case errors.Is(err, usecase.ErrInvalidCredentials):
			writeError(c, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "Username or password is incorrect")
		default:
			writeError(c, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "An error occurred during login")
		}
		return
	}
	setRefreshCookie(c, result.RefreshToken, result.RefreshExpiresAt)
	c.JSON(http.StatusOK, TokenResponse{
		AccessToken: result.AccessToken, ExpiresAt: result.ExpiresAt, User: result.User,
	})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return
		}
	}
	token := req.RefreshToken
	if token == "" {
		if cookie, err := c.Cookie(refreshCookieName); err == nil {
			token = cookie
		}
	}
	if token == "" {
		writeError(c, http.StatusUnauthorized, "AUTH_INVALID_TOKEN", "Refresh token is required")
		return
	}
	tokenHash := sha256.Sum256([]byte(token))
	if !h.limiter.Allow("refresh:ip:"+requestIPAddress(c), "refresh:token:"+hex.EncodeToString(tokenHash[:])) {
		writeError(c, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "Too many refresh attempts")
		return
	}

	result, err := h.authUsecase.Refresh(c.Request.Context(), token, requestMetadata(c))
	if err != nil {
		_ = c.Error(err)
		switch {
		case errors.Is(err, usecase.ErrRefreshReuse):
			clearRefreshCookie(c)
			writeError(c, http.StatusUnauthorized, "AUTH_REFRESH_REUSE", "Refresh token reuse detected; all sessions were revoked")
		case errors.Is(err, usecase.ErrInvalidToken):
			clearRefreshCookie(c)
			writeError(c, http.StatusUnauthorized, "AUTH_INVALID_TOKEN", "Invalid or expired refresh token")
		default:
			writeError(c, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "An error occurred while refreshing the session")
		}
		return
	}
	setRefreshCookie(c, result.RefreshToken, result.RefreshExpiresAt)
	c.JSON(http.StatusOK, TokenResponse{
		AccessToken: result.AccessToken, ExpiresAt: result.ExpiresAt, User: result.User,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	accessToken := extractToken(c)
	refreshToken := refreshTokenFromRequest(c)
	userID, _ := c.Get("user_id")
	id, _ := userID.(string)
	if err := h.authUsecase.Logout(c.Request.Context(), id, accessToken, refreshToken, requestIPAddress(c)); err != nil {
		_ = c.Error(err)
		if errors.Is(err, usecase.ErrInvalidToken) {
			writeError(c, http.StatusUnauthorized, "AUTH_INVALID_TOKEN", "Invalid or expired token")
			return
		}
		writeError(c, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "An error occurred during logout")
		return
	}
	clearRefreshCookie(c)
	c.JSON(http.StatusOK, SuccessResponse{Message: "Successfully logged out"})
}

func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID, _ := c.Get("user_id")
	id, ok := userID.(string)
	if !ok || id == "" {
		writeError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "User is not authenticated")
		return
	}
	if err := h.authUsecase.LogoutAll(c.Request.Context(), id, requestIPAddress(c)); err != nil {
		_ = c.Error(err)
		writeError(c, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "Unable to revoke sessions")
		return
	}
	clearRefreshCookie(c)
	c.JSON(http.StatusOK, SuccessResponse{Message: "All sessions revoked"})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
		return
	}
	userID, _ := c.Get("user_id")
	id, ok := userID.(string)
	if !ok || id == "" {
		writeError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "User is not authenticated")
		return
	}
	err := h.authUsecase.ChangePassword(c.Request.Context(), id, req.CurrentPassword, req.NewPassword, requestIPAddress(c))
	if err != nil {
		_ = c.Error(err)
	}
	switch {
	case errors.Is(err, usecase.ErrPasswordPolicy):
		writeError(c, http.StatusBadRequest, "AUTH_PASSWORD_POLICY", err.Error())
	case errors.Is(err, usecase.ErrInvalidCredentials):
		writeError(c, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "Current password is incorrect")
	case err != nil:
		writeError(c, http.StatusInternalServerError, "AUTH_INTERNAL_ERROR", "Unable to change password")
	default:
		clearRefreshCookie(c)
		c.JSON(http.StatusOK, SuccessResponse{Message: "Password changed; sign in again"})
	}
}

func (h *AuthHandler) Me(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		writeError(c, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "User is not authenticated")
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func refreshTokenFromRequest(c *gin.Context) string {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.RefreshToken != "" {
		return req.RefreshToken
	}
	if token, err := c.Cookie(refreshCookieName); err == nil {
		return token
	}
	return ""
}

func setRefreshCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookieName, Value: token, Path: "/api/v1/auth",
		Expires: expiresAt, MaxAge: int(time.Until(expiresAt).Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: refreshCookieName, Value: "", Path: "/api/v1/auth",
		MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func requestMetadata(c *gin.Context) usecase.RequestMetadata {
	return usecase.RequestMetadata{IPAddress: requestIPAddress(c), UserAgent: c.Request.UserAgent()}
}

func requestIPAddress(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		host = c.Request.RemoteAddr
	}
	if net.ParseIP(host) == nil {
		return ""
	}
	return host
}

func writeError(c *gin.Context, status int, code, message string) {
	c.Set("http_error_code", code)
	c.JSON(status, ErrorResponse{Error: APIError{Code: code, Message: message}})
}

func extractToken(c *gin.Context) string {
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
