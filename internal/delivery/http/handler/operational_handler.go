package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
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

const (
	maxInventoryImageSize = 5 << 20
	maxPolicyFileSize     = 10 << 20
	policyDownloadExpiry  = 10 * time.Minute
)

type OperationalHandler struct {
	operationalUsecase usecase.OperationalUsecase
	objectStorage      storage.ObjectStorage
}

func NewOperationalHandler(operationalUsecase usecase.OperationalUsecase, objectStorage storage.ObjectStorage) *OperationalHandler {
	return &OperationalHandler{operationalUsecase: operationalUsecase, objectStorage: objectStorage}
}

func (h *OperationalHandler) List(resource entity.OperationalResource) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter, err := operationalFilterFromQuery(c)
		if err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid list filter")
			return
		}
		records, total, err := h.operationalUsecase.List(c.Request.Context(), currentUser(c), resource, filter)
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		// Inventory photos are object keys: list rows get signed URLs too (thumbnails).
		if resource == entity.ResourceInventory && h.objectStorage != nil {
			for i := range records {
				if records[i].Image == nil || *records[i].Image == "" {
					continue
				}
				imageURL, err := h.objectStorage.SignedDownloadURL(c.Request.Context(), *records[i].Image, policyDownloadExpiry)
				if err != nil {
					_ = c.Error(err)
					continue
				}
				records[i].ImageURL = &imageURL
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"data": records, "page": filter.Page, "limit": filter.Limit, "total": total,
		})
	}
}

func (h *OperationalHandler) Get(resource entity.OperationalResource) gin.HandlerFunc {
	return func(c *gin.Context) {
		record, err := h.operationalUsecase.Get(c.Request.Context(), currentUser(c), resource, c.Param("id"))
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		if resource == entity.ResourceInventory && record.Status == "ACTIVE" && record.Image != nil {
			if h.objectStorage == nil {
				writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
				return
			}
			imageURL, err := h.objectStorage.SignedDownloadURL(c.Request.Context(), *record.Image, policyDownloadExpiry)
			if err != nil {
				_ = c.Error(err)
				writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to generate image URL")
				return
			}
			record.ImageURL = &imageURL
		}
		c.JSON(http.StatusOK, gin.H{"data": record})
	}
}

func (h *OperationalHandler) Create(resource entity.OperationalResource) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input entity.OperationalInput
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return
		}
		record, err := h.operationalUsecase.Create(c.Request.Context(), currentUser(c), resource, input, requestIPAddress(c))
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": record})
	}
}

func (h *OperationalHandler) CreatePipelineItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		pipelineID := c.Param("id")
		if !uuidPattern.MatchString(pipelineID) {
			writeError(c, http.StatusNotFound, "AUTH_NOT_FOUND", "Pipeline not found")
			return
		}
		var input entity.OperationalInput
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return
		}
		input.PipelineID = &pipelineID
		record, err := h.operationalUsecase.Create(
			c.Request.Context(), currentUser(c), entity.ResourcePipelineItem, input, requestIPAddress(c),
		)
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": record})
	}
}

func (h *OperationalHandler) ListPipelineItems() gin.HandlerFunc {
	return func(c *gin.Context) {
		pipelineID := c.Param("id")
		if !uuidPattern.MatchString(pipelineID) {
			writeError(c, http.StatusNotFound, "AUTH_NOT_FOUND", "Pipeline not found")
			return
		}
		filter, err := operationalFilterFromQuery(c)
		if err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid list filter")
			return
		}
		filter.PipelineID = pipelineID
		records, total, err := h.operationalUsecase.List(
			c.Request.Context(), currentUser(c), entity.ResourcePipelineItem, filter,
		)
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"data": records, "page": filter.Page, "limit": filter.Limit, "total": total,
		})
	}
}

func (h *OperationalHandler) Update(resource entity.OperationalResource) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input entity.OperationalInput
		if err := c.ShouldBindJSON(&input); err != nil {
			writeError(c, http.StatusBadRequest, "AUTH_INVALID_REQUEST", "Invalid request body")
			return
		}
		record, err := h.operationalUsecase.Update(
			c.Request.Context(), currentUser(c), resource, c.Param("id"), input, requestIPAddress(c),
		)
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": record})
	}
}

