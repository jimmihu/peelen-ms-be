package handler

import (
	"bytes"
	"context"
	"encoding/binary"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/usecase"
)

type reportUsecaseFake struct {
	usecase.WorkflowUsecase
	input entity.PipelineReportInput
	err   error
}

func (f *reportUsecaseFake) CreatePipelineReport(_ context.Context, _ *entity.User, input entity.PipelineReportInput) (*entity.PipelineReport, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
	return &entity.PipelineReport{ID: "report"}, nil
}

type objectStorageFake struct {
	uploaded []string
	deleted  []string
	signed   []string
}

func (s *objectStorageFake) Upload(_ context.Context, key, _ string, _ []byte) error {
	s.uploaded = append(s.uploaded, key)
	return nil
}

func (s *objectStorageFake) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func (s *objectStorageFake) SignedDownloadURL(_ context.Context, key string, _ time.Duration) (string, error) {
	s.signed = append(s.signed, key)
	return "https://minio.example/" + key, nil
}

// jpegWithCaptureTime builds a minimal little-endian EXIF JPEG carrying
// DateTimeOriginal.
func jpegWithCaptureTime(dateTime string) []byte {
	order := binary.LittleEndian
	const ifd0, exifIFD, dataStart = 8, 26, 44
	tiff := make([]byte, dataStart)
	copy(tiff, "II")
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], ifd0)
	order.PutUint16(tiff[ifd0:], 1)
	order.PutUint16(tiff[ifd0+2:], 0x8769)
	order.PutUint16(tiff[ifd0+4:], 4)
	order.PutUint32(tiff[ifd0+6:], 1)
	order.PutUint32(tiff[ifd0+10:], exifIFD)
	value := []byte(dateTime + "\x00")
	order.PutUint16(tiff[exifIFD:], 1)
	order.PutUint16(tiff[exifIFD+2:], 0x9003)
	order.PutUint16(tiff[exifIFD+4:], 2)
	order.PutUint32(tiff[exifIFD+6:], uint32(len(value)))
	order.PutUint32(tiff[exifIFD+10:], dataStart)
	tiff = append(tiff, value...)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(jpeg[4:], uint16(len(app1)+2))
	jpeg = append(jpeg, app1...)
	return append(jpeg, 0xFF, 0xDA, 0, 2, 0xFF, 0xD9)
}

func multipartReportRequest(t *testing.T, fields map[string]string, files map[string][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	for field, data := range files {
		part, err := writer.CreateFormFile(field, field+".jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/pipeline-reports", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestCreatePipelineReportTakesWorkTimesFromPhotoEXIF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jakarta := time.FixedZone("WIB", 7*3600)
	fake := &reportUsecaseFake{}
	storage := &objectStorageFake{}
	h := NewWorkflowHandler(fake, storage, jakarta)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = multipartReportRequest(t, map[string]string{
		"pipeline_item_id": "30000000-0000-4000-8000-000000000001",
		"shift":            "DAY",
		"report_date":      "2026-10-14",
		"status":           "COMPLETED",
		// A client-supplied time must not override the camera's capture time.
		"closed_date": "2026-10-14T17:30:00+07:00",
	}, map[string][]byte{
		"evidence_before": jpegWithCaptureTime("2026:10:14 09:00:00"),
		"evidence_after":  jpegWithCaptureTime("2026:10:14 09:40:00"),
	})
	h.CreatePipelineReport(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(storage.uploaded) != 2 {
		t.Fatalf("uploaded %d photos, want 2", len(storage.uploaded))
	}
	in := fake.input
	if in.EvidenceBefore == nil || !strings.HasPrefix(*in.EvidenceBefore, "pipeline-reports/") ||
		in.EvidenceAfter == nil || !strings.HasPrefix(*in.EvidenceAfter, "pipeline-reports/") {
		t.Fatalf("evidence keys not set: before=%v after=%v", in.EvidenceBefore, in.EvidenceAfter)
	}
	// EXIF without an offset is a wall-clock time at the site; the repository
	// converts it with the site's time zone.
	if in.OpenDate != nil || in.OpenDateLocal == nil || *in.OpenDateLocal != "2026-10-14T09:00:00" {
		t.Fatalf("open = %v / %v, want local 2026-10-14T09:00:00", in.OpenDate, in.OpenDateLocal)
	}
	if in.ClosedDate != nil || in.ClosedDateLocal == nil || *in.ClosedDateLocal != "2026-10-14T09:40:00" {
		t.Fatalf("closed = %v / %v, want local 2026-10-14T09:40:00", in.ClosedDate, in.ClosedDateLocal)
	}
}

func TestCreatePipelineReportRemovesPhotosWhenSaveFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &reportUsecaseFake{err: usecase.ErrInvalidOperationalData}
	storage := &objectStorageFake{}
	h := NewWorkflowHandler(fake, storage, time.UTC)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = multipartReportRequest(t, map[string]string{"shift": "DAY"}, map[string][]byte{
		"evidence_after": jpegWithCaptureTime("2026:10:14 09:40:00"),
	})
	h.CreatePipelineReport(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != storage.uploaded[0] {
		t.Fatalf("uploaded %v but deleted %v", storage.uploaded, storage.deleted)
	}
}
