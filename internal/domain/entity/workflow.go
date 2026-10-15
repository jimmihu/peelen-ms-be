package entity

import "time"

type Site struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SiteType  *string   `json:"site_type,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Vendor struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	SiteIDs   []string  `json:"site_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Shift struct {
	Shift     string `json:"shift"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

type PipelineReport struct {
	ID                       string     `json:"id"`
	PipelineItemAssignmentID string     `json:"pipeline_item_assignment_id"`
	PipelineItemID           string     `json:"pipeline_item_id"`
	PipelineID               string     `json:"pipeline_id"`
	ItemID                   string     `json:"item_id"`
	ItemName                 string     `json:"item_name"`
	VendorID                 string     `json:"vendor_id"`
	UserID                   string     `json:"user_id"`
	Username                 string     `json:"username"`
	Shift                    string     `json:"shift"`
	ReportDate               string     `json:"report_date"`
	Status                   string     `json:"status"`
	StatusRevisi             string     `json:"status_revisi"`
	EvidenceBefore           *string    `json:"evidence_before,omitempty"`
	EvidenceAfter            *string    `json:"evidence_after,omitempty"`
	OpenDate                 *time.Time `json:"open_date,omitempty"`
	ClosedDate               *time.Time `json:"closed_date,omitempty"`
	NoteCategory             *string    `json:"note_category,omitempty"`
	Notes                    *string    `json:"notes,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type PipelineReportInput struct {
	PipelineItemID string     `json:"pipeline_item_id"`
	Shift          string     `json:"shift"`
	ReportDate     string     `json:"report_date"`
	Status         string     `json:"status"`
	EvidenceBefore *string    `json:"evidence_before"`
	EvidenceAfter  *string    `json:"evidence_after"`
	OpenDate       *time.Time `json:"open_date"`
	ClosedDate     *time.Time `json:"closed_date"`
	NoteCategory   *string    `json:"note_category"`
	Notes          *string    `json:"notes"`
	Reason         *string    `json:"reason"`
}

type InventoryReport struct {
	ID           string    `json:"id"`
	VendorID     string    `json:"vendor_id"`
	InventoryID  *string   `json:"inventory_id,omitempty"`
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	ReportType   string    `json:"report_type"`
	Evidence     *string   `json:"evidence,omitempty"`
	Description  string    `json:"description"`
	Status       string    `json:"status"`
	ReviewReason *string   `json:"review_reason,omitempty"`
	ReviewedBy   *string   `json:"reviewed_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type InventoryReportInput struct {
	VendorID     string  `json:"vendor_id"`
	InventoryID  *string `json:"inventory_id"`
	ReportType   string  `json:"report_type"`
	Evidence     *string `json:"evidence"`
	Description  string  `json:"description"`
	Status       string  `json:"status"`
	ReviewReason *string `json:"review_reason"`
}

type ReportFilter struct {
	VendorID string
	SiteID   string
	UserID   string
	Status   string
	From     string
	To       string
	Page     int
	Limit    int
}

type MyTask struct {
	AssignmentID   string  `json:"assignment_id"`
	PipelineItemID string  `json:"pipeline_item_id"`
	PipelineID     string  `json:"pipeline_id"`
	PipelineName   string  `json:"pipeline_name"`
	ItemID         string  `json:"item_id"`
	ItemName       string  `json:"item_name"`
	VendorID       string  `json:"vendor_id"`
	Shift          *string `json:"shift,omitempty"`
	ReportDate     string  `json:"report_date"`
	ReportID       *string `json:"report_id,omitempty"`
	ReportStatus   *string `json:"report_status,omitempty"`
	StatusRevisi   *string `json:"status_revisi,omitempty"`
}