func (h *OperationalHandler) Delete(resource entity.OperationalResource) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h.operationalUsecase.Delete(
			c.Request.Context(), currentUser(c), resource, c.Param("id"), requestIPAddress(c),
		); err != nil {
			handleOperationalError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func (h *OperationalHandler) CreateInventory() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := parseMultipartRequest(c, maxInventoryImageSize); err != nil {
			handleMultipartError(c, err)
			return
		}
		defer removeMultipartForm(c)
		input := entity.OperationalInput{
			VendorID: formString(c, "vendor_id"), Item: formString(c, "item"),
			ItemType: formString(c, "jenis_item"), Condition: formString(c, "status_condition"),
		}
		var err error
		input.Quantity, err = formInt(c, "qty")
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", "qty must be a non-negative integer")
			return
		}
		upload, err := readUpload(c, "image", maxInventoryImageSize, isSupportedImage)
		if err != nil {
			handleMultipartError(c, err)
			return
		}
		if upload != nil {
			if h.objectStorage == nil {
				writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
				return
			}
			key, err := newObjectKey("inventories", upload.extension)
			if err != nil {
				handleOperationalError(c, err)
				return
			}
			if err := h.objectStorage.Upload(c.Request.Context(), key, upload.contentType, upload.data); err != nil {
				_ = c.Error(err)
				writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to upload image")
				return
			}
			input.Image = &key
			record, err := h.operationalUsecase.Create(c.Request.Context(), currentUser(c), entity.ResourceInventory, input, requestIPAddress(c))
			if err != nil {
				deleteObjectAfterFailure(h.objectStorage, key)
				handleOperationalError(c, err)
				return
			}
			c.JSON(http.StatusCreated, gin.H{"data": record})
			return
		}
		record, err := h.operationalUsecase.Create(c.Request.Context(), currentUser(c), entity.ResourceInventory, input, requestIPAddress(c))
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": record})
	}
}

func (h *OperationalHandler) UpdateInventory() gin.HandlerFunc {
	return func(c *gin.Context) {
		old, err := h.operationalUsecase.Get(c.Request.Context(), currentUser(c), entity.ResourceInventory, c.Param("id"))
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		if err := parseMultipartRequest(c, maxInventoryImageSize); err != nil {
			handleMultipartError(c, err)
			return
		}
		defer removeMultipartForm(c)
		input := entity.OperationalInput{
			Condition: formString(c, "status_condition"), Status: formString(c, "status"),
		}
		input.Quantity, err = formInt(c, "qty")
		if err != nil {
			writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", "qty must be a non-negative integer")
			return
		}
		upload, err := readUpload(c, "image", maxInventoryImageSize, isSupportedImage)
		if err != nil {
			handleMultipartError(c, err)
			return
		}
		newKey := ""
		if upload != nil {
			if h.objectStorage == nil {
				writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
				return
			}
			newKey, err = newObjectKey("inventories", upload.extension)
			if err != nil {
				handleOperationalError(c, err)
				return
			}
			if err := h.objectStorage.Upload(c.Request.Context(), newKey, upload.contentType, upload.data); err != nil {
				_ = c.Error(err)
				writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to upload image")
				return
			}
			input.Image = &newKey
		}
		record, err := h.operationalUsecase.Update(
			c.Request.Context(), currentUser(c), entity.ResourceInventory, c.Param("id"), input, requestIPAddress(c),
		)
		if err != nil {
			if newKey != "" {
				deleteObjectAfterFailure(h.objectStorage, newKey)
			}
			handleOperationalError(c, err)
			return
		}
		if newKey != "" && old.Image != nil {
			deleteOldObject(h.objectStorage, *old.Image)
		}
		c.JSON(http.StatusOK, gin.H{"data": record})
	}
}

