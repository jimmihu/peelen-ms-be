package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/usecase"
)

type inventoryReportUsecaseFake struct {
	usecase.WorkflowUsecase
	input  entity.InventoryReportInput
	report entity.InventoryReport
	err    error
}

func (f *inventoryReportUsecaseFake) CreateInventoryReport(_ context.Context, _ *entity.User, input entity.InventoryReportInput) (*entity.InventoryReport, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	return &entity.InventoryReport{ID: "inventory-report"}, nil
}

func (f *inventoryReportUsecaseFake) GetInventoryReport(context.Context, *entity.User, string) (*entity.InventoryReport, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &f.report, nil
}

func TestCreateInventoryReportAcceptsMultipartFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &inventoryReportUsecaseFake{}
	objectStorage := &objectStorageFake{}
	handler := NewWorkflowHandler(fake, objectStorage, time.UTC)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = multipartReportRequest(t, map[string]string{
		"vendor_id":    "20000000-0000-4000-8000-000000000001",
		"inventory_id": "4a550371-7050-4d56-ba58-f86f9bff4daf",
		"report_type":  "REQUEST",
		"description":  "robek",
	}, map[string][]byte{"file": jpegWithCaptureTime("2026:10:15 12:00:00")})

	handler.CreateInventoryReport(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if fake.input.VendorID != "20000000-0000-4000-8000-000000000001" ||
		fake.input.InventoryID == nil || *fake.input.InventoryID != "4a550371-7050-4d56-ba58-f86f9bff4daf" ||
		fake.input.ReportType != "REQUEST" || fake.input.Description != "robek" {
		t.Fatalf("multipart fields not bound correctly: %+v", fake.input)
	}
	if fake.input.Evidence == nil || !strings.HasPrefix(*fake.input.Evidence, "inventory-reports/") {
		t.Fatalf("uploaded file key not assigned as evidence: %v", fake.input.Evidence)
	}
	if len(objectStorage.uploaded) != 1 || objectStorage.uploaded[0] != *fake.input.Evidence {
		t.Fatalf("uploaded keys = %v, evidence = %v", objectStorage.uploaded, fake.input.Evidence)
	}
}

func TestCreateInventoryReportRemovesUploadedFileWhenSaveFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &inventoryReportUsecaseFake{err: usecase.ErrInvalidOperationalData}
	objectStorage := &objectStorageFake{}
	handler := NewWorkflowHandler(fake, objectStorage, time.UTC)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = multipartReportRequest(t, map[string]string{
		"vendor_id":   "20000000-0000-4000-8000-000000000001",
		"report_type": "REQUEST",
		"description": "restock",
	}, map[string][]byte{"file": jpegWithCaptureTime("2026:10:15 12:00:00")})

	handler.CreateInventoryReport(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if len(objectStorage.uploaded) != 1 || len(objectStorage.deleted) != 1 ||
		objectStorage.deleted[0] != objectStorage.uploaded[0] {
		t.Fatalf("uploaded %v but deleted %v", objectStorage.uploaded, objectStorage.deleted)
	}
}

func TestCreateInventoryReportRejectsPDFEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	objectStorage := &objectStorageFake{}
	handler := NewWorkflowHandler(&inventoryReportUsecaseFake{}, objectStorage, time.UTC)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = multipartReportRequest(t, map[string]string{
		"vendor_id":   "20000000-0000-4000-8000-000000000001",
		"report_type": "REQUEST",
		"description": "restock",
	}, map[string][]byte{"file": []byte("%PDF-1.7 evidence")})

	handler.CreateInventoryReport(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if len(objectStorage.uploaded) != 0 {
		t.Fatalf("uploaded unsupported PDF evidence: %v", objectStorage.uploaded)
	}
}

func TestGetInventoryReportEvidenceReturnsSignedURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	objectKey := "inventory-reports/photo.jpg"
	fake := &inventoryReportUsecaseFake{report: entity.InventoryReport{Evidence: &objectKey}}
	objectStorage := &objectStorageFake{}
	handler := NewWorkflowHandler(fake, objectStorage, time.UTC)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "report-id"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/inventory-reports/report-id/evidence", nil)

	handler.GetInventoryReportEvidence(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "https://minio.example/"+objectKey) ||
		!strings.Contains(recorder.Body.String(), `"expires_in":600`) {
		t.Fatalf("unexpected signed URL response: %s", recorder.Body.String())
	}
	if len(objectStorage.signed) != 1 || objectStorage.signed[0] != objectKey {
		t.Fatalf("signed keys = %v, want [%s]", objectStorage.signed, objectKey)
	}
}
