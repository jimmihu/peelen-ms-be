package entity

import (
	"encoding/json"
	"time"
)

type DashboardFilter struct {
	UserID   string `json:"-"`
	IsWorker bool   `json:"-"`
	SiteID   string `json:"site_id"`
	VendorID string `json:"vendor_id"`
	Date     string `json:"date"`
}

type DashboardTaskSummary struct {
	TotalPoint int64 `json:"total_point"`
	TotalTask  int64 `json:"total_task"`
	TotalArea  int64 `json:"total_area"`
	InProgress int64 `json:"in_progress"`
	Completed  int64 `json:"completed"`
	NotStarted int64 `json:"pending"`
}

type DashboardResponse struct {
	Filter          DashboardFilter                  `json:"filter"`
	TaskSummary     DashboardTaskSummary             `json:"task_summary"`
	VendorProgress  []DashboardVendorProgressSummary `json:"vendor_progress"`
	InventoryReport []DashboardInventoryReport       `json:"inventory_reports"`
	SafetyReports   []DashboardSafetyReport          `json:"safety_reports"`
}

type DashboardSummaryResponse = DashboardResponse

// DashboardVendorProgressSummary digunakan untuk response ringkasan
// vendor_progress pada endpoint dashboard summary.
type DashboardVendorProgressSummary struct {
	SiteID             string  `json:"site_id"`
	SiteName           string  `json:"site_name"`
	VendorID           string  `json:"vendor_id"`
	VendorName         string  `json:"vendor_name"`
	TotalTask          int64   `json:"total_task"`
	TotalArea          int64   `json:"total_area"`
	TotalPoint         int64   `json:"total_point"`
	TotalWorker        int64   `json:"total_worker"`
	TotalReport        int64   `json:"total_report"`
	Completed          int64   `json:"completed"`
	InProgress         int64   `json:"in_progress"`
	NotStarted         int64   `json:"pending"`
	ProgressPercentage float64 `json:"progress_percentage"`
	StatusCheckPoint   string  `json:"status_check_point"`
}

// DashboardVendorProgress digunakan untuk response detail vendor_progress.
// VendorProgressDetail berisi area_detail dan check_point yang dibentuk
// oleh query repository.
type DashboardVendorProgress struct {
	SiteID               string          `json:"site_id"`
	SiteName             string          `json:"site_name"`
	VendorID             string          `json:"vendor_id"`
	VendorName           string          `json:"vendor_name"`
	TotalTask            int64           `json:"total_task"`
	TotalArea            int64           `json:"total_area"`
	TotalPoint           int64           `json:"total_point"`
	TotalWorker          int64           `json:"total_worker"`
	TotalReport          int64           `json:"total_report"`
	Completed            int64           `json:"completed"`
	InProgress           int64           `json:"in_progress"`
	NotStarted           int64           `json:"pending"`
	ProgressPercentage   float64         `json:"progress_percentage"`
	StatusCheckPoint     string          `json:"status_check_point"`
	VendorProgressDetail json.RawMessage `json:"vendor_progress_detail,omitempty"`
}

type DashboardVendorProgressResponse struct {
	VendorProgress []DashboardVendorProgress `json:"vendor_progress"`
}

// DashboardVendorCheckPoint menunjukkan apakah semua target point
// pada site dan vendor sudah memiliki assignment aktif.
type DashboardVendorCheckPoint struct {
	Status                string                           `json:"status"`
	TotalTargetPoint      int64                            `json:"total_target_point"`
	TotalAssignedPoint    int64                            `json:"total_assigned_point"`
	TotalUnassignedPoint  int64                            `json:"total_unassigned_point"`
	UnassignedPointDetail []DashboardVendorUnassignedPoint `json:"unassigned_point_detail"`
}

// DashboardVendorUnassignment dipertahankan untuk kebutuhan internal,
// bukan sebagai field langsung pada response dashboard.
type DashboardVendorUnassignment struct {
	SiteID               string                            `json:"site_id"`
	SiteName             string                            `json:"site_name"`
	VendorID             string                            `json:"vendor_id"`
	VendorName           string                            `json:"vendor_name"`
	TotalUnassignedPoint int64                             `json:"total_unassigned_point"`
	AreaDetail           []DashboardVendorUnassignmentArea `json:"area_detail"`
}

// DashboardVendorUnassignmentArea mengelompokkan point yang belum
// ditugaskan berdasarkan area.
type DashboardVendorUnassignmentArea struct {
	AreaID            string                           `json:"area_id"`
	NamaArea          string                           `json:"nama_area"`
	TotalPointPerArea int64                            `json:"total_point_per_area"`
	PointDetail       []DashboardVendorUnassignedPoint `json:"point_detail"`
}

// DashboardVendorUnassignedPoint merepresentasikan satu target point yang belum memiliki assignment aktif.
type DashboardVendorUnassignedPoint struct {
	PipelineItemID string `json:"pipeline_item_id"`
	ItemID         string `json:"item_id"`
	NamaPoint      string `json:"nama_point"`
	AreaID         string `json:"area_id,omitempty"`
	NamaArea       string `json:"nama_area,omitempty"`
}

type DashboardInventoryReport struct {
	InventoryReportID string    `json:"inventory_report_id"`
	VendorName        string    `json:"vendor_name"`
	FullName          string    `json:"full_name"`
	Item              *string   `json:"item"`
	Description       *string   `json:"description"`
	StatusCondition   *string   `json:"status_condition"`
	Evidence          *string   `json:"evidence"`
	ReportType        string    `json:"report_type"`
	ReportStatus      string    `json:"report_status"`
	ReportDate        time.Time `json:"report_date"`
}

type DashboardSafetyReport struct {
	SiteName     string     `json:"site_name"`
	VendorName   string     `json:"vendor_name"`
	WorkerName   string     `json:"worker_name"`
	CategoryName string     `json:"category_name"`
	AreaName     string     `json:"area_name"`
	NoteCategory *string    `json:"note_category"`
	Notes        *string    `json:"notes"`
	OpenDate     *time.Time `json:"open_date"`
	ClosedDate   *time.Time `json:"closed_date"`
	Shift        string     `json:"shift"`
	ReportDate   time.Time  `json:"report_date"`
}

type DashboardDetailResponse struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}
