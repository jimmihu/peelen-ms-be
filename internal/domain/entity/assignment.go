package entity

import "time"

type Assignment struct {
	ID             string    `json:"id"`
	PipelineItemID string    `json:"pipeline_item_id"`
	PipelineID     string    `json:"pipeline_id"`
	PipelineName   string    `json:"pipeline_name"`
	ItemID         string    `json:"item_id"`
	ItemName       string    `json:"item_name"`
	VendorID       string    `json:"vendor_id"`
	UserID         string    `json:"user_id"`
	Username       string    `json:"username"`
	Shift          string    `json:"shift"`
	IsActive       bool      `json:"is_active"`
	AssignedBy     *string   `json:"assigned_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AssignmentInput struct {
	PipelineItemID string `json:"pipeline_item_id"`
	UserID         string `json:"user_id"`
	Shift          string `json:"shift"`
	IsActive       *bool  `json:"is_active"`
}

type AssignmentFilter struct {
	VendorID       string
	SiteID         string
	PipelineID     string
	PipelineItemID string
	UserID         string
	Shift          string
	IsActive       *bool
	Page           int
	Limit          int
}
