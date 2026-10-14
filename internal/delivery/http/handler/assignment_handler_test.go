package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAssignmentFilterFromQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET",
		"/api/v1/assignments?user_id=u&pipeline_id=p&is_active=false&page=2&limit=50", nil)

	filter, err := assignmentFilterFromQuery(context)
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	if filter.UserID != "u" || filter.PipelineID != "p" || filter.Page != 2 || filter.Limit != 50 {
		t.Fatalf("unexpected filter: %+v", filter)
	}
	if filter.IsActive == nil || *filter.IsActive {
		t.Fatalf("expected is_active=false, got %v", filter.IsActive)
	}
}

func TestAssignmentFilterFromQueryLeavesIsActiveUnsetByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/v1/assignments", nil)

	filter, err := assignmentFilterFromQuery(context)
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	if filter.IsActive != nil {
		t.Fatalf("expected is_active to be unset, got %v", *filter.IsActive)
	}
}

func TestAssignmentFilterFromQueryRejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"is_active=maybe", "page=0", "limit=abc"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/api/v1/assignments?"+query, nil)
		if _, err := assignmentFilterFromQuery(context); err == nil {
			t.Errorf("expected %q to be rejected", query)
		}
	}
}
