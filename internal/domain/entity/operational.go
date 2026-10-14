package entity

import (
	"errors"
	"time"
)

var (
	ErrInvalidID                  = errors.New("invalid resource ID")
	ErrInvalidOperationalData     = errors.New("invalid operational data")
	ErrInvalidOperationalStatus   = errors.New("invalid operational status")
	ErrInvalidOperationalResource = errors.New("invalid operational resource")
	ErrReportUploadClosed         = errors.New("report upload window has closed; it ends when the same shift starts the next day")
)

type OperationalResource string

const (
	ResourceInventory    OperationalResource = "inventories"
	ResourcePolicy       OperationalResource = "policies"
	ResourceItem         OperationalResource = "items"
	ResourcePipeline     OperationalResource = "pipelines"
	ResourcePipelineItem OperationalResource = "pipeline-items"
)

type OperationalRecord struct {
	ID          string    `json:"id"`
	VendorID    *string   `json:"vendor_id,omitempty"`
	Name        string    `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Item        string    `json:"item,omitempty"`
	ItemType    *string   `json:"jenis_item,omitempty"`
	Quantity    *int      `json:"qty,omitempty"`
	Image       *string   `json:"-"`
	ImageURL    *string   `json:"image_url,omitempty"`
	Condition   string    `json:"status_condition,omitempty"`
	FileURL     *string   `json:"-"`
	PipelineID  *string   `json:"pipeline_id,omitempty"`
	ItemID      *string   `json:"item_id,omitempty"`
	SortOrder   *int      `json:"sort_order,omitempty"`
	Status      string    `json:"status"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type OperationalInput struct {
	VendorID    *string `json:"vendor_id"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Item        *string `json:"item"`
	ItemType    *string `json:"jenis_item"`
	Quantity    *int    `json:"qty"`
	Image       *string `json:"img"`
	Condition   *string `json:"status_condition"`
	FileURL     *string `json:"file_url"`
	PipelineID  *string `json:"pipeline_id"`
	ItemID      *string `json:"item_id"`
	SortOrder   *int    `json:"sort_order"`
	Status      *string `json:"status"`
}

type OperationalFilter struct {
	VendorID   string
	SiteID     string
	PipelineID string
	Status     string
	Condition  string
	Query      string
	Page       int
	Limit      int
}

type OperationalAccess struct {
	UserID    string
	Roles     []string
	SiteIDs   []string
	VendorIDs []string
}
