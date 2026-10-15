package usecase

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

var workflowCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,20}$`)

type WorkflowUsecase interface {
	ListSites(context.Context, *entity.User) ([]entity.Site, error)
	GetSite(context.Context, *entity.User, string) (*entity.Site, error)
	CreateSite(context.Context, *entity.User, entity.Site) (*entity.Site, error)
	UpdateSite(context.Context, *entity.User, string, entity.Site) (*entity.Site, error)
	DeleteSite(context.Context, *entity.User, string) error
	ListVendors(context.Context, *entity.User) ([]entity.Vendor, error)
	GetVendor(context.Context, *entity.User, string) (*entity.Vendor, error)
	CreateVendor(context.Context, *entity.User, entity.Vendor) (*entity.Vendor, error)
	UpdateVendor(context.Context, *entity.User, string, entity.Vendor) (*entity.Vendor, error)
	DeleteVendor(context.Context, *entity.User, string) error
	ListShifts(context.Context, *entity.User) ([]entity.Shift, error)
	GetShift(context.Context, *entity.User, string) (*entity.Shift, error)
	CreateShift(context.Context, *entity.User, entity.Shift) (*entity.Shift, error)
	UpdateShift(context.Context, *entity.User, string, entity.Shift) (*entity.Shift, error)
	DeleteShift(context.Context, *entity.User, string) error
	ListMyTasks(context.Context, *entity.User, string) ([]entity.MyTask, error)
	ListPipelineReports(context.Context, *entity.User, entity.ReportFilter) ([]entity.PipelineReport, int, error)
	GetPipelineReport(context.Context, *entity.User, string) (*entity.PipelineReport, error)
	CreatePipelineReport(context.Context, *entity.User, entity.PipelineReportInput) (*entity.PipelineReport, error)
	UpdatePipelineReport(context.Context, *entity.User, string, entity.PipelineReportInput) (*entity.PipelineReport, error)
	ListInventoryReports(context.Context, *entity.User, entity.ReportFilter) ([]entity.InventoryReport, int, error)
	GetInventoryReport(context.Context, *entity.User, string) (*entity.InventoryReport, error)
	CreateInventoryReport(context.Context, *entity.User, entity.InventoryReportInput) (*entity.InventoryReport, error)
	ReviewInventoryReport(context.Context, *entity.User, string, string, *string) (*entity.InventoryReport, error)
	ReportDashboard(context.Context, *entity.User, string, string, string) (map[string]any, error)
	DailyReport(context.Context, *entity.User, string, string, string) ([]map[string]any, error)
}

type WorkflowUsecaseImpl struct {
	repository repository.WorkflowRepository
}

func NewWorkflowUsecase(repo repository.WorkflowRepository) WorkflowUsecase {
	return &WorkflowUsecaseImpl{repository: repo}
}

func (u *WorkflowUsecaseImpl) ListSites(ctx context.Context, actor *entity.User) ([]entity.Site, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	return u.repository.ListSites(ctx, operationalAccess(actor))
}

func (u *WorkflowUsecaseImpl) GetSite(ctx context.Context, actor *entity.User, id string) (*entity.Site, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	return u.repository.GetSite(ctx, operationalAccess(actor), id)
}

func (u *WorkflowUsecaseImpl) CreateSite(ctx context.Context, actor *entity.User, site entity.Site) (*entity.Site, error) {
	if !workflowSuperAdmin(actor) {
		return nil, ErrForbidden
	}
	if !workflowCodePattern.MatchString(site.Code) || strings.TrimSpace(site.Name) == "" || len(site.Name) > 150 ||
		site.SiteType != nil && len(*site.SiteType) > 30 || site.Status != "" && !validWorkflowStatus(site.Status) {
		return nil, ErrInvalidOperationalData
	}
	site.Code = strings.ToUpper(strings.TrimSpace(site.Code))
	site.Name = strings.TrimSpace(site.Name)
	return u.repository.CreateSite(ctx, site)
}

func (u *WorkflowUsecaseImpl) UpdateSite(ctx context.Context, actor *entity.User, id string, site entity.Site) (*entity.Site, error) {
	if !workflowSuperAdmin(actor) {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) || site.Code == "" && site.Name == "" && site.SiteType == nil && site.Status == "" {
		return nil, ErrInvalidOperationalData
	}
	if site.Code != "" && !workflowCodePattern.MatchString(site.Code) ||
		site.Name != "" && (strings.TrimSpace(site.Name) == "" || len(site.Name) > 150) ||
		site.SiteType != nil && len(*site.SiteType) > 30 ||
		site.Status != "" && !validWorkflowStatus(site.Status) {
		return nil, ErrInvalidOperationalData
	}
	site.Code = strings.ToUpper(strings.TrimSpace(site.Code))
	site.Name = strings.TrimSpace(site.Name)
	return u.repository.UpdateSite(ctx, id, site)
}

func (u *WorkflowUsecaseImpl) DeleteSite(ctx context.Context, actor *entity.User, id string) error {
	if !workflowSuperAdmin(actor) {
		return ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return ErrNotFound
	}
	return u.repository.DeleteSite(ctx, id)
}

func (u *WorkflowUsecaseImpl) ListVendors(ctx context.Context, actor *entity.User) ([]entity.Vendor, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	return u.repository.ListVendors(ctx, operationalAccess(actor))
}

func (u *WorkflowUsecaseImpl) GetVendor(ctx context.Context, actor *entity.User, id string) (*entity.Vendor, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	return u.repository.GetVendor(ctx, operationalAccess(actor), id)
}

func (u *WorkflowUsecaseImpl) CreateVendor(ctx context.Context, actor *entity.User, vendor entity.Vendor) (*entity.Vendor, error) {
	if !workflowSuperAdmin(actor) {
		return nil, ErrForbidden
	}
	if !workflowCodePattern.MatchString(vendor.Code) || strings.TrimSpace(vendor.Name) == "" ||
		len(vendor.Name) > 150 || vendor.Status != "" && !validWorkflowStatus(vendor.Status) ||
		!validUUIDList(vendor.SiteIDs) {
		return nil, ErrInvalidOperationalData
	}
	vendor.Code = strings.ToUpper(strings.TrimSpace(vendor.Code))
	vendor.Name = strings.TrimSpace(vendor.Name)
	return u.repository.CreateVendor(ctx, vendor, vendor.SiteIDs)
}

func (u *WorkflowUsecaseImpl) UpdateVendor(ctx context.Context, actor *entity.User, id string, vendor entity.Vendor) (*entity.Vendor, error) {
	if !workflowSuperAdmin(actor) {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) ||
		vendor.Code == "" && vendor.Name == "" && vendor.Status == "" && vendor.SiteIDs == nil {
		return nil, ErrInvalidOperationalData
	}
	if vendor.Code != "" && !workflowCodePattern.MatchString(vendor.Code) ||
		vendor.Name != "" && (strings.TrimSpace(vendor.Name) == "" || len(vendor.Name) > 150) ||
		vendor.Status != "" && !validWorkflowStatus(vendor.Status) ||
		!validUUIDList(vendor.SiteIDs) {
		return nil, ErrInvalidOperationalData
	}
	vendor.Code = strings.ToUpper(strings.TrimSpace(vendor.Code))
	vendor.Name = strings.TrimSpace(vendor.Name)
	return u.repository.UpdateVendor(ctx, id, vendor, vendor.SiteIDs)
}

func (u *WorkflowUsecaseImpl) DeleteVendor(ctx context.Context, actor *entity.User, id string) error {
	if !workflowSuperAdmin(actor) {
		return ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) {
		return ErrNotFound
	}
	return u.repository.DeleteVendor(ctx, id)
}

func (u *WorkflowUsecaseImpl) ListShifts(ctx context.Context, actor *entity.User) ([]entity.Shift, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	return u.repository.ListShifts(ctx)
}

func (u *WorkflowUsecaseImpl) GetShift(ctx context.Context, actor *entity.User, shift string) (*entity.Shift, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	shift, err := normalizeShiftName(shift)
	if err != nil {
		return nil, err
	}
	return u.repository.GetShift(ctx, shift)
}

func (u *WorkflowUsecaseImpl) CreateShift(ctx context.Context, actor *entity.User, shift entity.Shift) (*entity.Shift, error) {
	if !workflowSuperAdmin(actor) {
		return nil, ErrForbidden
	}
	normalized, err := normalizeShift(shift)
	if err != nil {
		return nil, err
	}
	return u.repository.CreateShift(ctx, normalized)
}

func (u *WorkflowUsecaseImpl) UpdateShift(ctx context.Context, actor *entity.User, name string, shift entity.Shift) (*entity.Shift, error) {
	if !workflowSuperAdmin(actor) {
		return nil, ErrForbidden
	}
	name, err := normalizeShiftName(name)
	if err != nil {
		return nil, err
	}
	shift.Shift = name
	normalized, err := normalizeShift(shift)
	if err != nil {
		return nil, err
	}
	return u.repository.UpdateShift(ctx, name, normalized)
}

func (u *WorkflowUsecaseImpl) DeleteShift(ctx context.Context, actor *entity.User, name string) error {
	if !workflowSuperAdmin(actor) {
		return ErrForbidden
	}
	name, err := normalizeShiftName(name)
	if err != nil {
		return err
	}
	return u.repository.DeleteShift(ctx, name)
}

func normalizeShift(shift entity.Shift) (entity.Shift, error) {
	name, err := normalizeShiftName(shift.Shift)
	if err != nil {
		return entity.Shift{}, err
	}
	if !validShiftTime(shift.StartTime) || !validShiftTime(shift.EndTime) || shift.StartTime == shift.EndTime {
		return entity.Shift{}, ErrInvalidOperationalData
	}
	shift.Shift = name
	return shift, nil
}

func normalizeShiftName(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !validShift(value) {
		return "", ErrInvalidOperationalData
	}
	return value, nil
}

func validShiftTime(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func (u *WorkflowUsecaseImpl) ListMyTasks(ctx context.Context, actor *entity.User, date string) ([]entity.MyTask, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	parsed, err := parseReportDate(date)
	if err != nil {
		return nil, err
	}
	return u.repository.ListMyTasks(ctx, operationalAccess(actor), parsed)
}

func (u *WorkflowUsecaseImpl) ListPipelineReports(ctx context.Context, actor *entity.User, filter entity.ReportFilter) ([]entity.PipelineReport, int, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, 0, err
	}
	if err := validateReportFilter(&filter); err != nil {
		return nil, 0, err
	}
	return u.repository.ListPipelineReports(ctx, operationalAccess(actor), filter)
}

func (u *WorkflowUsecaseImpl) GetPipelineReport(ctx context.Context, actor *entity.User, id string) (*entity.PipelineReport, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	return u.repository.GetPipelineReport(ctx, operationalAccess(actor), id)
}

func (u *WorkflowUsecaseImpl) CreatePipelineReport(ctx context.Context, actor *entity.User, input entity.PipelineReportInput) (*entity.PipelineReport, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(input.PipelineItemID) || !validShift(input.Shift) ||
		!validReportStatus(input.Status) || !validNoteCategory(input.NoteCategory) {
		return nil, ErrInvalidOperationalData
	}
	if input.Status == "COMPLETED" && (input.EvidenceAfter == nil || input.ClosedDate == nil) {
		return nil, fmt.Errorf("%w: evidence_after and closed_date are required to complete a report", ErrInvalidOperationalData)
	}
	if _, err := parseReportDate(input.ReportDate); err != nil {
		return nil, err
	}
	if input.ClosedDate != nil && input.OpenDate != nil && input.ClosedDate.Before(*input.OpenDate) {
		return nil, ErrInvalidOperationalData
	}
	input.Shift = strings.ToUpper(strings.TrimSpace(input.Shift))
	return u.repository.CreatePipelineReport(ctx, operationalAccess(actor), input)
}

func validShift(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DAY", "EVENING", "NIGHT":
		return true
	default:
		return false
	}
}

func (u *WorkflowUsecaseImpl) UpdatePipelineReport(ctx context.Context, actor *entity.User, id string, input entity.PipelineReportInput) (*entity.PipelineReport, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	// Reports are Worker-owned; managers only read them (no review step).
	if !hasRole(actor, "WORKER") {
		return nil, ErrForbidden
	}
	if !validReportStatus(input.Status) {
		return nil, ErrInvalidOperationalData
	}
	if input.Status == "" && input.EvidenceBefore == nil && input.EvidenceAfter == nil &&
		input.OpenDate == nil && input.ClosedDate == nil && input.NoteCategory == nil && input.Notes == nil {
		return nil, ErrInvalidOperationalData
	}
	if !validNoteCategory(input.NoteCategory) ||
		input.ClosedDate != nil && input.OpenDate != nil && input.ClosedDate.Before(*input.OpenDate) {
		return nil, ErrInvalidOperationalData
	}
	return u.repository.UpdatePipelineReport(ctx, operationalAccess(actor), id, input)
}

// validReportStatus accepts the Worker-settable pipeline report statuses;
// status_revisi is derived by the server from the shift window.
func validReportStatus(status string) bool {
	return status == "" || status == "IN_PROGRESS" || status == "COMPLETED"
}

func (u *WorkflowUsecaseImpl) ListInventoryReports(ctx context.Context, actor *entity.User, filter entity.ReportFilter) ([]entity.InventoryReport, int, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, 0, err
	}
	if err := validateReportFilter(&filter); err != nil {
		return nil, 0, err
	}
	return u.repository.ListInventoryReports(ctx, operationalAccess(actor), filter)
}

func (u *WorkflowUsecaseImpl) GetInventoryReport(ctx context.Context, actor *entity.User, id string) (*entity.InventoryReport, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	return u.repository.GetInventoryReport(ctx, operationalAccess(actor), id)
}

func (u *WorkflowUsecaseImpl) CreateInventoryReport(ctx context.Context, actor *entity.User, input entity.InventoryReportInput) (*entity.InventoryReport, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if !scopeUUIDPattern.MatchString(input.VendorID) || input.InventoryID != nil && !scopeUUIDPattern.MatchString(*input.InventoryID) ||
		input.ReportType != "REQUEST" && input.ReportType != "DAMAGED" ||
		input.ReportType == "DAMAGED" && input.InventoryID == nil ||
		strings.TrimSpace(input.Description) == "" || len(input.Description) > 5000 {
		return nil, ErrInvalidOperationalData
	}
	if input.Evidence != nil && len(*input.Evidence) > 500 {
		return nil, ErrInvalidOperationalData
	}
	if hasRole(actor, "WORKER") && !hasRole(actor, "ADMIN_WORKER", "ADMIN", "SUPER_ADMIN") &&
		!containsString(actor.Vendors, input.VendorID) {
		return nil, ErrForbidden
	}
	input.Description = strings.TrimSpace(input.Description)
	return u.repository.CreateInventoryReport(ctx, operationalAccess(actor), input)
}

func (u *WorkflowUsecaseImpl) ReviewInventoryReport(ctx context.Context, actor *entity.User, id, status string, reason *string) (*entity.InventoryReport, error) {
	if actor == nil || !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER") {
		return nil, ErrForbidden
	}
	if !scopeUUIDPattern.MatchString(id) || status != "IN_REVIEW" && status != "APPROVED" &&
		status != "REJECTED" && status != "RESOLVED" {
		return nil, ErrInvalidOperationalData
	}
	if reason != nil && len(*reason) > 2000 {
		return nil, ErrInvalidOperationalData
	}
	return u.repository.ReviewInventoryReport(ctx, operationalAccess(actor), id, status, reason)
}

func (u *WorkflowUsecaseImpl) ReportDashboard(ctx context.Context, actor *entity.User, date, siteID, vendorID string) (map[string]any, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if siteID != "" && !scopeUUIDPattern.MatchString(siteID) || vendorID != "" && !scopeUUIDPattern.MatchString(vendorID) {
		return nil, ErrInvalidOperationalData
	}
	parsed, err := parseReportDate(date)
	if err != nil {
		return nil, err
	}
	return u.repository.ReportDashboard(ctx, operationalAccess(actor), parsed, siteID, vendorID)
}

func (u *WorkflowUsecaseImpl) DailyReport(ctx context.Context, actor *entity.User, date, siteID, vendorID string) ([]map[string]any, error) {
	if err := validateWorkflowActor(actor); err != nil {
		return nil, err
	}
	if siteID != "" && !scopeUUIDPattern.MatchString(siteID) || vendorID != "" && !scopeUUIDPattern.MatchString(vendorID) {
		return nil, ErrInvalidOperationalData
	}
	parsed, err := parseReportDate(date)
	if err != nil {
		return nil, err
	}
	return u.repository.DailyReport(ctx, operationalAccess(actor), parsed, siteID, vendorID)
}

func validateWorkflowActor(actor *entity.User) error {
	if actor == nil || !hasRole(actor, "SUPER_ADMIN", "ADMIN", "ADMIN_WORKER", "WORKER") {
		return ErrForbidden
	}
	return nil
}

func workflowSuperAdmin(actor *entity.User) bool {
	return actor != nil && hasRole(actor, "SUPER_ADMIN")
}

func validWorkflowStatus(value string) bool {
	return value == "ACTIVE" || value == "INACTIVE"
}

func validNoteCategory(value *string) bool {
	if value == nil {
		return true
	}
	return *value == "NEAR_MISS" || *value == "UNSAFE_ACTION" || *value == "UNSAFE_CONDITION"
}

func validUUIDList(values []string) bool {
	for _, value := range values {
		if !scopeUUIDPattern.MatchString(value) {
			return false
		}
	}
	return true
}

func validateReportFilter(filter *entity.ReportFilter) error {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit == 0 {
		filter.Limit = 20
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		return ErrInvalidOperationalData
	}
	if filter.VendorID != "" && !scopeUUIDPattern.MatchString(filter.VendorID) ||
		filter.SiteID != "" && !scopeUUIDPattern.MatchString(filter.SiteID) ||
		filter.UserID != "" && !scopeUUIDPattern.MatchString(filter.UserID) {
		return ErrInvalidOperationalData
	}
	for _, date := range []string{filter.From, filter.To} {
		if date != "" {
			if _, err := parseReportDate(date); err != nil {
				return err
			}
		}
	}
	if filter.From != "" && filter.To != "" && filter.From > filter.To {
		return ErrInvalidOperationalData
	}
	if filter.Status != "" {
		valid := map[string]bool{
			"IN_PROGRESS": true, "SUBMITTED": true, "COMPLETED": true, "REJECTED": true,
			"PENDING": true, "IN_REVIEW": true, "APPROVED": true, "RESOLVED": true,
		}
		if !valid[filter.Status] {
			return ErrInvalidOperationalData
		}
	}
	return nil
}

func parseReportDate(value string) (time.Time, error) {
	if value == "" {
		return time.Now().UTC().Truncate(24 * time.Hour), nil
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must use YYYY-MM-DD", ErrInvalidOperationalData)
	}
	return date, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