func (h *OperationalHandler) CreatePolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := parseMultipartRequest(c, maxPolicyFileSize); err != nil {
			handleMultipartError(c, err)
			return
		}
		defer removeMultipartForm(c)
		upload, err := readUpload(c, "file", maxPolicyFileSize, isPDF)
		if err != nil {
			handleMultipartError(c, err)
			return
		}
		if upload == nil {
			writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", "A PDF file is required")
			return
		}
		if h.objectStorage == nil {
			writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
			return
		}
		key, err := newObjectKey("policies", ".pdf")
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		if err := h.objectStorage.Upload(c.Request.Context(), key, "application/pdf", upload.data); err != nil {
			_ = c.Error(err)
			writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to upload policy")
			return
		}
		input := entity.OperationalInput{
			VendorID: formString(c, "vendor_id"), Name: formString(c, "name"), FileURL: &key,
		}
		record, err := h.operationalUsecase.Create(
			c.Request.Context(), currentUser(c), entity.ResourcePolicy, input, requestIPAddress(c),
		)
		if err != nil {
			deleteObjectAfterFailure(h.objectStorage, key)
			handleOperationalError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": record})
	}
}

func (h *OperationalHandler) UpdatePolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		old, err := h.operationalUsecase.Get(c.Request.Context(), currentUser(c), entity.ResourcePolicy, c.Param("id"))
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		if err := parseMultipartRequest(c, maxPolicyFileSize); err != nil {
			handleMultipartError(c, err)
			return
		}
		defer removeMultipartForm(c)
		input := entity.OperationalInput{Name: formString(c, "name"), Status: formString(c, "status")}
		upload, err := readUpload(c, "file", maxPolicyFileSize, isPDF)
		if err != nil {
			handleMultipartError(c, err)
			return
		}
		newKey := ""
		if upload != nil {
			if h.objectStorage == nil {
				writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "File storage is not configured")
				return
			}
			newKey, err = newObjectKey("policies", ".pdf")
			if err != nil {
				handleOperationalError(c, err)
				return
			}
			if err := h.objectStorage.Upload(c.Request.Context(), newKey, "application/pdf", upload.data); err != nil {
				_ = c.Error(err)
				writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to upload policy")
				return
			}
			input.FileURL = &newKey
		}
		if inputIsEmptyForHandler(input) {
			writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", "At least one policy field is required")
			return
		}
		record, err := h.operationalUsecase.Update(
			c.Request.Context(), currentUser(c), entity.ResourcePolicy, c.Param("id"), input, requestIPAddress(c),
		)
		if err != nil {
			if newKey != "" {
				deleteObjectAfterFailure(h.objectStorage, newKey)
			}
			handleOperationalError(c, err)
			return
		}
		if newKey != "" && old.FileURL != nil {
			deleteOldObject(h.objectStorage, *old.FileURL)
		}
		c.JSON(http.StatusOK, gin.H{"data": record})
	}
}

func (h *OperationalHandler) PolicyFile() gin.HandlerFunc {
	return func(c *gin.Context) {
		record, err := h.operationalUsecase.Get(c.Request.Context(), currentUser(c), entity.ResourcePolicy, c.Param("id"))
		if err != nil {
			handleOperationalError(c, err)
			return
		}
		if record.Status != "ACTIVE" {
			writeError(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Resource not found")
			return
		}
		if h.objectStorage == nil || record.FileURL == nil {
			writeError(c, http.StatusServiceUnavailable, "OBJECT_STORAGE_UNAVAILABLE", "Policy file is not available")
			return
		}
		url, err := h.objectStorage.SignedDownloadURL(c.Request.Context(), *record.FileURL, policyDownloadExpiry)
		if err != nil {
			_ = c.Error(err)
			writeError(c, http.StatusInternalServerError, "OBJECT_STORAGE_ERROR", "Unable to generate policy URL")
			return
		}
		c.JSON(http.StatusOK, gin.H{"url": url, "expires_in": int(policyDownloadExpiry.Seconds())})
	}
}

type uploadedFile struct {
	data        []byte
	contentType string
	extension   string
}

func parseMultipartRequest(c *gin.Context, maxSize int64) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize+(1<<20))
	if err := c.Request.ParseMultipartForm(maxSize); err != nil {
		return err
	}
	if c.Request.MultipartForm == nil {
		return errors.New("multipart form is required")
	}
	return nil
}

func removeMultipartForm(c *gin.Context) {
	if c.Request.MultipartForm != nil {
		if err := c.Request.MultipartForm.RemoveAll(); err != nil {
			_ = c.Error(err)
		}
	}
}

