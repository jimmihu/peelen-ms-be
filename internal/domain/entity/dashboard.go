package entity

import "time"

type DashboardFilter struct {
	UserID   string `json:"-"`
	IsWorker bool   `json:"-"`
	SiteID   string `json:"site_id"`
	VendorID string `json:"vendor_id"`
	Date     string `json:"date"`
}

type DashboardTaskSummary struct {
	TotalTask  int64 `json:"total_task"`
	InProgress int64 `json:"in_progress"`
	Completed  int64 `json:"completed"`
	NotStarted int64 `json:"pending"`
}

type DashboardVendorProgress struct {
	VendorID           string  `json:"vendor_id"`
	VendorName         string  `json:"vendor_name"`
	TotalTask          int64   `json:"total_task"`
	Completed          int64   `json:"completed"`
	InProgress         int64   `json:"in_progress"`
	NotStarted         int64   `json:"pending"`
	ProgressPercentage float64 `json:"progress_percentage"`
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

type DashboardResponse struct {
	Filter          DashboardFilter            `json:"filter"`
	TaskSummary     DashboardTaskSummary       `json:"task_summary"`
	VendorProgress  []DashboardVendorProgress  `json:"vendor_progress"`
	InventoryReport []DashboardInventoryReport `json:"inventory_reports"`
	SafetyReports   []DashboardSafetyReport    `json:"safety_reports"`
}
