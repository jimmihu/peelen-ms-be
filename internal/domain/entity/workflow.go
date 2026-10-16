package entity

import "time"

type Site struct {
	ID       string  `json:"id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	SiteType *string `json:"site_type,omitempty"`
	// Timezone is the site's IANA zone (Asia/Jakarta, Asia/Makassar, Asia/Jayapura).
	Timezone  string    `json:"timezone"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// VendorFilter narrows a vendor list: vendors working at SiteID, with Status,
// whose code or name contains Query. Empty fields do not filter.
type VendorFilter struct {
	SiteID string
	Status string
	Query  string
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
	ID                       string  `json:"id"`
	PipelineItemAssignmentID string  `json:"pipeline_item_assignment_id"`
	PipelineItemID           string  `json:"pipeline_item_id"`
	PipelineID               string  `json:"pipeline_id"`
	ItemID                   string  `json:"item_id"`
	ItemName                 string  `json:"item_name"`
	VendorID                 string  `json:"vendor_id"`
	UserID                   string  `json:"user_id"`
	Username                 string  `json:"username"`
	SiteID                   string  `json:"site_id"`
	SiteName                 string  `json:"site_name"`
	SiteTimezone             string  `json:"site_timezone"` // IANA zone of shift hours and report_date
	Shift                    string  `json:"shift"`
	ReportDate               string  `json:"report_date"`
	Status                   string  `json:"status"`
	StatusRevisi             string  `json:"status_revisi"`
	EvidenceBefore           *string `json:"evidence_before,omitempty"`
	EvidenceAfter            *string `json:"evidence_after,omitempty"`
	// Short-lived signed download URLs of the evidence photos (set by the handler).
	EvidenceBeforeURL *string    `json:"evidence_before_url,omitempty"`
	EvidenceAfterURL  *string    `json:"evidence_after_url,omitempty"`
	OpenDate          *time.Time `json:"open_date,omitempty"`
	ClosedDate        *time.Time `json:"closed_date,omitempty"`
	NoteCategory      *string    `json:"note_category,omitempty"`
	Notes             *string    `json:"notes,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
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
	// Wall-clock photo times without an offset ("2006-01-02T15:04:05"), read in
	// the site's time zone. Set by the upload handler from EXIF; not part of the API.
	OpenDateLocal   *string `json:"-"`
	ClosedDateLocal *string `json:"-"`
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
	SiteID         string  `json:"site_id"`
	SiteName       string  `json:"site_name"`
	SiteTimezone   string  `json:"site_timezone"` // IANA zone of shift hours and report_date
	Shift          *string `json:"shift,omitempty"`
	ShiftStart     *string `json:"shift_start,omitempty"`
	ShiftEnd       *string `json:"shift_end,omitempty"`
	ReportDate     string  `json:"report_date"` // today at the site when no date was asked
	ReportID       *string `json:"report_id,omitempty"`
	ReportStatus   *string `json:"report_status,omitempty"`
	StatusRevisi   *string `json:"status_revisi,omitempty"`
}
