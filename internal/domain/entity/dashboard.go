
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
	TargetPekerjaan int64 `json:"target_pekerjaan"`
	TotalArea       int64 `json:"total_area"`
	TotalPoint      int64 `json:"total_point"`
	InProgress      int64 `json:"in_progress"`
	Completed       int64 `json:"completed"`
	NotStarted      int64 `json:"pending"`
}

type DashboardResponse struct {
	Filter             DashboardFilter                  `json:"filter"`
	TaskSummary        DashboardTaskSummary             `json:"task_summary"`
	VendorProgress     []DashboardVendorProgressSummary `json:"vendor_progress"`
	VendorUnassignments []DashboardVendorUnassignment   `json:"vendor_unassignments"`
	InventoryReport    []DashboardInventoryReport      `json:"inventory_reports"`
	SafetyReports      []DashboardSafetyReport         `json:"safety_reports"`
}

type DashboardSummaryResponse = DashboardResponse

type DashboardVendorProgressSummary struct {
	SiteID             string  `json:"site_id"`
	SiteName           string  `json:"site_name"`
	VendorID           string  `json:"vendor_id"`
	VendorName         string  `json:"vendor_name"`
	TargetPekerjaan    int64   `json:"target_pekerjaan"`
	TotalPenugasan     int64   `json:"total_penugasan"`
	TotalArea          int64   `json:"total_area"`
	TotalPoint         int64   `json:"total_point"`
	Completed          int64   `json:"completed"`
	InProgress         int64   `json:"in_progress"`
	NotStarted         int64   `json:"pending"`
	ProgressPercentage float64 `json:"progress_percentage"`
}

type DashboardVendorProgress struct {
	SiteID               string          `json:"site_id"`
	SiteName              string          `json:"site_name"`
	VendorID              string          `json:"vendor_id"`
	VendorName            string          `json:"vendor_name"`
	TargetPekerjaan       int64           `json:"target_pekerjaan"`
	TotalPenugasan        int64           `json:"total_penugasan"`
	TotalArea             int64           `json:"total_area"`
	TotalPoint            int64           `json:"total_point"`
	Completed             int64           `json:"completed"`
	InProgress            int64           `json:"in_progress"`
	NotStarted            int64           `json:"pending"`
	ProgressPercentage    float64         `json:"progress_percentage"`
	VendorProgressDetail  json.RawMessage `json:"vendor_progress_detail,omitempty"`
}

type DashboardVendorProgressResponse struct {
	VendorProgress      []DashboardVendorProgress    `json:"vendor_progress"`
	VendorUnassignments []DashboardVendorUnassignment `json:"vendor_unassignments"`
}

// DashboardVendorUnassignment berisi target point yang belum memiliki
// penugasan aktif pada site dan vendor terkait.
type DashboardVendorUnassignment struct {
	SiteID                string                            `json:"site_id"`
	SiteName              string                            `json:"site_name"`
	VendorID              string                            `json:"vendor_id"`
	VendorName            string                            `json:"vendor_name"`
	TotalUnassignedPoint  int64                             `json:"total_unassigned_point"`
	AreaDetail            []DashboardVendorUnassignmentArea `json:"area_detail"`
}

// DashboardVendorUnassignmentArea mengelompokkan point yang belum
// ditugaskan berdasarkan area.
type DashboardVendorUnassignmentArea struct {
	AreaID             string                             `json:"area_id"`
	NamaArea           string                             `json:"nama_area"`
	TotalPointPerArea  int64                              `json:"total_point_per_area"`
	PointDetail        []DashboardVendorUnassignedPoint   `json:"point_detail"`
}

// DashboardVendorUnassignedPoint adalah satu target point yang belum
// memiliki pipeline_item_assignment aktif.
type DashboardVendorUnassignedPoint struct {
	PipelineItemID string `json:"pipeline_item_id"`
	ItemID         string `json:"item_id"`
	NamaPoint      string `json:"nama_point"`
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
