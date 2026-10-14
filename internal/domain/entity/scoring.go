package entity

import (
	"time"

	"peelen-ms-be/internal/scoring"
)

const (
	ScopeVendor = "VENDOR"
	ScopeSite   = "SITE"
	ScopeWorker = "WORKER"

	RuleKindSLA      = "SLA"
	RuleKindMaturity = "MATURITY"

	ResultStatusNoRule = "NO_RULE"
)

type FactQuery struct {
	Scope          string
	VendorID       string
	SiteIDs        []string
	ExcludeSiteIDs []string
	From, To       time.Time
}

type ScopeCounts struct {
	VendorID string
	SiteID   *string
	WorkerID *string
	Counts   scoring.Counts
}

type RuleItem struct {
	Code   string   `json:"code"`
	Weight float64  `json:"weight"`
	Target *float64 `json:"target,omitempty"`
}

type ScoreRule struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	VendorID     string              `json:"vendor_id"`
	SiteID       *string             `json:"site_id,omitempty"`
	PeriodType   string              `json:"period_type"`
	Version      int                 `json:"version"`
	Items        []RuleItem          `json:"items"`
	Levels       []scoring.LevelRule `json:"levels,omitempty"`
	CreatedBy    *string             `json:"created_by,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	SupersededAt *time.Time          `json:"superseded_at,omitempty"`
}

type ScoreRuleInput struct {
	Kind       string              `json:"-"`
	VendorID   string              `json:"vendor_id"`
	SiteID     *string             `json:"site_id"`
	PeriodType string              `json:"period_type"`
	Items      []RuleItem          `json:"items"`
	Levels     []scoring.LevelRule `json:"levels"`
}

type RuleFilter struct {
	Kind       string
	VendorID   string
	SiteID     *string
	ActiveOnly bool
	// Visibility, set by the usecase from the caller's access.
	AllVendors bool
	VendorIDs  []string
}
