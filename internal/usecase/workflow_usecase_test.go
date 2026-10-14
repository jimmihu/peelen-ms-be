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

func TestValidShift(t *testing.T) {
	for _, shift := range []string{"pagi", "siang", "malam", " PAGI "} {
		if !validShift(shift) {
			t.Errorf("validShift(%q) = false, want true", shift)
		}
	}
	for _, shift := range []string{"day", "", "night shift"} {
		if validShift(shift) {
			t.Errorf("validShift(%q) = true, want false", shift)
		}
	}
}

func TestNormalizeShift(t *testing.T) {
	tests := []struct {
		name    string
		input   entity.Shift
		want    entity.Shift
		wantErr bool
	}{
		{name: "valid overnight shift", input: entity.Shift{Shift: " MALAM ", StartTime: "23:30", EndTime: "07:30"}, want: entity.Shift{Shift: "malam", StartTime: "23:30", EndTime: "07:30"}},
		{name: "invalid name", input: entity.Shift{Shift: "sore", StartTime: "16:00", EndTime: "00:00"}, wantErr: true},
		{name: "invalid time", input: entity.Shift{Shift: "pagi", StartTime: "8:00", EndTime: "16:00"}, wantErr: true},
		{name: "zero duration", input: entity.Shift{Shift: "siang", StartTime: "16:00", EndTime: "16:00"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeShift(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeShift() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("normalizeShift() = %+v, want %+v", got, test.want)
			}
		})
	}
}