func readUpload(c *gin.Context, field string, maxSize int64, validate func([]byte) (string, string, bool)) (*uploadedFile, error) {
	file, _, err := c.Request.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("uploaded file is too large")
	}
	contentType, extension, ok := validate(data)
	if !ok {
		return nil, errors.New("unsupported uploaded file type")
	}
	return &uploadedFile{data: data, contentType: contentType, extension: extension}, nil
}

func isPDF(data []byte) (string, string, bool) {
	return "application/pdf", ".pdf", bytes.HasPrefix(data, []byte("%PDF-"))
}

func isSupportedImage(data []byte) (string, string, bool) {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return "image/jpeg", ".jpg", true
	case "image/png":
		return "image/png", ".png", true
	case "image/webp":
		return "image/webp", ".webp", true
	default:
		return "", "", false
	}
}

func newObjectKey(prefix, extension string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate object key: %w", err)
	}
	return prefix + "/" + hex.EncodeToString(random[:]) + extension, nil
}

func formString(c *gin.Context, key string) *string {
	value, exists := c.GetPostForm(key)
	if !exists {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func formInt(c *gin.Context, key string) (*int, error) {
	value, exists := c.GetPostForm(key)
	if !exists {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return nil, errors.New("value must be a non-negative integer")
	}
	return &parsed, nil
}

func operationalFilterFromQuery(c *gin.Context) (entity.OperationalFilter, error) {
	page, limit := 1, 20
	var err error
	if value := c.Query("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 {
			return entity.OperationalFilter{}, errors.New("page must be positive")
		}
	}
	if value := c.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return entity.OperationalFilter{}, errors.New("limit must be between 1 and 100")
		}
	}
	// Lists show active records unless asked; status=ALL includes inactive ones
	// (master data screens that reactivate them).
	status := strings.ToUpper(c.Query("status"))
	switch status {
	case "":
		status = "ACTIVE"
	case "ALL":
		status = ""
	}
	return entity.OperationalFilter{
		VendorID: c.Query("vendor_id"), SiteID: c.Query("site_id"),
		PipelineID: c.Query("pipeline_id"), Status: status,
		Condition: strings.ToUpper(c.Query("status_condition")), Query: c.Query("q"),
		Page: page, Limit: limit,
	}, nil
}

func handleMultipartError(c *gin.Context, err error) {
	_ = c.Error(err)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) || strings.Contains(err.Error(), "too large") {
		writeError(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Uploaded file exceeds the allowed size")
		return
	}
	writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", err.Error())
}

func deleteObjectAfterFailure(objectStorage storage.ObjectStorage, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := objectStorage.Delete(ctx, key); err != nil {
		log.Printf("failed to remove orphaned object key=%q: %v", key, err)
	}
}

func deleteOldObject(objectStorage storage.ObjectStorage, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := objectStorage.Delete(ctx, key); err != nil {
		log.Printf("failed to remove replaced object key=%q: %v", key, err)
	}
}

func inputIsEmptyForHandler(input entity.OperationalInput) bool {
	return input.Name == nil && input.Status == nil && input.FileURL == nil
}

func handleOperationalError(c *gin.Context, err error) {
	_ = c.Error(err)
	switch {
	case errors.Is(err, usecase.ErrForbidden):
		writeError(c, http.StatusForbidden, "AUTH_FORBIDDEN", "You are not allowed to perform this action")
	case errors.Is(err, usecase.ErrNotFound), errors.Is(err, repository.ErrNotFound), errors.Is(err, entity.ErrInvalidID):
		writeError(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Resource not found")
	case errors.Is(err, usecase.ErrInvalidOperationalData), errors.Is(err, entity.ErrInvalidOperationalData), errors.Is(err, entity.ErrInvalidOperationalStatus):
		writeError(c, http.StatusBadRequest, "INVALID_OPERATIONAL_DATA", err.Error())
	case errors.Is(err, repository.ErrConflict):
		writeError(c, http.StatusConflict, "RESOURCE_CONFLICT", "The resource already exists")
	default:
		writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Unable to complete the request")
	}
}
