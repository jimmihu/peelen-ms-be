package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOperationalFilterFromQueryDefaultsToActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/v1/inventories", nil)

	filter, err := operationalFilterFromQuery(context)
	if err != nil {
		t.Fatalf("parse default filter: %v", err)
	}
	if filter.Status != "ACTIVE" {
		t.Fatalf("expected default status ACTIVE, got %q", filter.Status)
	}
}

func TestOperationalFilterFromQueryAcceptsExplicitStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/v1/inventories?status=inactive", nil)

	filter, err := operationalFilterFromQuery(context)
	if err != nil {
		t.Fatalf("parse explicit filter: %v", err)
	}
	if filter.Status != "INACTIVE" {
		t.Fatalf("expected status INACTIVE, got %q", filter.Status)
	}
}
