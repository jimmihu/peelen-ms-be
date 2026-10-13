package usecase

import (
	"context"
	"testing"

	"peelen-ms-be/internal/domain/entity"
)

func TestValidateReportFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  entity.ReportFilter
		wantErr bool
	}{
		{name: "valid defaults", filter: entity.ReportFilter{Page: 1, Limit: 20}},
		{name: "valid date range", filter: entity.ReportFilter{
			Page: 1, Limit: 20, From: "2026-10-01", To: "2026-10-31", Status: "SUBMITTED",
		}},
		{name: "invalid status", filter: entity.ReportFilter{Page: 1, Limit: 20, Status: "UNKNOWN"}, wantErr: true},
		{name: "invalid date", filter: entity.ReportFilter{Page: 1, Limit: 20, From: "yesterday"}, wantErr: true},
		{name: "reversed date range", filter: entity.ReportFilter{
			Page: 1, Limit: 20, From: "2026-10-31", To: "2026-10-01",
		}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateReportFilter(&test.filter)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateReportFilter() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestSiteAndVendorWritesRequireSuperAdmin(t *testing.T) {
	service := NewWorkflowUsecase(nil)
	admin := &entity.User{Roles: []string{"ADMIN"}}
	_, err := service.CreateSite(context.Background(), admin, entity.Site{Code: "SITE-01", Name: "Site"})
	if err != ErrForbidden {
		t.Fatalf("expected site create to be forbidden, got %v", err)
	}
	_, err = service.CreateVendor(context.Background(), admin, entity.Vendor{Code: "VND-01", Name: "Vendor"})
	if err != ErrForbidden {
		t.Fatalf("expected vendor create to be forbidden, got %v", err)
	}
}
